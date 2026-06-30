package httptransport

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/handler"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/middleware"
)

type RouterDependencies struct {
	Chat handler.ChatExecutor
}

func NewRouter(logger *slog.Logger, serviceName string, dependencies RouterDependencies) *gin.Engine {
	router := gin.New()
	router.Use(
		middleware.RequestLogger(logger),
		middleware.Recover(logger),
	)

	healthHandler := handler.NewHealth(serviceName)
	router.GET("/healthz", healthHandler.Handle)

	api := router.Group("/api/v1")
	chatHandler := handler.NewChat(dependencies.Chat)
	api.POST("/chat", chatHandler.Handle)

	return router
}
