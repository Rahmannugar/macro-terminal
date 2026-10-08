package articles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Rahmannugar/authlier"
	"github.com/Rahmannugar/authlier/emailpassword"
	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/authentication"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	usersmodels "github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var feedTestUserID = uuid.New()

type feedStubSessions struct{}

func (feedStubSessions) ResolveSession(*http.Request) (authlier.Session, error) {
	return authlier.Session{
		ID:        "session-1",
		SubjectID: "subject-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

type feedStubUsers struct{}

func (feedStubUsers) ResolveUser(context.Context, string, string) (usersmodels.User, error) {
	return usersmodels.User{ID: feedTestUserID, Role: usersmodels.RoleUser, Status: usersmodels.StatusActive}, nil
}

func (feedStubUsers) UpdateUsername(_ context.Context, _ uuid.UUID, _ *string) (usersmodels.User, error) {
	return usersmodels.User{}, nil
}

type feedStubCredentials struct{}

func (feedStubCredentials) FindBySubject(context.Context, string) (emailpassword.User, emailpassword.PasswordCredential, error) {
	return emailpassword.User{ID: "subject-1", Email: "reader@example.com"}, emailpassword.PasswordCredential{}, nil
}

func (feedStubCredentials) AddPassword(context.Context, string, string, time.Time) (emailpassword.User, error) {
	return emailpassword.User{}, nil
}

func (feedStubCredentials) ReplacePasswordHash(context.Context, string, string, string, time.Time) error {
	return nil
}

func (feedStubCredentials) RemovePassword(context.Context, string, string, time.Time) error {
	return nil
}

type fakeFeedRepo struct {
	refs []models.RecentArticleRef
	next *paging.Cursor

	gotEntityIDs []uuid.UUID
}

func (fake *fakeFeedRepo) RecentArticleIDsPage(_ context.Context, entityIDs []uuid.UUID, _ *paging.Cursor, _ int32) ([]models.RecentArticleRef, *paging.Cursor, error) {
	fake.gotEntityIDs = entityIDs
	return fake.refs, fake.next, nil
}

type fakeFeedAssets struct {
	ids []uuid.UUID
}

func (fake *fakeFeedAssets) EntityIDsForUser(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return fake.ids, nil
}

type reverseHydrator struct {
	articles map[uuid.UUID]models.StoredArticle
}

func (hydrator reverseHydrator) GetArticlesByIDs(_ context.Context, ids []uuid.UUID) ([]models.StoredArticle, error) {
	found := make([]models.StoredArticle, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		if article, ok := hydrator.articles[ids[i]]; ok {
			found = append(found, article)
		}
	}
	return found, nil
}

func feedRouter(t *testing.T, feed *FeedService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1")
	group.Use(authentication.RequireUser(authentication.NewService(
		feedStubSessions{},
		feedStubUsers{},
		feedStubCredentials{},
	)))
	RegisterFeedRoutes(group, feed)
	return router
}

func feedArticleFixture(id uuid.UUID, title string) models.StoredArticle {
	published := time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC)
	return models.StoredArticle{
		ID:          id,
		SourceID:    uuid.New(),
		SourceName:  "Reuters",
		Title:       title,
		Content:     "Body for " + title,
		URL:         "https://www.reuters.com/markets/us/",
		PublishedAt: &published,
	}
}

func performFeed(router *gin.Engine, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestFeedPreservesRequestedOrder(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	repo := &fakeFeedRepo{
		refs: []models.RecentArticleRef{{ID: first}, {ID: second}},
		next: &paging.Cursor{At: time.Now(), ID: second},
	}
	hydrator := reverseHydrator{articles: map[uuid.UUID]models.StoredArticle{
		first:  feedArticleFixture(first, "First story"),
		second: feedArticleFixture(second, "Second story"),
	}}
	feed := NewFeedService(repo, hydrator, &fakeFeedAssets{ids: []uuid.UUID{uuid.New()}})
	router := feedRouter(t, feed)

	recorder := performFeed(router, "/api/v1/articles")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var page feedResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if len(page.Articles) != 2 || page.Articles[0].ID != first || page.Articles[1].ID != second {
		t.Fatalf("articles = %+v, want the page order", page.Articles)
	}
	if page.NextCursor == nil {
		t.Error("nextCursor = nil, want the page cursor")
	}
	if len(repo.gotEntityIDs) != 1 {
		t.Errorf("entityIDs = %v, want the followed legs", repo.gotEntityIDs)
	}
}

func TestFeedWithoutFollowsQueriesEverything(t *testing.T) {
	id := uuid.New()
	repo := &fakeFeedRepo{refs: []models.RecentArticleRef{{ID: id}}}
	hydrator := reverseHydrator{articles: map[uuid.UUID]models.StoredArticle{
		id: feedArticleFixture(id, "Global story"),
	}}
	feed := NewFeedService(repo, hydrator, &fakeFeedAssets{})
	router := feedRouter(t, feed)

	recorder := performFeed(router, "/api/v1/articles")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if len(repo.gotEntityIDs) != 0 {
		t.Errorf("entityIDs = %v, want an empty list so the query falls back to global", repo.gotEntityIDs)
	}
	var page feedResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if len(page.Articles) != 1 || page.Articles[0].ID != id {
		t.Errorf("articles = %+v, want the global story", page.Articles)
	}
}

func TestFeedRejectsBadPageParameters(t *testing.T) {
	feed := NewFeedService(&fakeFeedRepo{}, reverseHydrator{}, &fakeFeedAssets{})
	router := feedRouter(t, feed)

	recorder := performFeed(router, "/api/v1/articles?limit=0")
	if recorder.Code != http.StatusBadRequest || decodeFailure(t, recorder).Error.Code != "invalid_limit" {
		t.Errorf("limit=0 → status/code = %d/%q, want 400 invalid_limit", recorder.Code, decodeFailure(t, recorder).Error.Code)
	}

	recorder = performFeed(router, "/api/v1/articles?cursor=not-base64!!")
	if recorder.Code != http.StatusBadRequest || decodeFailure(t, recorder).Error.Code != "invalid_cursor" {
		t.Errorf("garbage cursor → status/code = %d/%q, want 400 invalid_cursor", recorder.Code, decodeFailure(t, recorder).Error.Code)
	}
}

func TestFeedRequiresSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	feed := NewFeedService(&fakeFeedRepo{}, reverseHydrator{}, &fakeFeedAssets{})
	router := gin.New()
	RegisterFeedRoutes(router.Group("/api/v1"), feed)

	recorder := performFeed(router, "/api/v1/articles")
	if recorder.Code != http.StatusUnauthorized || decodeFailure(t, recorder).Error.Code != "unauthenticated" {
		t.Errorf("status/code = %d/%q, want 401 unauthenticated", recorder.Code, decodeFailure(t, recorder).Error.Code)
	}
}
