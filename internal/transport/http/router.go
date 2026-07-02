package httptransport

import (
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/handler"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/middleware"
	webui "github.com/jiawei-wang-dev/WatchOps-Lite/web"
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
	router.GET("/", serveConsoleIndex)
	router.GET("/web/*filepath", serveConsoleAsset)
	if dependencies.Metrics != nil {
		router.GET("/metrics", gin.WrapH(dependencies.Metrics))
	}

	api := router.Group("/api/v1")
	chatHandler := handler.NewChat(dependencies.Chat)
	api.POST("/chat", chatHandler.Handle)
	api.POST("/chat/stream", chatHandler.Stream)

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

func serveConsoleIndex(c *gin.Context) {
	serveEmbeddedConsoleFile(c, "index.html", "text/html; charset=utf-8")
}

func serveConsoleAsset(c *gin.Context) {
	filePath := strings.TrimPrefix(c.Param("filepath"), "/")
	filePath = path.Clean(filePath)
	if filePath == "." || strings.HasPrefix(filePath, "../") {
		c.Status(http.StatusNotFound)
		return
	}
	contentType := mime.TypeByExtension(path.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	serveEmbeddedConsoleFile(c, filePath, contentType)
}

func serveEmbeddedConsoleFile(c *gin.Context, filePath, contentType string) {
	content, err := webui.Files.ReadFile(filePath)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, contentType, content)
}
