package httptransport

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/handler"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/middleware"
)

func NewRouter(logger *slog.Logger, serviceName string) *gin.Engine {
	router := gin.New()
	router.Use(
		middleware.RequestLogger(logger),
		middleware.Recover(logger),
	)

	healthHandler := handler.NewHealth(serviceName)
	router.GET("/healthz", healthHandler.Handle)

	return router
}
