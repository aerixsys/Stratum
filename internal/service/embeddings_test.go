package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stratum/gateway/internal/bedrock"
	"github.com/stratum/gateway/internal/schema"
)

type fakeEmbeddingRuntime struct {
	lastReq *schema.EmbeddingRequest
	resp    *schema.EmbeddingResponse
	err     error
}

func (f *fakeEmbeddingRuntime) Embed(ctx context.Context, req *schema.EmbeddingRequest) (*schema.EmbeddingResponse, error) {
	f.lastReq = req
	return f.resp, f.err
}

func TestEmbeddingsService_DefaultModel(t *testing.T) {
	rt := &fakeEmbeddingRuntime{resp: &schema.EmbeddingResponse{Object: "list"}}
	svc := NewEmbeddingsService(rt, "cohere.embed-multilingual-v3")

	req := &schema.EmbeddingRequest{
		Input: json.RawMessage(`"hello"`),
	}
	_, err := svc.Embed(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.lastReq == nil {
		t.Fatal("expected embedding request")
	}
	if rt.lastReq.Model != "cohere.embed-multilingual-v3" {
		t.Fatalf("expected fallback model, got %s", rt.lastReq.Model)
	}
}

func TestEmbeddingsService_EmptyInput(t *testing.T) {
	rt := &fakeEmbeddingRuntime{}
	svc := NewEmbeddingsService(rt, "cohere.embed-multilingual-v3")
	_, err := svc.Embed(context.Background(), &schema.EmbeddingRequest{
		Model: "cohere.embed-multilingual-v3",
		Input: json.RawMessage(`[]`),
	})
	if err == nil {
		t.Fatal("expected bad request error")
	}
	var svcErr *Error
	if !asServiceError(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
}

func TestEmbeddingsService_NilRequest(t *testing.T) {
	svc := NewEmbeddingsService(&fakeEmbeddingRuntime{}, "cohere.embed-multilingual-v3")
	_, err := svc.Embed(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !asServiceError(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
}

func TestEmbeddingsService_ModelRequiredWithoutFallback(t *testing.T) {
	svc := NewEmbeddingsService(&fakeEmbeddingRuntime{}, "")
	_, err := svc.Embed(context.Background(), &schema.EmbeddingRequest{
		Input: json.RawMessage(`"hello"`),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var svcErr *Error
	if !asServiceError(err, &svcErr) || svcErr.Kind != ErrorBadRequest {
		t.Fatalf("expected bad request service error, got %v", err)
	}
}

func TestEmbeddingsService_RuntimeErrorPassThrough(t *testing.T) {
	rt := &fakeEmbeddingRuntime{err: errors.New("embed failed")}
	svc := NewEmbeddingsService(rt, "cohere.embed-multilingual-v3")
	_, err := svc.Embed(context.Background(), &schema.EmbeddingRequest{
		Model: "cohere.embed-multilingual-v3",
		Input: json.RawMessage(`"hello"`),
	})
	if err == nil || err.Error() != "embed failed" {
		t.Fatalf("expected runtime error passthrough, got %v", err)
	}
}

var _ bedrock.EmbeddingRuntime = (*fakeEmbeddingRuntime)(nil)
