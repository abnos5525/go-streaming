package swagger

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/swaggest/openapi-go"
	"github.com/swaggest/openapi-go/openapi3"
	"github.com/swaggest/swgui"
	"github.com/swaggest/swgui/v5emb"
)

type helloResponse struct {
	Message string `json:"message"`
}

type streamStart struct {
	URL        string `json:"url" default:"udp://224.2.0.1:5484" example:"udp://224.2.0.1:5484"`
	OutputType int    `json:"output_type" example:"6"`
}

type streamURL struct {
	URL        string `json:"url"`
	OutputType int    `json:"output_type"`
}

type streamStop struct {
	URL string `json:"url" default:"udp://224.2.0.1:5484" example:"udp://224.2.0.1:5484"`
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
	ID         string        `json:"id"`
	URL        string        `json:"url"`
	RTSP       string        `json:"rtsp"`
	Started    bool          `json:"started"`
	Ready      bool          `json:"ready"`
	Error      string        `json:"error,omitempty"`
	Tracks     []streamTrack `json:"tracks,omitempty"`
	RTP        uint32        `json:"rtp,omitempty"`
	AI         any           `json:"ai,omitempty"`
	SEI        any           `json:"sei,omitempty"`
	AIAgeMS    int64         `json:"ai_age_ms,omitempty"`
	AIFPS      float64       `json:"ai_fps,omitempty"`
	OutputType int           `json:"output_type,omitempty"`
}

func spec() []byte {
	r := openapi3.NewReflector()
	r.SpecEns().Info.WithTitle("go-streaming").WithVersion("0.1.0").
		WithDescription("Low-latency multicast / RTSP ingest to Apple LL-HLS and WebRTC.")
	op, err := r.NewOperationContext(http.MethodGet, "/")
	if err != nil {
		panic(err)
	}
	op.SetSummary("health")
	op.AddRespStructure(new(helloResponse), func(cu *openapi.ContentUnit) { cu.HTTPStatus = http.StatusOK })
	if err := r.AddOperation(op); err != nil {
		panic(err)
	}
	meta, err := r.NewOperationContext(http.MethodGet, "/stream/meta")
	if err != nil {
		panic(err)
	}
	meta.SetSummary("List stream sessions")
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
	st.SetSummary("Start ingest; output_type 6 = LL-HLS, 7 = WebRTC")
	st.AddReqStructure(new(streamStart))
	st.AddRespStructure(new(streamURL), func(cu *openapi.ContentUnit) { cu.HTTPStatus = http.StatusOK })
	if err := r.AddOperation(st); err != nil {
		panic(err)
	}
	sp, err := r.NewOperationContext(http.MethodPost, "/stream/stop")
	if err != nil {
		panic(err)
	}
	sp.SetSummary("Stop a stream by source URL")
	sp.AddReqStructure(new(streamStop))
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

func Register(r *gin.Engine) {
	doc := spec()
	docs := v5emb.NewHandlerWithConfig(swgui.Config{
		Title:       "go-streaming",
		SwaggerJSON: "/openapi.json",
		BasePath:    "/api/doc/",
	})
	r.GET("/openapi.json", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", doc)
	})
	r.GET("/api/doc", func(c *gin.Context) { c.Redirect(http.StatusFound, "/api/doc/") })
	r.Any("/api/doc/*path", gin.WrapH(docs))
}
