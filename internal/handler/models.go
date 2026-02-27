package handler

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/stratum/gateway/internal/schema"
	"github.com/stratum/gateway/internal/service"
)

// ModelsHandler handles GET /v1/models.
type ModelsHandler struct {
	svc *service.ModelsService
}

// NewModelsHandler creates a models handler.
func NewModelsHandler(svc *service.ModelsService) *ModelsHandler {
	return &ModelsHandler{svc: svc}
}

// Handle returns the list of available models.
func (h *ModelsHandler) Handle(c *gin.Context) {
	models, err := h.svc.List(c.Request.Context())
	if err != nil {
		if writeServiceError(c, err) {
			return
		}
		log.Printf("[models] list error: %v", err)
		schema.InternalError(c, "Failed to list models")
		return
	}

	c.JSON(http.StatusOK, schema.ModelList{
		Object: "list",
		Data:   models,
	})
}

// HandleGet returns a single model by ID.
func (h *ModelsHandler) HandleGet(c *gin.Context) {
	id := c.Param("id")
	model, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		if writeServiceError(c, err) {
			return
		}
		log.Printf("[models] get error: %v", err)
		schema.InternalError(c, "Failed to get model")
		return
	}
	c.JSON(http.StatusOK, model)
}
