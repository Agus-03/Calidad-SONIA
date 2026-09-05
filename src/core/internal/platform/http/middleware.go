package http

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

const (
	requestIDKey    = "sonia.request_id"
	requestIDHeader = "X-Request-Id"
)

// RequestID ties a log line to a failure an operator reports.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(requestIDHeader)
		if rid == "" {
			rid = id.New().String()
		}
		c.Set(requestIDKey, rid)
		c.Header(requestIDHeader, rid)
		c.Next()
	}
}

// RequestIDFrom returns the identifier stamped by RequestID.
func RequestIDFrom(c *gin.Context) string {
	if v, ok := c.Get(requestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// AccessLog logs one line per request, latency in ms to match C03.
func AccessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()

		log.Info("request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("latency_ms", time.Since(started).Milliseconds()),
			slog.String("request_id", RequestIDFrom(c)),
		)
	}
}

// Recovery turns a panic into a 500 without taking the process down.
func Recovery(log *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		log.Error("panic recovered",
			slog.Any("panic", recovered),
			slog.String("path", c.Request.URL.Path),
			slog.String("request_id", RequestIDFrom(c)),
		)
		Fail(c, log, Internal(nil))
	})
}
