package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/stratum/gateway/internal/schema"
	"github.com/stratum/gateway/internal/service"
)

// ChatHandler handles POST /v1/chat/completions and /api/v1/chat/completions.
type ChatHandler struct {
	svc *service.ChatService
}

// NewChatHandler creates a chat handler.
func NewChatHandler(svc *service.ChatService) *ChatHandler {
	return &ChatHandler{svc: svc}
}

// Handle processes chat completion requests.
func (h *ChatHandler) Handle(c *gin.Context) {
	var req schema.ChatRequest
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

	if req.Stream {
		h.handleStream(c, &req)
	} else {
		h.handleSync(c, &req)
	}
}

func (h *ChatHandler) handleSync(c *gin.Context, req *schema.ChatRequest) {
	resp, err := h.svc.Converse(c.Request.Context(), req)
	if err != nil {
		if writeServiceError(c, err) {
			return
		}
		log.Printf("[chat] Converse error: %v", err)
		schema.MapBedrockError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *ChatHandler) handleStream(c *gin.Context, req *schema.ChatRequest) {
	dataCh, err := h.svc.ConverseStream(c.Request.Context(), req)
	if err != nil {
		if writeServiceError(c, err) {
			return
		}
		log.Printf("[stream] ConverseStream setup error: %v", err)
		schema.MapBedrockError(c, err)
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		log.Printf("[stream] ResponseWriter does not support Flusher")
		return
	}
	flusher.Flush()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case data, ok := <-dataCh:
			if !ok {
				return
			}
			_, _ = c.Writer.Write(data)
			flusher.Flush()
		}
	}
}
