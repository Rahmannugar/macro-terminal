package clustering

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func RegisterRoutes(router gin.IRoutes, service *Service) {
	router.GET("/api/v1/articles/:id/related", relatedArticles(service))
}

type sourceJSON struct {
	ID   uuid.UUID `json:"id" example:"3f8b2a60-9d1e-4a63-8f6d-2b9c1f4e7a55"`
	Name string    `json:"name" example:"Reuters"`
}

type articleJSON struct {
	ID          uuid.UUID  `json:"id" example:"9d1e4a63-8f6d-4b9c-1f4e-7a553f8b2a60"`
	Title       string     `json:"title" example:"Fed holds rates steady"`
	Content     string     `json:"content" example:"The Federal Reserve left its benchmark rate unchanged."`
	URL         string     `json:"url" example:"https://www.reuters.com/markets/us/"`
	ImageURL    *string    `json:"imageUrl" example:"https://cdn.reuters.com/photos/board-meeting.jpg"`
	PublishedAt *time.Time `json:"publishedAt"`
	Source      sourceJSON `json:"source"`
}

type relatedResponse struct {
	Articles []articleJSON `json:"articles"`
}

// @Summary Get articles related to one article.
// @Description Returns story cluster mates first (most recent first), then semantically similar indexed articles, deduplicated and excluding the article itself. When semantic search is not configured only cluster mates are returned.
// @Tags articles
// @Param id path string true "Article id (UUID)."
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Success 200 {object} relatedResponse "The related articles, cluster mates first."
// @Failure 400 {object} openapi.Error "The request was malformed."
// @Failure 404 {object} openapi.Error "The article does not exist."
// @Failure 429 {object} openapi.Error "Too many requests. Retry after the indicated delay."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Failure 503 {object} openapi.Error "Semantic search is unavailable."
// @Router /api/v1/articles/{id}/related [get]
func relatedArticles(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		articleID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_id", "The article id must be a UUID.")
			return
		}
		limit, ok := parseLimit(ctx.Query("limit"))
		if !ok {
			writeFailure(ctx, http.StatusBadRequest, "invalid_limit", "Limit must be a number between 1 and 100.")
			return
		}

		related, err := service.Related(ctx.Request.Context(), articleID, limit)
		switch {
		case errors.Is(err, ErrNotFound):
			writeFailure(ctx, http.StatusNotFound, "not_found", "The article does not exist.")
		case errors.Is(err, ErrUnavailable):
			writeFailure(ctx, http.StatusServiceUnavailable, "search_unavailable", "Semantic search is not available right now.")
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "related article lookup failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		default:
			ctx.JSON(http.StatusOK, newRelatedResponse(related))
		}
	}
}

func parseLimit(value string) (int, bool) {
	if value == "" {
		return DefaultPageSize, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > MaximumPageSize {
		return 0, false
	}
	return limit, true
}

func newRelatedResponse(articles []models.StoredArticle) relatedResponse {
	results := make([]articleJSON, 0, len(articles))
	for _, article := range articles {
		results = append(results, articleJSON{
			ID:          article.ID,
			Title:       article.Title,
			Content:     article.Content,
			URL:         article.URL,
			ImageURL:    article.ImageURL,
			PublishedAt: article.PublishedAt,
			Source: sourceJSON{
				ID:   article.SourceID,
				Name: article.SourceName,
			},
		})
	}
	return relatedResponse{Articles: results}
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
