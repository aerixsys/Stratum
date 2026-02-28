package middleware

import (
	"github.com/gin-gonic/gin"
)

// RequestID is a compatibility shim that preserves legacy middleware wiring
// while delegating request ID behavior to RequestContext.
func RequestID() gin.HandlerFunc {
	return RequestContext(RequestContextOptions{AccessLogEnabled: false})
}
