package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const requestIDHeader = "X-Request-ID"

var requestSequence atomic.Uint64

func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		requestID := c.GetHeader(requestIDHeader)
		if requestID == "" {
			requestID = fmt.Sprintf("req-%d-%d", started.UnixMilli(), requestSequence.Add(1))
		}

		c.Header(requestIDHeader, requestID)
		c.Set("request_id", requestID)
		c.Next()

		logger.Info(
			"http request completed",
			"request_id", requestID,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"bytes", c.Writer.Size(),
			"duration_ms", time.Since(started).Milliseconds(),
			"errors", c.Errors.String(),
		)
	}
}

func Recover(logger *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logger.Error(
			"http handler panic",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"error", recovered,
		)

		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": http.StatusText(http.StatusInternalServerError),
		})
	})
}
