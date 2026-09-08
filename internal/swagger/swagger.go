package swagger

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/swaggest/openapi-go"
	"github.com/swaggest/openapi-go/openapi3"
	"github.com/swaggest/swgui"
	"github.com/swaggest/swgui/v5emb"
	"project1/internal/config"
)

type helloResponse struct {
	Message string `json:"message"`
}

type streamStart struct {
	URL string `json:"url" default:"udp://224.2.0.1:5484" example:"udp://224.2.0.1:5484"`
}

type streamURL struct {
	URL string `json:"url"`
}

type streamMsg struct {
	Message string `json:"message"`
}

type streamID struct {
	ID string `path:"id"`
}

type streamTrack struct {
	Type    string `json:"type"`
	Codec   string `json:"codec"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	FPS     string `json:"fps,omitempty"`
	Handler string `json:"handler,omitempty"`
}

type streamMeta struct {
	URL     string        `json:"url"`
	RTSP    string        `json:"rtsp"`
	Started bool          `json:"started"`
	Ready   bool          `json:"ready"`
	Error   string        `json:"error,omitempty"`
	Tracks  []streamTrack `json:"tracks,omitempty"`
	RTP     uint32        `json:"rtp,omitempty"`
	AI      any           `json:"ai,omitempty"`
}

func spec() []byte {
	r := openapi3.NewReflector()
	r.SpecEns().Info.WithTitle("project1").WithVersion("1.0.0")
	r.SpecEns().SetHTTPBearerTokenSecurity("bearer-auth", "JWT", "JWT")
	r.SpecEns().ComponentsEns().SecuritySchemesEns().WithMapOfSecuritySchemeOrRefValuesItem(
		"keycloak-auth",
		openapi3.SecuritySchemeOrRef{
			SecurityScheme: (&openapi3.SecurityScheme{}).WithOAuth2SecurityScheme(
				*(&openapi3.OAuth2SecurityScheme{}).WithFlows(
					*(&openapi3.OAuthFlows{}).WithPassword(
						*(&openapi3.PasswordOAuthFlow{}).
							WithTokenURL("/oauth/token").
							WithScopes(map[string]string{
								"openid":  "OpenID Connect",
								"profile": "Profile",
								"email":   "Email",
							}),
					),
				),
			),
		},
	)
	op, err := r.NewOperationContext(http.MethodGet, "/")
	if err != nil {
		panic(err)
	}
	op.SetSummary("hello world")
	op.AddSecurity("keycloak-auth", "openid", "profile", "email")
	op.AddSecurity("bearer-auth")
	op.AddRespStructure(new(helloResponse), func(cu *openapi.ContentUnit) { cu.HTTPStatus = http.StatusOK })
	if err := r.AddOperation(op); err != nil {
		panic(err)
	}
	meta, err := r.NewOperationContext(http.MethodGet, "/stream/meta")
	if err != nil {
		panic(err)
	}
	meta.SetSummary("HLS playlist status")
	meta.AddRespStructure(new([]streamMeta), func(cu *openapi.ContentUnit) { cu.HTTPStatus = http.StatusOK })
	if err := r.AddOperation(meta); err != nil {
		panic(err)
	}
	one, err := r.NewOperationContext(http.MethodGet, "/stream/meta/{id}")
	if err != nil {
		panic(err)
	}
	one.SetSummary("One stream status")
	one.AddReqStructure(new(streamID))
	one.AddRespStructure(new(streamMeta), func(cu *openapi.ContentUnit) { cu.HTTPStatus = http.StatusOK })
	if err := r.AddOperation(one); err != nil {
		panic(err)
	}
	st, err := r.NewOperationContext(http.MethodPost, "/stream/start")
	if err != nil {
		panic(err)
	}
	st.SetSummary("Start RTSP to HLS")
	st.AddReqStructure(new(streamStart))
	st.AddRespStructure(new(streamURL), func(cu *openapi.ContentUnit) { cu.HTTPStatus = http.StatusOK })
	if err := r.AddOperation(st); err != nil {
		panic(err)
	}
	sp, err := r.NewOperationContext(http.MethodPost, "/stream/stop")
	if err != nil {
		panic(err)
	}
	sp.SetSummary("Stop RTSP to HLS")
	sp.AddReqStructure(new(streamStart))
	sp.AddRespStructure(new(streamMsg), func(cu *openapi.ContentUnit) { cu.HTTPStatus = http.StatusOK })
	if err := r.AddOperation(sp); err != nil {
		panic(err)
	}
	b, err := json.Marshal(r.Spec)
	if err != nil {
		panic(err)
	}
	return b
}

func ui() http.Handler {
	oauth, err := json.Marshal(map[string]string{
		"clientId":     config.ClientID,
		"clientSecret": config.Secret,
		"realm":        config.Realm,
		"appName":      "VMS API",
		"scopes":       "openid profile email",
	})
	if err != nil {
		panic(err)
	}
	return v5emb.NewHandlerWithConfig(swgui.Config{
		Title:       "project1",
		SwaggerJSON: "/openapi.json",
		BasePath:    "/api/doc/",
		SettingsUI: map[string]string{
			"persistAuthorization": "true",
			"onComplete":           fmt.Sprintf(`function(){setTimeout(function(){window.ui.initOAuth(%s)},0)}`, oauth),
		},
	})
}

func writeJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write([]byte(body))
}

func tokenProxy(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, `{"error":"invalid_request"}`)
		return
	}
	r.PostForm.Set("client_id", config.ClientID)
	r.PostForm.Set("client_secret", config.Secret)
	if r.PostForm.Get("scope") == "" {
		r.PostForm.Set("scope", "openid")
	}
	res, err := config.HTTP.PostForm(config.TokenURL(), r.PostForm)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, `{"error":"token_request_failed"}`)
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	w.WriteHeader(res.StatusCode)
	io.Copy(w, res.Body)
}

func Register(r *gin.Engine) {
	doc := spec()
	docs := ui()
	r.GET("/openapi.json", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", doc)
	})
	r.GET("/api/doc", func(c *gin.Context) { c.Redirect(http.StatusFound, "/api/doc/") })
	r.Any("/api/doc/*path", gin.WrapH(docs))
	r.POST("/oauth/token", gin.WrapF(tokenProxy))
}
