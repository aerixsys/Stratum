package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stratum/gateway/internal/logging"
	"github.com/stratum/gateway/internal/schema"
	"github.com/stratum/gateway/internal/service"
)

// ChatHandler handles POST /v1/chat/completions.
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
		logging.Errorf("chat converse failed: %v", err)
		schema.MapBedrockError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *ChatHandler) handleStream(c *gin.Context, req *schema.ChatRequest) {
	start := time.Now()
	logging.StreamLog("stream_start", req.Model, 0, 0, nil)

	dataCh, err := h.svc.ConverseStream(c.Request.Context(), req)
	if err != nil {
		if writeServiceError(c, err) {
			return
		}
		durationMs := time.Since(start).Milliseconds()
		logging.Warnf("stream setup failed: %v", err)
		logging.StreamLog("stream_error", req.Model, 0, durationMs, map[string]any{"stage": "setup"})
		logging.InferenceDone(req.Model, true, "error", durationMs, 0)
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
		durationMs := time.Since(start).Milliseconds()
		logging.Warnf("response writer does not support streaming flush")
		logging.StreamLog("stream_error", req.Model, 0, durationMs, map[string]any{"stage": "flusher"})
		logging.InferenceDone(req.Model, true, "error", durationMs, 0)
		return
	}
	flusher.Flush()

	for {
		select {
		case <-c.Request.Context().Done():
			durationMs := time.Since(start).Milliseconds()
			logging.StreamLog("stream_error", req.Model, 0, durationMs, map[string]any{"stage": "client_disconnect"})
			logging.InferenceDone(req.Model, true, "error", durationMs, 0)
			return
		case data, ok := <-dataCh:
			if !ok {
				durationMs := time.Since(start).Milliseconds()
				logging.StreamLog("stream_done", req.Model, 0, durationMs, nil)
				logging.InferenceDone(req.Model, true, "ok", durationMs, 0)
				return
			}
			if _, err := c.Writer.Write(data); err != nil {
				durationMs := time.Since(start).Milliseconds()
				logging.Warnf("stream write failed: %v", err)
				logging.StreamLog("stream_error", req.Model, 0, durationMs, map[string]any{"stage": "write"})
				logging.InferenceDone(req.Model, true, "error", durationMs, 0)
				return
			}
			flusher.Flush()
		}
	}
}
