package handler

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
	"github.com/stratum/gateway/internal/middleware"
	"github.com/stratum/gateway/internal/schema"
	"github.com/stratum/gateway/internal/service"
)

type fakeChatRuntime struct {
	lastInput  *bedrock.ConverseInput
	resp       *schema.ChatResponse
	err        error
	streamData [][]byte
}

func (f *fakeChatRuntime) Converse(ctx context.Context, input *bedrock.ConverseInput) (*schema.ChatResponse, error) {
	f.lastInput = input
	return f.resp, f.err
}

func (f *fakeChatRuntime) ConverseStream(ctx context.Context, input *bedrock.ConverseInput) <-chan []byte {
	f.lastInput = input
	ch := make(chan []byte, len(f.streamData))
	if len(f.streamData) == 0 {
		ch <- []byte("data: [DONE]\n\n")
	} else {
		for _, d := range f.streamData {
			ch <- d
		}
	}
	close(ch)
	return ch
}

type fakeEmbeddingRuntime struct {
	lastReq *schema.EmbeddingRequest
	resp    *schema.EmbeddingResponse
	err     error
}

func (f *fakeEmbeddingRuntime) Embed(ctx context.Context, req *schema.EmbeddingRequest) (*schema.EmbeddingResponse, error) {
	f.lastReq = req
	return f.resp, f.err
}

type fakeModelDiscovery struct {
	models map[string]schema.Model
	err    error
}

func (f *fakeModelDiscovery) GetModels(ctx context.Context) ([]schema.Model, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]schema.Model, 0, len(f.models))
	for _, m := range f.models {
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeModelDiscovery) FindModel(ctx context.Context, modelID string) (*schema.Model, error) {
	if f.err != nil {
		return nil, f.err
	}
	m, ok := f.models[modelID]
	if !ok {
		return nil, nil
	}
	return &m, nil
}

func newJSONRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestChatHandler_SyncSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rt := &fakeChatRuntime{
		resp: &schema.ChatResponse{
			ID:                "chatcmpl-1",
			Object:            "chat.completion",
			Created:           1,
			Model:             "amazon.nova-micro-v1:0",
			SystemFingerprint: "fp",
			Choices: []schema.Choice{{
				Index:        0,
				Message:      &schema.ResponseMessage{Role: "assistant", Content: strPtr("hi")},
				FinishReason: strPtr("stop"),
			}},
		},
	}
	models := &fakeModelDiscovery{
		models: map[string]schema.Model{
			"amazon.nova-micro-v1:0": {ID: "amazon.nova-micro-v1:0"},
		},
	}
	h := NewChatHandler(service.NewChatService(rt, models, bedrock.TranslateConfig{}, ""))

	r := gin.New()
	r.POST("/v1/chat/completions", h.Handle)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, newJSONRequest(http.MethodPost, "/v1/chat/completions",
		`{"model":"amazon.nova-micro-v1:0","messages":[{"role":"user","content":"hello"}]}`))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var out schema.ChatResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Object != "chat.completion" {
		t.Fatalf("unexpected object: %s", out.Object)
	}
}

func TestChatHandler_StreamSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rt := &fakeChatRuntime{
		streamData: [][]byte{
			[]byte("data: {\"id\":\"chatcmpl-1\"}\n\n"),
			[]byte("data: [DONE]\n\n"),
		},
	}
	models := &fakeModelDiscovery{
		models: map[string]schema.Model{
			"amazon.nova-micro-v1:0": {ID: "amazon.nova-micro-v1:0"},
		},
	}
	h := NewChatHandler(service.NewChatService(rt, models, bedrock.TranslateConfig{}, ""))

	r := gin.New()
	r.POST("/v1/chat/completions", h.Handle)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, newJSONRequest(http.MethodPost, "/v1/chat/completions",
		`{"model":"amazon.nova-micro-v1:0","stream":true,"messages":[{"role":"user","content":"hello"}]}`))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("expected SSE content-type, got %q", rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), "data: [DONE]") {
		t.Fatalf("expected [DONE] in stream body: %s", rr.Body.String())
	}
}

