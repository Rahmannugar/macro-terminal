package articles

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ArticleHydrator interface {
	GetArticlesByIDs(ctx context.Context, ids []uuid.UUID) ([]models.StoredArticle, error)
}

func RegisterRoutes(router gin.IRoutes, hydrator ArticleHydrator) {
	router.GET("/api/v1/articles/:id", getArticle(hydrator))
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

type articleResponse struct {
	Article articleJSON `json:"article"`
}

// @Summary Get one article.
// @Description Returns a single article by id.
// @Tags articles
// @Param id path string true "Article id (UUID)."
// @Success 200 {object} articleResponse "The article."
// @Failure 400 {object} openapi.Error "The request was malformed."
// @Failure 404 {object} openapi.Error "The article does not exist."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/articles/{id} [get]
func getArticle(hydrator ArticleHydrator) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		articleID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_id", "The article id must be a UUID.")
			return
		}
		found, err := hydrator.GetArticlesByIDs(ctx.Request.Context(), []uuid.UUID{articleID})
		if err != nil {
			slog.Default().ErrorContext(ctx.Request.Context(), "article lookup failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		if len(found) == 0 {
			writeFailure(ctx, http.StatusNotFound, "not_found", "The article does not exist.")
			return
		}
		ctx.JSON(http.StatusOK, articleResponse{Article: newArticleJSON(found[0])})
	}
}

func newArticleJSON(article models.StoredArticle) articleJSON {
	return articleJSON{
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
	}
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
