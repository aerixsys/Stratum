package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stratum/gateway/internal/config"
	"github.com/stratum/gateway/internal/logging"
)

func TestCorsMiddleware_AllowedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(corsMiddleware())
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected wildcard allow-origin")
	}
}

func TestCorsMiddleware_AnyOriginGetsHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(corsMiddleware())
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("Origin", "https://blocked.example")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected wildcard allow-origin")
	}
}

func TestCorsMiddleware_NoOriginHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(corsMiddleware())
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected wildcard allow-origin")
	}
}

func TestCorsMiddleware_Options(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(corsMiddleware())
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodOptions, "/ok", nil)
	req.Header.Set("Origin", "https://allowed.example")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected wildcard allow-origin")
	}
}

func TestMetricsCollector(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newMetricsCollector()

	r := gin.New()
	r.Use(m.middleware())
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusCreated) })

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/ok", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", rr.Code)
		}
	}

	mr := gin.New()
	mr.GET("/metrics", m.handler)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	mr.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "stratum_requests_total 2") {
		t.Fatalf("expected total requests metric, got: %s", body)
	}
	if !strings.Contains(body, "status=\"201\"") {
		t.Fatalf("expected status label metric, got: %s", body)
	}
}

func TestMetricsNoopAndDisabledHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Next() })
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/metrics", func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "metrics disabled"})
	})

	okRR := httptest.NewRecorder()
	r.ServeHTTP(okRR, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if okRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", okRR.Code)
	}

	metricsRR := httptest.NewRecorder()
	r.ServeHTTP(metricsRR, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsRR.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", metricsRR.Code)
	}
}

func TestPrintBanner(t *testing.T) {
	var buf bytes.Buffer
	logging.SetOutput(&buf)
	logging.SetTTYMode(false)
	defer func() {
		logging.SetOutput(nil)
		logging.SetTTYMode(false)
	}()

	printBanner(&config.Config{
		Port:      "8000",
		AWSRegion: "us-east-1",
		LogLevel:  "info",
	})

	out := buf.String()
	if !strings.Contains(out, "Stratum Gateway") {
		t.Fatalf("expected banner content")
	}
	if !strings.Contains(out, "http://localhost:8000/v1") {
		t.Fatalf("expected banner API line")
	}
}
