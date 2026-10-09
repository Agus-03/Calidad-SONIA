// Package http holds only what is transversal: the declared exception to rule 4.
package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Error carries an HTTP status and a stable code the frontend can switch on.
type Error struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
	cause   error
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func (e *Error) Unwrap() error { return e.cause }

// WithDetails attaches field-level information to a validation error.
func (e *Error) WithDetails(details map[string]string) *Error {
	e.Details = details
	return e
}

// WithCause keeps the cause for the log, out of the response body.
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	return e
}

func BadRequest(code, message string) *Error {
	return &Error{Status: http.StatusBadRequest, Code: code, Message: message}
}

func Unauthorized(code, message string) *Error {
	return &Error{Status: http.StatusUnauthorized, Code: code, Message: message}
}

func Forbidden(code, message string) *Error {
	return &Error{Status: http.StatusForbidden, Code: code, Message: message}
}

func NotFound(code, message string) *Error {
	return &Error{Status: http.StatusNotFound, Code: code, Message: message}
}

func Conflict(code, message string) *Error {
	return &Error{Status: http.StatusConflict, Code: code, Message: message}
}

// TooLarge is a body or a quota the request would pass: the 100 MB of a knowledge base is the first (F06).
func TooLarge(code, message string) *Error {
	return &Error{Status: http.StatusRequestEntityTooLarge, Code: code, Message: message}
}

func Unavailable(code, message string) *Error {
	return &Error{Status: http.StatusServiceUnavailable, Code: code, Message: message}
}

func InternalServerError(code, message string) *Error {
	return &Error{Status: http.StatusInternalServerError, Code: code, Message: message}
}

func Internal(err error) *Error {
	return &Error{
		Status:  http.StatusInternalServerError,
		Code:    "internal_error",
		Message: "unexpected error",
		cause:   err,
	}
}

// Fail writes err; an unrecognised one becomes a 500 and its detail goes to the log only.
func Fail(c *gin.Context, log *slog.Logger, err error) {
	if log == nil {
		log = slog.Default()
	}

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		apiErr = Internal(err)
	}

	attrs := []any{
		slog.String("code", apiErr.Code),
		slog.Int("status", apiErr.Status),
		slog.String("path", c.FullPath()),
		slog.String("request_id", RequestIDFrom(c)),
	}
	if apiErr.cause != nil {
		attrs = append(attrs, slog.String("cause", apiErr.cause.Error()))
	}
	if apiErr.Status >= http.StatusInternalServerError {
		log.Error("request failed", attrs...)
	} else {
		log.Warn("request rejected", attrs...)
	}

	c.AbortWithStatusJSON(apiErr.Status, gin.H{"error": apiErr})
}

// OK writes a successful payload.
func OK(c *gin.Context, payload any) { c.JSON(http.StatusOK, payload) }

// Text writes a body verbatim: an external callback can demand an exact string that a JSON envelope would break.
func Text(c *gin.Context, body string) { c.String(http.StatusOK, body) }

// Created writes a successful creation payload.
func Created(c *gin.Context, payload any) { c.JSON(http.StatusCreated, payload) }

// NoContent acknowledges a mutation with no body.
func NoContent(c *gin.Context) { c.Status(http.StatusNoContent) }
