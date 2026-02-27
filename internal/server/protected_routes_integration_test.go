package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stratum/gateway/internal/bedrock"
	"github.com/stratum/gateway/internal/config"
	"github.com/stratum/gateway/internal/handler"
	"github.com/stratum/gateway/internal/middleware"
	"github.com/stratum/gateway/internal/schema"
	"github.com/stratum/gateway/internal/service"
)

type integrationChatRuntime struct{}

func (integrationChatRuntime) Converse(ctx context.Context, input *bedrock.ConverseInput) (*schema.ChatResponse, error) {
	respText := "ok"
	finish := "stop"
	return &schema.ChatResponse{
		ID:                "chatcmpl-integration",
		Object:            "chat.completion",
		Created:           1700000000,
		Model:             input.ModelID,
		SystemFingerprint: "fp",
		Choices: []schema.Choice{{
			Index:        0,
			Message:      &schema.ResponseMessage{Role: "assistant", Content: &respText},
			FinishReason: &finish,
		}},
	}, nil
}

func (integrationChatRuntime) ConverseStream(ctx context.Context, input *bedrock.ConverseInput) <-chan []byte {
	ch := make(chan []byte, 2)
	ch <- []byte("data: {\"id\":\"chatcmpl-stream\"}\n\n")
	ch <- []byte("data: [DONE]\n\n")
	close(ch)
	return ch
}

type integrationModelDiscovery struct {
	models map[string]schema.Model
}

func (m *integrationModelDiscovery) GetModels(ctx context.Context) ([]schema.Model, error) {
	out := make([]schema.Model, 0, len(m.models))
	for _, model := range m.models {
		out = append(out, model)
	}
	return out, nil
}

func (m *integrationModelDiscovery) FindModel(ctx context.Context, modelID string) (*schema.Model, error) {
	model, ok := m.models[strings.TrimSpace(modelID)]
	if !ok {
		return nil, nil
	}
	return &model, nil
}

type integrationModelPolicy struct {
	blocked map[string]bool
}

func (p *integrationModelPolicy) IsBlocked(modelID string) bool {
	if p == nil {
		return false
	}
	return p.blocked[strings.TrimSpace(modelID)]
}

func buildProtectedTestRouter(cfg *config.Config) *gin.Engine {
	return buildProtectedTestRouterWithPolicy(cfg, nil)
}

func buildProtectedTestRouterWithPolicy(cfg *config.Config, modelPolicy service.ModelPolicy) *gin.Engine {
	gin.SetMode(gin.TestMode)

	modelsDiscovery := &integrationModelDiscovery{models: map[string]schema.Model{
		"amazon.nova-micro-v1:0": {
			ID:      "amazon.nova-micro-v1:0",
			Object:  "model",
			Created: 1700000000,
			OwnedBy: "bedrock",
		},
		"anthropic.claude-3-sonnet-20240229-v1:0": {
			ID:      "anthropic.claude-3-sonnet-20240229-v1:0",
			Object:  "model",
			Created: 1700000000,
			OwnedBy: "bedrock",
		},
	}}

	chatHandler := handler.NewChatHandler(service.NewChatService(
		integrationChatRuntime{},
		modelsDiscovery,
		modelPolicy,
	))
	modelsHandler := handler.NewModelsHandler(service.NewModelsService(modelsDiscovery, modelPolicy))

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.RequestID())
	router.Use(requestLogger("error"))
	router.Use(corsMiddleware())
	router.Use(middleware.BodyLimit(cfg.MaxRequestBodyBytes))

	v1 := router.Group("/v1")
	v1.Use(middleware.APIKeyAuth(cfg.APIKey))
	{
		v1.GET("/models", modelsHandler.Handle)
		v1.GET("/models/:id", modelsHandler.HandleGet)
		v1.POST("/chat/completions", chatHandler.Handle)
	}

	return router
}

func TestProtectedRoutes_Auth(t *testing.T) {
	cfg := &config.Config{
		APIKey:              "test-key",
		MaxRequestBodyBytes: 1024 * 1024,
	}
	router := buildProtectedTestRouter(cfg)

	tests := []struct {
		name         string
		path         string
		authHeader   string
		expectStatus int
	}{
		{name: "missing auth v1", path: "/v1/models", expectStatus: http.StatusUnauthorized},
		{name: "malformed auth", path: "/v1/models", authHeader: "Bearer", expectStatus: http.StatusUnauthorized},
		{name: "wrong auth", path: "/v1/models", authHeader: "Bearer wrong", expectStatus: http.StatusUnauthorized},
		{name: "valid auth", path: "/v1/models", authHeader: "Bearer test-key", expectStatus: http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.RemoteAddr = "203.0.113.10:4444"
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tc.expectStatus {
				t.Fatalf("expected status %d got %d body=%s", tc.expectStatus, rr.Code, rr.Body.String())
			}
			if tc.expectStatus == http.StatusUnauthorized {
				assertErrorType(t, rr.Body.Bytes(), "invalid_api_key")
			}
		})
	}
}

func TestProtectedRoutes_CORSPreflightBeforeAuth(t *testing.T) {
	cfg := &config.Config{
		APIKey:              "test-key",
		MaxRequestBodyBytes: 1024 * 1024,
	}
	router := buildProtectedTestRouter(cfg)

	req := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
	req.RemoteAddr = "203.0.113.11:4444"
	req.Header.Set("Origin", "https://app.example.com")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 preflight, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected wildcard allow origin header for preflight")
	}
}

