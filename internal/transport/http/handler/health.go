package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Health struct {
	serviceName string
}

type healthResponse struct {
	Status  string    `json:"status"`
	Service string    `json:"service"`
	Time    time.Time `json:"time"`
}

func NewHealth(serviceName string) *Health {
	return &Health{serviceName: serviceName}
}

func (h *Health) Handle(c *gin.Context) {
	c.JSON(http.StatusOK, healthResponse{
		Status:  "ok",
		Service: h.serviceName,
		Time:    time.Now().UTC(),
	})
}
