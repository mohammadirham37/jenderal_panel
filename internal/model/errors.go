package model

import "fmt"

type DomainError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

func (e *DomainError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *DomainError) Unwrap() error {
	return e.Err
}

var (
	ErrNotFound           = &DomainError{Code: "NOT_FOUND", Message: "resource not found"}
	ErrInvalidCredentials = &DomainError{Code: "INVALID_CREDENTIALS", Message: "invalid username or password"}
	ErrUserExists         = &DomainError{Code: "USER_EXISTS", Message: "user already exists"}
	ErrSessionExpired     = &DomainError{Code: "SESSION_EXPIRED", Message: "session has expired"}
	ErrUnauthorized       = &DomainError{Code: "UNAUTHORIZED", Message: "authentication required"}
	ErrForbidden          = &DomainError{Code: "FORBIDDEN", Message: "insufficient permissions"}
	ErrValidation         = &DomainError{Code: "VALIDATION_ERROR", Message: "validation failed"}
	ErrRateLimited        = &DomainError{Code: "RATE_LIMITED", Message: "too many requests"}
	ErrServiceNotAllowed  = &DomainError{Code: "SERVICE_NOT_ALLOWED", Message: "service not in allowed list"}
	ErrUserInactive       = &DomainError{Code: "USER_INACTIVE", Message: "user account is inactive"}
)

func NewDomainError(code, message string, err error) *DomainError {
	return &DomainError{Code: code, Message: message, Err: err}
}

func NewValidationError(message string) *DomainError {
	return &DomainError{Code: "VALIDATION_ERROR", Message: message}
}
