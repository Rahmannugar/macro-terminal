package health

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const readinessTimeout = 2 * time.Second

type Database interface {
	Ping(context.Context) error
}

type response struct {
	Status string `json:"status"`
}

func RegisterRoutes(router gin.IRoutes, database Database) {
	router.GET("/health/live", live)
	router.GET("/health/ready", ready(database))
}

func live(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, response{Status: "ok"})
}

func ready(database Database) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		checkContext, cancel := context.WithTimeout(ctx.Request.Context(), readinessTimeout)
		defer cancel()

		if err := database.Ping(checkContext); err != nil {
			ctx.JSON(http.StatusServiceUnavailable, response{Status: "unavailable"})
			return
		}

		ctx.JSON(http.StatusOK, response{Status: "ok"})
	}
}
