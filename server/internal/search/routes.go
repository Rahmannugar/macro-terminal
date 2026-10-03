package search

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
	router.GET("/api/v1/search/articles", searchArticles(service))
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
	PublishedAt *time.Time `json:"publishedAt"`
	Source      sourceJSON `json:"source"`
}

type searchResponse struct {
	Articles   []articleJSON `json:"articles"`
	NextCursor *string       `json:"nextCursor"`
}

// @Summary Search articles by meaning.
// @Description Embeds the query text, retrieves the closest indexed article ids from the vector index, and hydrates them from PostgreSQL in relevance order. Pass the returned nextCursor to fetch the next page.
// @Tags search
// @Param q query string true "Search text, 1 to 500 characters."
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Cursor returned by the previous page."
// @Success 200 {object} searchResponse "The matching articles, closest first."
// @Failure 400 {object} openapi.Error "The request was malformed."
// @Failure 429 {object} openapi.Error "Too many requests. Retry after the indicated delay."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Failure 503 {object} openapi.Error "Semantic search is unavailable."
// @Router /api/v1/search/articles [get]
func searchArticles(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		cursor, err := DecodeCursor(ctx.Query("cursor"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_cursor", "Use the next cursor returned by the previous page.")
			return
		}
		limit, ok := parseLimit(ctx.Query("limit"))
		if !ok {
			writeFailure(ctx, http.StatusBadRequest, "invalid_limit", "Limit must be a number between 1 and 100.")
			return
		}

		articles, next, err := service.Search(ctx.Request.Context(), ctx.Query("q"), limit, cursor)
		switch {
		case errors.Is(err, ErrQueryInvalid):
			writeFailure(ctx, http.StatusBadRequest, "invalid_query", "Search text must be 1 to 500 characters.")
		case errors.Is(err, ErrWindowExhausted):
			writeFailure(ctx, http.StatusBadRequest, "invalid_cursor", "The cursor points past the first 200 results. Refine the query.")
		case errors.Is(err, ErrUnavailable):
			writeFailure(ctx, http.StatusServiceUnavailable, "search_unavailable", "Semantic search is not available right now.")
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "search failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		default:
			ctx.JSON(http.StatusOK, newSearchResponse(articles, next))
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

func newSearchResponse(articles []models.StoredArticle, next *Cursor) searchResponse {
	results := make([]articleJSON, 0, len(articles))
	for _, article := range articles {
		results = append(results, articleJSON{
			ID:          article.ID,
			Title:       article.Title,
			Content:     article.Content,
			URL:         article.URL,
			PublishedAt: article.PublishedAt,
			Source: sourceJSON{
				ID:   article.SourceID,
				Name: article.SourceName,
			},
		})
	}
	response := searchResponse{Articles: results}
	if next != nil {
		encoded := EncodeCursor(*next)
		response.NextCursor = &encoded
	}
	return response
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
