package core_http_request

import (
	"fmt"

	core_errors "github.com/vasya2314/golang-kp/internal/core/errors"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func NewFieldError(field, message string) FieldError {
	return FieldError{Field: field, Message: message}
}

type ValidationError struct {
	Fields []FieldError
}

func NewValidationError(fields []FieldError) *ValidationError {
	return &ValidationError{Fields: fields}
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("ошибка валидации: полей с ошибками — %d", len(e.Fields))
}

func (e *ValidationError) Unwrap() error {
	return core_errors.ErrInvalidArgument
}
