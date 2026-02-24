package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/stratum/gateway/internal/schema"
)

// APIKeyAuth validates the Bearer token against the configured API key.
func APIKeyAuth(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" {
			schema.Unauthorized(c, "Missing Authorization header. Expected: Bearer <api_key>")
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		token = strings.TrimSpace(token)

		if token == "" || token == auth {
			schema.Unauthorized(c, "Invalid Authorization format. Expected: Bearer <api_key>")
			return
		}

		if !secureCompare(token, apiKey) {
			schema.Unauthorized(c, "Invalid API key")
			return
		}

		c.Set("api_key_present", true)
		c.Next()
	}
}

func secureCompare(a, b string) bool {
	ah := sha256.Sum256([]byte(a))
	bh := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ah[:], bh[:]) == 1
}
