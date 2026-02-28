package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stratum/gateway/internal/logging"
)

func configureTestLogging(t *testing.T, level string) *bytes.Buffer {
	t.Helper()
	if err := logging.Configure(level); err != nil {
		t.Fatalf("configure log level: %v", err)
	}
	buf := &bytes.Buffer{}
	logging.SetOutput(buf)
	logging.SetTTYMode(false)
	t.Cleanup(func() {
		logging.SetOutput(nil)
		logging.SetTTYMode(false)
		_ = logging.Configure("info")
	})
	return buf
}

func TestRequestContext_AccessLogEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := configureTestLogging(t, "debug")

	r := gin.New()
	r.Use(RequestContext(RequestContextOptions{AccessLogEnabled: true}))
	r.GET("/v1/models", func(c *gin.Context) { c.Status(http.StatusOK) })

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.RemoteAddr = "203.0.113.9:4321"
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	out := buf.String()
	if !strings.Contains(out, "GET /v1/models 200") {
		t.Fatalf("expected access log, got %q", out)
	}
	if !strings.Contains(out, "ip=203.0.113.9") {
		t.Fatalf("expected client ip in access log, got %q", out)
	}
}

func TestRequestContext_AccessLogDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := configureTestLogging(t, "debug")

	r := gin.New()
	r.Use(RequestContext(RequestContextOptions{AccessLogEnabled: false}))
	r.GET("/v1/models", func(c *gin.Context) { c.Status(http.StatusOK) })

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no access logs when disabled, got %q", buf.String())
	}
}

func TestRequestContext_SkipPathsAndOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := configureTestLogging(t, "debug")

	r := gin.New()
	r.Use(RequestContext(RequestContextOptions{AccessLogEnabled: true}))
	r.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/ready", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/metrics", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/v1/models", func(c *gin.Context) { c.Status(http.StatusOK) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ready", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodOptions, "/v1/models", nil))

	out := buf.String()
	if strings.Contains(out, "/health") || strings.Contains(out, "/ready") || strings.Contains(out, "/metrics") {
		t.Fatalf("expected skip paths to be omitted, got %q", out)
	}
	if strings.Contains(out, "OPTIONS /v1/models") {
		t.Fatalf("expected OPTIONS to be skipped, got %q", out)
	}
}

func TestRequestContext_UsesIncomingRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(RequestContext(RequestContextOptions{AccessLogEnabled: false}))
	r.GET("/x", func(c *gin.Context) {
		if got := GetRequestID(c); got != "req-123" {
			t.Fatalf("expected request id req-123, got %q", got)
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-ID", "req-123")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("X-Request-ID") != "req-123" {
		t.Fatalf("expected propagated request id, got %q", rr.Header().Get("X-Request-ID"))
	}
}

func TestRequestContext_ErrorCountInAccessLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := configureTestLogging(t, "debug")

	r := gin.New()
	r.Use(RequestContext(RequestContextOptions{AccessLogEnabled: true}))
	r.GET("/v1/chat/completions", func(c *gin.Context) {
		_ = c.Error(http.ErrBodyNotAllowed)
		c.Status(http.StatusInternalServerError)
	})

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	out := buf.String()
	if !strings.Contains(out, "errors=1") {
		t.Fatalf("expected error count in access log, got %q", out)
	}
}