func TestChatHandler_BodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rt := &fakeChatRuntime{resp: &schema.ChatResponse{Object: "chat.completion"}}
	models := &fakeModelDiscovery{
		models: map[string]schema.Model{
			"amazon.nova-micro-v1:0": {ID: "amazon.nova-micro-v1:0"},
		},
	}
	h := NewChatHandler(service.NewChatService(rt, models, bedrock.TranslateConfig{}, ""))

	r := gin.New()
	r.Use(middleware.BodyLimit(64))
	r.POST("/v1/chat/completions", h.Handle)

	large := strings.Repeat("a", 512)
	body := fmt.Sprintf(`{"model":"amazon.nova-micro-v1:0","messages":[{"role":"user","content":"%s"}]}`, large)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, newJSONRequest(http.MethodPost, "/v1/chat/completions", body))

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestEmbeddingsHandler_DefaultModelFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rt := &fakeEmbeddingRuntime{
		resp: &schema.EmbeddingResponse{
			Object: "list",
			Data: []schema.EmbeddingData{
				{Object: "embedding", Embedding: []float64{0.1, 0.2}, Index: 0},
			},
			Model: "cohere.embed-multilingual-v3",
		},
	}
	h := NewEmbeddingsHandler(service.NewEmbeddingsService(rt, "cohere.embed-multilingual-v3"))

	r := gin.New()
	r.POST("/v1/embeddings", h.Handle)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, newJSONRequest(http.MethodPost, "/v1/embeddings", `{"input":"hello"}`))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if rt.lastReq == nil || rt.lastReq.Model != "cohere.embed-multilingual-v3" {
		t.Fatalf("expected default embedding model to be applied")
	}
}

func TestEmbeddingsHandler_BodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rt := &fakeEmbeddingRuntime{resp: &schema.EmbeddingResponse{Object: "list"}}
	h := NewEmbeddingsHandler(service.NewEmbeddingsService(rt, "cohere.embed-multilingual-v3"))

	r := gin.New()
	r.Use(middleware.BodyLimit(64))
	r.POST("/v1/embeddings", h.Handle)

	large := strings.Repeat("a", 512)
	body := fmt.Sprintf(`{"model":"cohere.embed-multilingual-v3","input":"%s"}`, large)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, newJSONRequest(http.MethodPost, "/v1/embeddings", body))

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestModelsHandler_List(t *testing.T) {
	gin.SetMode(gin.TestMode)

	models := &fakeModelDiscovery{
		models: map[string]schema.Model{
			"m1": {ID: "m1", Object: "model"},
		},
	}
	h := NewModelsHandler(service.NewModelsService(models))

	r := gin.New()
	r.GET("/v1/models", h.Handle)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestModelsHandler_GetNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	models := &fakeModelDiscovery{models: map[string]schema.Model{}}
	h := NewModelsHandler(service.NewModelsService(models))

	r := gin.New()
	r.GET("/v1/models/:id", h.HandleGet)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models/missing", nil)
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHealthAndReadyHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/health", HealthHandler)
	r.GET("/ready", ReadyHandler)

	healthRR := httptest.NewRecorder()
	r.ServeHTTP(healthRR, httptest.NewRequest(http.MethodGet, "/health", nil))
	if healthRR.Code != http.StatusOK {
		t.Fatalf("health expected 200, got %d", healthRR.Code)
	}

	readyRR := httptest.NewRecorder()
	r.ServeHTTP(readyRR, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if readyRR.Code != http.StatusOK {
		t.Fatalf("ready expected 200, got %d", readyRR.Code)
	}
}

func strPtr(v string) *string { return &v }

var _ bedrock.ChatRuntime = (*fakeChatRuntime)(nil)
var _ bedrock.EmbeddingRuntime = (*fakeEmbeddingRuntime)(nil)
var _ bedrock.ModelDiscovery = (*fakeModelDiscovery)(nil)
