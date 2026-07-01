package httptransport

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/handler"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/middleware"
)

type RouterDependencies struct {
	Chat      handler.ChatExecutor
	Knowledge handler.KnowledgeExecutor
	Feedback  handler.FeedbackExecutor
	Eval      handler.EvalExecutor
	Metrics   http.Handler
}

func NewRouter(logger *slog.Logger, serviceName string, dependencies RouterDependencies) *gin.Engine {
	router := gin.New()
	router.Use(
		middleware.RequestLogger(logger),
		middleware.TraceRequests(),
		middleware.Recover(logger),
	)

	healthHandler := handler.NewHealth(serviceName)
	router.GET("/healthz", healthHandler.Handle)
	if dependencies.Metrics != nil {
		router.GET("/metrics", gin.WrapH(dependencies.Metrics))
	}

	api := router.Group("/api/v1")
	chatHandler := handler.NewChat(dependencies.Chat)
	api.POST("/chat", chatHandler.Handle)

	knowledgeHandler := handler.NewKnowledge(dependencies.Knowledge)
	knowledgeAPI := api.Group("/knowledge")
	knowledgeAPI.POST("/documents", knowledgeHandler.Ingest)
	knowledgeAPI.POST("/search", knowledgeHandler.Search)
	knowledgeAPI.GET("/documents/:id", knowledgeHandler.GetDocument)

	feedbackHandler := handler.NewFeedback(dependencies.Feedback)
	api.POST("/feedback", feedbackHandler.Create)
	api.GET("/feedback/:id", feedbackHandler.Get)

	evalHandler := handler.NewEval(dependencies.Eval)
	api.POST("/eval/cases", evalHandler.Create)
	api.GET("/eval/cases", evalHandler.List)
	api.POST("/eval/runs", evalHandler.CreateRun)
	api.GET("/eval/runs/:id", evalHandler.GetRun)
	api.GET("/eval/runs/:id/results", evalHandler.ListRunResults)

	return router
}
