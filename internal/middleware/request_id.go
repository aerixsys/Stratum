package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDKey = "request_id"

// RequestID sets and propagates an X-Request-ID for each request.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" {
			reqID = uuid.NewString()
		}
		c.Set(requestIDKey, reqID)
		c.Writer.Header().Set("X-Request-ID", reqID)
		c.Next()
	}
}

// GetRequestID returns the request ID from context if available.
func GetRequestID(c *gin.Context) string {
	v, ok := c.Get(requestIDKey)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
