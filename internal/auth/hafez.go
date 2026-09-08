package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"project1/internal/config"
)

var (
	rolesMu    sync.Mutex
	rolesCache []string
	rolesExp   time.Time
)

func extractRoles(body []byte) []string {
	var resp struct {
		Data []struct {
			RoleList []struct {
				Value string `json:"value"`
			} `json:"role_list"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &resp) != nil || len(resp.Data) == 0 {
		return nil
	}
	out := make([]string, 0, len(resp.Data[0].RoleList))
	for _, r := range resp.Data[0].RoleList {
		if r.Value != "" {
			out = append(out, r.Value)
		}
	}
	return out
}

func hafezRoles(header string) []string {
	req, err := http.NewRequest(http.MethodGet, config.HafezBase+"hafez-core/api/v1/clients/find-by-name?name="+url.QueryEscape(config.ClientID), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", header)
	res, err := config.HTTP.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil
	}
	return extractRoles(b)
}

func fetchAllowedRoles() ([]string, error) {
	if roles := hafezRoles("Basic " + base64.StdEncoding.EncodeToString([]byte(config.HafezUser+":"+config.HafezPass))); len(roles) > 0 {
		return roles, nil
	}
	res, err := config.HTTP.PostForm(config.TokenURL(), url.Values{
		"grant_type": {"client_credentials"}, "client_id": {config.ClientID}, "client_secret": {config.Secret},
	})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		return nil, errors.New("no hafez token")
	}
	if roles := hafezRoles("Bearer " + tok.AccessToken); len(roles) > 0 {
		return roles, nil
	}
	return nil, errors.New("no hafez roles")
}

func AllowedRoles() ([]string, error) {
	rolesMu.Lock()
	defer rolesMu.Unlock()
	if time.Now().Before(rolesExp) && rolesCache != nil {
		return rolesCache, nil
	}
	roles, err := fetchAllowedRoles()
	if err != nil {
		return nil, err
	}
	rolesCache, rolesExp = roles, time.Now().Add(time.Minute)
	return roles, nil
}

func Check() error {
	_, err := AllowedRoles()
	return err
}
