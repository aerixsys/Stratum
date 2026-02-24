package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/stratum/gateway/internal/schema"
	"github.com/stratum/gateway/internal/service"
)

// EmbeddingsHandler handles POST /v1/embeddings.
type EmbeddingsHandler struct {
	svc *service.EmbeddingsService
}

// NewEmbeddingsHandler creates an embeddings handler.
func NewEmbeddingsHandler(svc *service.EmbeddingsService) *EmbeddingsHandler {
	return &EmbeddingsHandler{svc: svc}
}

// Handle processes embedding requests.
func (h *EmbeddingsHandler) Handle(c *gin.Context) {
	var req schema.EmbeddingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			schema.AbortWithError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "Request body too large")
			return
		}
		schema.BadRequest(c, "Invalid request body")
		return
	}
	c.Set("model", req.Model)

	resp, err := h.svc.Embed(c.Request.Context(), &req)
	if err != nil {
		if writeServiceError(c, err) {
			return
		}
		log.Printf("[embeddings] Embed error: %v", err)
		schema.MapBedrockError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
