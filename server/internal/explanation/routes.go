package explanation

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func RegisterRoutes(router gin.IRoutes, service *Service) {
	router.POST("/api/v1/articles/:id/explain", explainArticle(service))
	router.POST("/api/v1/calendar-events/:id/explain", explainCalendarEvent(service))
}

type explanationJSON struct {
	Text string `json:"text" example:"The Federal Reserve held its benchmark rate unchanged, keeping policy restrictive as inflation eases slowly."`
}

type explanationResponse struct {
	Explanation explanationJSON `json:"explanation"`
}

// @Summary Explain one article with AI.
// @Description Generates a one-shot contextual explanation of the article from its enrichment, graph entities, affected asset pairs, story cluster, related coverage, and knowledge terms. The generated answer is cached briefly and regenerated after it expires.
// @Tags articles
// @Param id path string true "Article id (UUID)."
// @Success 200 {object} explanationResponse "The explanation."
// @Failure 400 {object} openapi.Error "The request was malformed."
// @Failure 404 {object} openapi.Error "The article does not exist."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Failure 503 {object} openapi.Error "The explanation service is unavailable."
// @Router /api/v1/articles/{id}/explain [post]
func explainArticle(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		articleID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_id", "The article id must be a UUID.")
			return
		}
		response, err := service.ExplainArticle(ctx.Request.Context(), articleID)
		switch {
		case errors.Is(err, ErrNotFound):
			writeFailure(ctx, http.StatusNotFound, "not_found", "The article does not exist.")
		case errors.Is(err, ErrUnavailable):
			slog.Default().ErrorContext(ctx.Request.Context(), "article explanation failed", "error", err)
			writeFailure(ctx, http.StatusServiceUnavailable, "explanation_unavailable", "The explanation service is not available right now.")
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "article explanation failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		default:
			ctx.JSON(http.StatusOK, response)
		}
	}
}

// @Summary Explain one calendar event with AI.
// @Description Generates a one-shot contextual explanation of the calendar event from its indicator, values, linked entities, affected asset pairs, and recent related coverage. The generated answer is cached briefly and regenerated after it expires.
// @Tags calendar
// @Param id path string true "Calendar event id (UUID)."
// @Success 200 {object} explanationResponse "The explanation."
// @Failure 400 {object} openapi.Error "The request was malformed."
// @Failure 404 {object} openapi.Error "The calendar event does not exist."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Failure 503 {object} openapi.Error "The explanation service is unavailable."
// @Router /api/v1/calendar-events/{id}/explain [post]
func explainCalendarEvent(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		eventID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_id", "The calendar event id must be a UUID.")
			return
		}
		response, err := service.ExplainEvent(ctx.Request.Context(), eventID)
		switch {
		case errors.Is(err, ErrNotFound):
			writeFailure(ctx, http.StatusNotFound, "not_found", "The calendar event does not exist.")
		case errors.Is(err, ErrUnavailable):
			slog.Default().ErrorContext(ctx.Request.Context(), "calendar event explanation failed", "error", err)
			writeFailure(ctx, http.StatusServiceUnavailable, "explanation_unavailable", "The explanation service is not available right now.")
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "calendar event explanation failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		default:
			ctx.JSON(http.StatusOK, response)
		}
	}
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
