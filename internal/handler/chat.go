package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
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
	startedAt := time.Now()
	status := "ok"
	totalTokens := 0
	defer func() {
		logging.InferenceDone(req.Model, false, status, time.Since(startedAt).Milliseconds(), totalTokens)
	}()

	resp, err := h.svc.Converse(c.Request.Context(), req)
	if err != nil {
		status = "error"
		if writeServiceError(c, err) {
			return
		}
		logging.Errorf("chat converse failed: %v", err)
		schema.MapBedrockError(c, err)
		return
	}
	if resp != nil && resp.Usage != nil && resp.Usage.TotalTokens > 0 {
		totalTokens = resp.Usage.TotalTokens
	}
	c.JSON(http.StatusOK, resp)
}

func (h *ChatHandler) handleStream(c *gin.Context, req *schema.ChatRequest) {
	startedAt := time.Now()
	status := "ok"
	totalTokens := 0
	defer func() {
		logging.InferenceDone(req.Model, true, status, time.Since(startedAt).Milliseconds(), totalTokens)
	}()

	logging.StreamLog("stream_start", req.Model, 0, 0, nil)
	sawTerminalErrorChunk := false

	dataCh, err := h.svc.ConverseStream(c.Request.Context(), req)
	if err != nil {
		status = "error"
		if writeServiceError(c, err) {
			return
		}
		durationMs := time.Since(startedAt).Milliseconds()
		logging.Warnf("stream setup failed: %v", err)
		logging.StreamLog("stream_error", req.Model, 0, durationMs, map[string]any{"stage": "setup"})
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
		status = "error"
		durationMs := time.Since(startedAt).Milliseconds()
		logging.Warnf("response writer does not support streaming flush")
		logging.StreamLog("stream_error", req.Model, 0, durationMs, map[string]any{"stage": "flusher"})
		return
	}
	flusher.Flush()

	for {
		select {
		case <-c.Request.Context().Done():
			status = "error"
			durationMs := time.Since(startedAt).Milliseconds()
			logging.StreamLog("stream_error", req.Model, 0, durationMs, map[string]any{"stage": "client_disconnect"})
			return
		case data, ok := <-dataCh:
			if !ok {
				durationMs := time.Since(startedAt).Milliseconds()
				if sawTerminalErrorChunk || status == "error" {
					status = "error"
					logging.StreamLog("stream_error", req.Model, int64(totalTokens), durationMs, map[string]any{"stage": "terminal_error"})
				} else {
					logging.StreamLog("stream_done", req.Model, int64(totalTokens), durationMs, nil)
				}
				return
			}
			parsedTokens, hasErrorChunk := parseStreamLogSignal(data)
			if parsedTokens > 0 {
				totalTokens = parsedTokens
			}
			if hasErrorChunk {
				sawTerminalErrorChunk = true
				status = "error"
			}
			if _, err := c.Writer.Write(data); err != nil {
				status = "error"
				durationMs := time.Since(startedAt).Milliseconds()
				logging.Warnf("stream write failed: %v", err)
				logging.StreamLog("stream_error", req.Model, int64(totalTokens), durationMs, map[string]any{"stage": "write"})
				return
			}
			flusher.Flush()
		}
	}
}

type streamSignalProbe struct {
	Usage *struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	Error json.RawMessage `json:"error"`
}

func parseStreamLogSignal(chunk []byte) (tokens int, hasError bool) {
	lines := strings.Split(string(chunk), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var probe streamSignalProbe
		if err := json.Unmarshal([]byte(payload), &probe); err != nil {
			continue
		}
		if probe.Usage != nil && probe.Usage.TotalTokens > 0 {
			tokens = probe.Usage.TotalTokens
		}
		rawErr := bytes.TrimSpace(probe.Error)
		if len(rawErr) > 0 && string(rawErr) != "null" {
			hasError = true
		}
	}
	return tokens, hasError
}
