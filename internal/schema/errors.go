package schema

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/gin-gonic/gin"
)

// ErrorResponse matches OpenAI's error format.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    *string `json:"code"`
}

// WriteError sends an OpenAI-style JSON error response.
func WriteError(c *gin.Context, status int, errType, message string) {
	c.Set("error_type", errType)
	c.JSON(status, ErrorResponse{
		Error: ErrorDetail{
			Message: message,
			Type:    errType,
			Param:   nil,
			Code:    nil,
		},
	})
}

// AbortWithError sends an error and aborts the request chain.
func AbortWithError(c *gin.Context, status int, errType, message string) {
	c.Set("error_type", errType)
	c.AbortWithStatusJSON(status, ErrorResponse{
		Error: ErrorDetail{
			Message: message,
			Type:    errType,
			Param:   nil,
			Code:    nil,
		},
	})
}

// Common error helpers.
func Unauthorized(c *gin.Context, msg string) {
	AbortWithError(c, http.StatusUnauthorized, "invalid_api_key", msg)
}

func BadRequest(c *gin.Context, msg string) {
	WriteError(c, http.StatusBadRequest, "invalid_request_error", msg)
}

func InternalError(c *gin.Context, msg string) {
	WriteError(c, http.StatusInternalServerError, "server_error", msg)
}

// MapBedrockError maps AWS Bedrock errors to OpenAI-compatible HTTP status and error type.
func MapBedrockError(c *gin.Context, err error) {
	var throttle *types.ThrottlingException
	var validation *types.ValidationException
	var accessDenied *types.AccessDeniedException
	var notFound *types.ResourceNotFoundException
	var serviceUnavail *types.ServiceUnavailableException
	var timeout *types.ModelTimeoutException

	switch {
	case errors.As(err, &throttle):
		WriteError(c, http.StatusTooManyRequests, "rate_limit_error", cleanMsg(throttle.ErrorMessage()))
	case errors.As(err, &validation):
		WriteError(c, http.StatusBadRequest, "invalid_request_error", cleanMsg(validation.ErrorMessage()))
	case errors.As(err, &accessDenied):
		WriteError(c, http.StatusForbidden, "permission_error", cleanMsg(accessDenied.ErrorMessage()))
	case errors.As(err, &notFound):
		WriteError(c, http.StatusNotFound, "not_found_error", cleanMsg(notFound.ErrorMessage()))
	case errors.As(err, &serviceUnavail):
		WriteError(c, http.StatusServiceUnavailable, "server_error", cleanMsg(serviceUnavail.ErrorMessage()))
	case errors.As(err, &timeout):
		WriteError(c, http.StatusGatewayTimeout, "timeout_error", cleanMsg(timeout.ErrorMessage()))
	default:
		// Check for common error patterns in the message
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "throttl") || strings.Contains(msg, "rate") {
			WriteError(c, http.StatusTooManyRequests, "rate_limit_error", "Rate limit exceeded")
		} else {
			InternalError(c, "Upstream model invocation failed")
		}
	}
}

func cleanMsg(msg string) string {
	if msg == "" {
		return "An error occurred"
	}
	return msg
}
