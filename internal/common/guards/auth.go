package guards

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"project1/internal/config"
	"project1/internal/utils"
)

func Allowed(authorization string) bool {
	typ, token, ok := strings.Cut(authorization, " ")
	if !ok || !strings.EqualFold(typ, "Bearer") || token == "" {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var p struct {
		Exp            float64 `json:"exp"`
		ResourceAccess map[string]struct {
			Roles []string `json:"roles"`
		} `json:"resource_access"`
	}
	if json.Unmarshal(b, &p) != nil || (p.Exp > 0 && float64(time.Now().Unix()) > p.Exp) {
		return false
	}
	allowed, err := utils.AllowedRoles()
	if err != nil {
		return false
	}
	return slices.ContainsFunc(p.ResourceAccess[config.ClientID].Roles, func(role string) bool {
		return slices.Contains(allowed, role)
	})
}

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !Allowed(c.GetHeader("Authorization")) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
			return
		}
		c.Next()
	}
}
