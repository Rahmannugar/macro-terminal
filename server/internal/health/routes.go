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
	Status string `json:"status" example:"ok"`
}

func RegisterRoutes(router gin.IRoutes, database Database) {
	router.GET("/health/live", live)
	router.GET("/health/ready", ready(database))
}

// @Summary Report whether the API process is alive.
// @Description Answers the process liveness probe; it succeeds as long as the API process can serve HTTP.
// @Tags health
// @Success 200 {object} response "The process is alive."
// @Failure 400 {object} openapi.Error "The request was malformed."
// @Failure 429 {object} openapi.Error "Too many requests. Retry after the indicated delay."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /health/live [get]
func live(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, response{Status: "ok"})
}

// @Summary Report whether PostgreSQL is reachable.
// @Description Answers the readiness probe; it succeeds only when PostgreSQL answers a ping within the readiness timeout, so traffic is held back while the database is unreachable.
// @Tags health
// @Success 200 {object} response "PostgreSQL answered the ping."
// @Failure 400 {object} openapi.Error "The request was malformed."
// @Failure 429 {object} openapi.Error "Too many requests. Retry after the indicated delay."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Failure 503 {object} response "PostgreSQL did not answer the ping; the instance is not ready for traffic."
// @Router /health/ready [get]
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
