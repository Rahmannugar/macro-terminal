package articles

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/authentication"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	feedDefaultPageSize = 25
	feedMaximumPageSize = 100
)

type FeedRepository interface {
	RecentArticleIDsPage(
		ctx context.Context,
		entityIDs []uuid.UUID,
		cursor *paging.Cursor,
		limit int32,
	) ([]models.RecentArticleRef, *paging.Cursor, error)
}

type FeedAssets interface {
	EntityIDsForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}

type FeedService struct {
	articles FeedRepository
	hydrator ArticleHydrator
	assets   FeedAssets
}

func NewFeedService(articles FeedRepository, hydrator ArticleHydrator, assets FeedAssets) *FeedService {
	return &FeedService{articles: articles, hydrator: hydrator, assets: assets}
}

// Feed resolves one page for the signed-in account. An account with no
// followed pairs falls back to the newest articles across the terminal.
func (service *FeedService) Feed(
	ctx context.Context,
	userID uuid.UUID,
	cursor *paging.Cursor,
	limit int32,
) ([]models.StoredArticle, *paging.Cursor, error) {
	entityIDs, err := service.assets.EntityIDsForUser(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve feed entities: %w", err)
	}
	refs, next, err := service.articles.RecentArticleIDsPage(ctx, entityIDs, cursor, limit)
	if err != nil {
		return nil, nil, err
	}
	if len(refs) == 0 {
		return []models.StoredArticle{}, next, nil
	}
	ids := make([]uuid.UUID, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.ID)
	}
	hydrated, err := service.hydrator.GetArticlesByIDs(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("hydrate feed: %w", err)
	}
	byID := make(map[uuid.UUID]models.StoredArticle, len(hydrated))
	for _, article := range hydrated {
		byID[article.ID] = article
	}
	ordered := make([]models.StoredArticle, 0, len(hydrated))
	for _, id := range ids {
		if article, ok := byID[id]; ok {
			ordered = append(ordered, article)
		}
	}
	return ordered, next, nil
}

type feedResponse struct {
	Articles   []articleJSON `json:"articles"`
	NextCursor *string       `json:"nextCursor"`
}

// RegisterFeedRoutes mounts the personalized feed at /articles on the
// caller's group. The caller mounts the session guard on the group.
func RegisterFeedRoutes(router gin.IRoutes, feed *FeedService) {
	router.GET("/articles", listFeed(feed))
}

// @Summary List the feed.
// @Description Returns the newest articles mapped to the account's followed asset pairs, newest first, falling back to the whole terminal when nothing is followed. Pass the returned nextCursor to fetch the next page.
// @Tags articles
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} feedResponse "The articles and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/articles [get]
func listFeed(feed *FeedService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		limit := int32(feedDefaultPageSize)
		if raw := ctx.Query("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > feedMaximumPageSize {
				writeFailure(ctx, http.StatusBadRequest, "invalid_limit", "Limit must be a number between 1 and 100.")
				return
			}
			limit = int32(parsed)
		}
		cursor, err := paging.DecodeCursor(ctx.Query("cursor"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_cursor", "Use the next cursor returned by the previous page.")
			return
		}
		articles, next, err := feed.Feed(ctx.Request.Context(), account.User.ID, cursor, limit)
		if err != nil {
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		response := feedResponse{Articles: make([]articleJSON, 0, len(articles))}
		for _, article := range articles {
			response.Articles = append(response.Articles, newArticleJSON(article))
		}
		if next != nil {
			encoded := paging.EncodeCursor(*next)
			response.NextCursor = &encoded
		}
		ctx.JSON(http.StatusOK, response)
	}
}