func TestProtectedRoutes_BodyLimitBeforeBind(t *testing.T) {
	cfg := &config.Config{
		APIKey:              "test-key",
		MaxRequestBodyBytes: 128,
	}
	router := buildProtectedTestRouter(cfg)

	large := strings.Repeat("a", 1024)
	body := fmt.Sprintf(`{"model":"amazon.nova-micro-v1:0","messages":[{"role":"user","content":"%s"}]}`, large)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.RemoteAddr = "203.0.113.12:4444"
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%s", rr.Code, rr.Body.String())
	}
	assertErrorType(t, rr.Body.Bytes(), "invalid_request_error")
}

func TestProtectedRoutes_RequestIDPropagationAndErrorEnvelope(t *testing.T) {
	cfg := &config.Config{
		APIKey:              "test-key",
		MaxRequestBodyBytes: 1024 * 1024,
	}
	router := buildProtectedTestRouter(cfg)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"unknown-model","messages":[{"role":"user","content":"hello"}]}`,
	))
	req.RemoteAddr = "203.0.113.15:4444"
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-integration-123")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Header().Get("X-Request-ID") != "req-integration-123" {
		t.Fatalf("expected request id to propagate")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported model, got %d body=%s", rr.Code, rr.Body.String())
	}
	assertErrorType(t, rr.Body.Bytes(), "invalid_request_error")

	var parsed map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("expected valid JSON envelope, got err=%v body=%s", err, rr.Body.String())
	}
	errObj := parsed["error"].(map[string]interface{})
	msg, _ := errObj["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "unsupported model") {
		t.Fatalf("expected unsupported model message, got %q", msg)
	}
}

func TestProtectedRoutes_ProfileLikeModelRejected(t *testing.T) {
	cfg := &config.Config{
		APIKey:              "test-key",
		MaxRequestBodyBytes: 1024 * 1024,
	}
	router := buildProtectedTestRouter(cfg)

	path := "/v1/chat/completions"
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(
		`{"model":"us.profile.system","messages":[{"role":"user","content":"hello"}]}`,
	))
	req.RemoteAddr = "203.0.113.23:4444"
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for profile-like model on %s, got %d body=%s", path, rr.Code, rr.Body.String())
	}
	assertErrorType(t, rr.Body.Bytes(), "invalid_request_error")
	if !strings.Contains(strings.ToLower(rr.Body.String()), "unsupported model") {
		t.Fatalf("expected unsupported-model message on %s, got %s", path, rr.Body.String())
	}
}

func TestProtectedRoutes_ModelRequired(t *testing.T) {
	cfg := &config.Config{
		APIKey:              "test-key",
		MaxRequestBodyBytes: 1024 * 1024,
	}
	router := buildProtectedTestRouter(cfg)

	path := "/v1/chat/completions"
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(
		`{"messages":[{"role":"user","content":"hello"}]}`,
	))
	req.RemoteAddr = "203.0.113.16:4444"
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing model on %s, got %d body=%s", path, rr.Code, rr.Body.String())
	}
	assertErrorType(t, rr.Body.Bytes(), "invalid_request_error")

	var parsed map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("expected valid JSON envelope on %s, got err=%v body=%s", path, err, rr.Body.String())
	}
	errObj, _ := parsed["error"].(map[string]interface{})
	msg, _ := errObj["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "model is required") {
		t.Fatalf("expected model-required message on %s, got %q", path, msg)
	}
}

func TestProtectedRoutes_ModelPolicyBlocksAcrossEndpoints(t *testing.T) {
	cfg := &config.Config{
		APIKey:              "test-key",
		MaxRequestBodyBytes: 1024 * 1024,
	}
	policy := &integrationModelPolicy{
		blocked: map[string]bool{
			"anthropic.claude-3-sonnet-20240229-v1:0": true,
		},
	}
	router := buildProtectedTestRouterWithPolicy(cfg, policy)

	assertModelMissingFromList := func(t *testing.T, path string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "203.0.113.20:4444"
		req.Header.Set("Authorization", "Bearer test-key")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload struct {
			Data []schema.Model `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("expected JSON body, got err=%v body=%s", err, rr.Body.String())
		}
		for _, model := range payload.Data {
			if model.ID == "anthropic.claude-3-sonnet-20240229-v1:0" {
				t.Fatalf("blocked model unexpectedly exposed in %s response", path)
			}
		}
	}

	assertModelMissingFromList(t, "/v1/models")

	path := "/v1/models/anthropic.claude-3-sonnet-20240229-v1:0"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "203.0.113.21:4444"
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for blocked model get %s, got %d body=%s", path, rr.Code, rr.Body.String())
	}
	assertErrorType(t, rr.Body.Bytes(), "not_found_error")

	path = "/v1/chat/completions"
	req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(
		`{"model":"anthropic.claude-3-sonnet-20240229-v1:0","messages":[{"role":"user","content":"hello"}]}`,
	))
	req.RemoteAddr = "203.0.113.22:4444"
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for blocked chat model %s, got %d body=%s", path, rr.Code, rr.Body.String())
	}
	assertErrorType(t, rr.Body.Bytes(), "invalid_request_error")
	if !strings.Contains(strings.ToLower(rr.Body.String()), "blocked by policy") {
		t.Fatalf("expected blocked-by-policy message for %s, got %s", path, rr.Body.String())
	}

}

func assertErrorType(t *testing.T, body []byte, want string) {
	t.Helper()
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("expected JSON error envelope, got err=%v body=%s", err, string(body))
	}
	errObj, ok := parsed["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object in envelope, got %v", parsed)
	}
	got, _ := errObj["type"].(string)
	if got != want {
		t.Fatalf("expected error type %q got %q body=%s", want, got, string(body))
	}
}

var _ bedrock.ChatRuntime = integrationChatRuntime{}
var _ bedrock.ModelDiscovery = (*integrationModelDiscovery)(nil)
var _ service.ModelPolicy = (*integrationModelPolicy)(nil)
