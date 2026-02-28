package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stratum/gateway/internal/logging"
)

const requestIDKey = "request_id"

type RequestContextOptions struct {
	AccessLogEnabled bool
}

var requestLogSkipMethods = map[string]struct{}{
	http.MethodOptions: {},
}

var requestLogSkipPaths = map[string]struct{}{
	"/health":      {},
	"/ready":       {},
	"/metrics":     {},
	"/favicon.ico": {},
}

// RequestContext sets and propagates X-Request-ID and optionally emits access logs.
func RequestContext(options RequestContextOptions) gin.HandlerFunc {
	accessLogEnabled := options.AccessLogEnabled

	return func(c *gin.Context) {
		reqID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if reqID == "" {
			reqID = uuid.NewString()
		}
		c.Set(requestIDKey, reqID)
		c.Writer.Header().Set("X-Request-ID", reqID)

		start := time.Now()
		c.Next()
		duration := time.Since(start)

		if !accessLogEnabled {
			return
		}
		if shouldSkipAccessLog(c.Request.Method, c.Request.URL.Path) {
			return
		}

		var errText string
		if len(c.Errors) > 0 {
			errText = strings.TrimSpace(c.Errors.String())
		}
		logging.AccessLog(
			c.Request.Method,
			c.Request.URL.Path,
			c.Writer.Status(),
			duration.Milliseconds(),
			c.ClientIP(),
			len(c.Errors),
			errText,
		)
	}
}

func shouldSkipAccessLog(method, path string) bool {
	if _, ok := requestLogSkipMethods[strings.ToUpper(strings.TrimSpace(method))]; ok {
		return true
	}
	if _, ok := requestLogSkipPaths[strings.TrimSpace(path)]; ok {
		return true
	}
	return false
}

// GetRequestID returns the request ID from context/header if available.
func GetRequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if rid := strings.TrimSpace(c.GetString(requestIDKey)); rid != "" {
		return rid
	}
	return strings.TrimSpace(c.GetHeader("X-Request-ID"))
}
