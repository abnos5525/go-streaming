package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var ErrSecret = errors.New("AUTH_SECRET is empty")

func Sign(secret string, ttl time.Duration, now time.Time) (string, error) {
	if secret == "" {
		return "", ErrSecret
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	header := encode([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}{Sub: "demo", Exp: now.Add(ttl).Unix()})
	if err != nil {
		return "", err
	}
	body := header + "." + encode(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	return body + "." + encode(mac.Sum(nil)), nil
}

func Auth() gin.HandlerFunc {
	secret := os.Getenv("AUTH_SECRET")
	return func(c *gin.Context) {
		if secret == "" || allowed(c.GetHeader("Authorization"), secret, time.Now()) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
	}
}

func allowed(authorization, secret string, now time.Time) bool {
	typ, token, ok := strings.Cut(authorization, " ")
	if !ok || !strings.EqualFold(typ, "Bearer") || strings.Count(token, ".") != 2 {
		return false
	}
	header, rest, _ := strings.Cut(token, ".")
	payload, sig, _ := strings.Cut(rest, ".")
	body := header + "." + payload
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(raw, &claims) != nil || claims.Exp <= now.Unix() {
		return false
	}
	return true
}

func encode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
