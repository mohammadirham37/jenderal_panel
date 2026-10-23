package httputil

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type Response struct {
	Data any   `json:"data,omitempty"`
	Meta *Meta `json:"meta,omitempty"`
}

type Meta struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Response{Data: data})
}

func JSONList(w http.ResponseWriter, data any, page, perPage, total int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Response{
		Data: data,
		Meta: &Meta{Page: page, PerPage: perPage, Total: total},
	})
}

func JSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorBody{Code: code, Message: message},
	})
}

func HandleError(w http.ResponseWriter, err error) {
	var domainErr *model.DomainError
	if errors.As(err, &domainErr) {
		status := domainErrorToStatus(domainErr.Code)
		JSONError(w, status, domainErr.Code, domainErr.Message)
		return
	}
	// Non-domain errors wrap command stderr, SQL text, and file paths.
	// They are useful in the panel log, not in a client response.
	slog.Error("request failed", "error", err)
	JSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
		"the operation failed; check the panel log for details")
}

func domainErrorToStatus(code string) int {
	switch code {
	case "NOT_FOUND":
		return http.StatusNotFound
	case "INVALID_CREDENTIALS", "SESSION_EXPIRED", "UNAUTHORIZED":
		return http.StatusUnauthorized
	case "FORBIDDEN":
		return http.StatusForbidden
	case "VALIDATION_ERROR", "USER_EXISTS":
		return http.StatusBadRequest
	case "RATE_LIMITED":
		return http.StatusTooManyRequests
	case "SERVICE_NOT_ALLOWED":
		return http.StatusForbidden
	case "SERVICE_ERROR", "NGINX_CONFIG_INVALID", "SSH_WARNING":
		return http.StatusBadRequest
	case "USER_INACTIVE":
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

// MaxJSONBody caps request bodies decoded as JSON. The panel's largest
// JSON payloads are configuration files, far below this; uploads use
// multipart handlers with their own limits.
const MaxJSONBody int64 = 2 << 20 // 2 MiB

func DecodeJSON(r *http.Request, v any) error {
	// An unbounded body lets one request pull the panel's memory down,
	// including on the unauthenticated login route.
	r.Body = http.MaxBytesReader(nil, r.Body, MaxJSONBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return model.NewValidationError("request body is too large")
		}
		// Include the decoder's reason (field name, type mismatch) so the
		// caller sees which part of the body was rejected.
		return model.NewValidationError("invalid JSON body: " + err.Error())
	}
	return nil
}
