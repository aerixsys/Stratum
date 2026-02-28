package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/stratum/gateway/internal/logging"
	"github.com/stratum/gateway/internal/schema"
	"github.com/stratum/gateway/internal/service"
)

func writeServiceError(c *gin.Context, err error) bool {
	var svcErr *service.Error
	if !errors.As(err, &svcErr) {
		return false
	}

	switch svcErr.Kind {
	case service.ErrorBadRequest:
		schema.BadRequest(c, svcErr.Message)
	case service.ErrorNotFound:
		schema.WriteError(c, http.StatusNotFound, "not_found_error", svcErr.Message)
	default:
		logging.Errorf("internal service error: %v", err)
		schema.InternalError(c, "Internal server error")
	}
	return true
}
