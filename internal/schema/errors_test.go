package schema

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/gin-gonic/gin"
)

func newTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, rr
}

func TestWriteError(t *testing.T) {
	c, rr := newTestContext()
	WriteError(c, http.StatusBadRequest, "invalid_request_error", "bad input")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if et, ok := c.Get("error_type"); !ok || et != "invalid_request_error" {
		t.Fatalf("expected error_type context set")
	}
}

func TestAbortWithError(t *testing.T) {
	c, rr := newTestContext()
	AbortWithError(c, http.StatusUnauthorized, "invalid_api_key", "bad key")

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if !c.IsAborted() {
		t.Fatalf("expected context aborted")
	}
}

func TestErrorHelpers(t *testing.T) {
	c1, rr1 := newTestContext()
	Unauthorized(c1, "no key")
	if rr1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr1.Code)
	}

	c2, rr2 := newTestContext()
	BadRequest(c2, "bad")
	if rr2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr2.Code)
	}
}

func TestMapBedrockError_Throttling(t *testing.T) {
	c, rr := newTestContext()
	MapBedrockError(c, &brtypes.ThrottlingException{Message: aws.String("slow down")})

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
}

func TestMapBedrockError_DefaultRatePattern(t *testing.T) {
	c, rr := newTestContext()
	MapBedrockError(c, errors.New("provider rate limit exceeded"))

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
}

func TestMapBedrockError_DefaultInternal(t *testing.T) {
	c, rr := newTestContext()
	MapBedrockError(c, errors.New("unexpected upstream blowup"))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}
