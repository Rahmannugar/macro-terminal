package articles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type fakeHydrator struct {
	articles map[uuid.UUID]models.StoredArticle
	err      error
}

func (hydrator *fakeHydrator) GetArticlesByIDs(_ context.Context, ids []uuid.UUID) ([]models.StoredArticle, error) {
	if hydrator.err != nil {
		return nil, hydrator.err
	}
	found := make([]models.StoredArticle, 0, len(ids))
	for _, id := range ids {
		if article, ok := hydrator.articles[id]; ok {
			found = append(found, article)
		}
	}
	return found, nil
}

func request(t *testing.T, hydrator ArticleHydrator, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, hydrator)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func decodeFailure(t *testing.T, recorder *httptest.ResponseRecorder) openapi.Error {
	t.Helper()
	var failure openapi.Error
	if err := json.Unmarshal(recorder.Body.Bytes(), &failure); err != nil {
		t.Fatalf("decode failure body %q: %v", recorder.Body.String(), err)
	}
	return failure
}

func articleFixture() (ArticleHydrator, uuid.UUID) {
	id := uuid.New()
	published := time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC)
	return &fakeHydrator{articles: map[uuid.UUID]models.StoredArticle{
		id: {
			ID:          id,
			SourceID:    uuid.New(),
			SourceName:  "Reuters",
			Title:       "Fed holds rates steady",
			Content:     "The Federal Reserve left rates unchanged.",
			URL:         "https://www.reuters.com/markets/us/",
			PublishedAt: &published,
		},
	}}, id
}

func TestGetArticleReturnsArticle(t *testing.T) {
	hydrator, id := articleFixture()

	recorder := request(t, hydrator, "/api/v1/articles/"+id.String())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var page articleResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if page.Article.ID != id || page.Article.Title != "Fed holds rates steady" {
		t.Fatalf("article = %+v, want the stored record", page.Article)
	}
	if page.Article.Source.Name != "Reuters" {
		t.Fatalf("source = %+v, want the hydrated source", page.Article.Source)
	}
}

func TestGetArticleRejectsMalformedID(t *testing.T) {
	hydrator, _ := articleFixture()

	recorder := request(t, hydrator, "/api/v1/articles/not-a-uuid")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if failure := decodeFailure(t, recorder); failure.Error.Code != "invalid_id" {
		t.Fatalf("code = %q, want invalid_id", failure.Error.Code)
	}
}

func TestGetArticleAnswersNotFoundForUnknownArticle(t *testing.T) {
	hydrator, _ := articleFixture()

	recorder := request(t, hydrator, "/api/v1/articles/"+uuid.NewString())
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if failure := decodeFailure(t, recorder); failure.Error.Code != "not_found" {
		t.Fatalf("code = %q, want not_found", failure.Error.Code)
	}
}

func TestGetArticleReportsHydrationFailure(t *testing.T) {
	hydrator, id := articleFixture()
	hydrator.(*fakeHydrator).err = context.DeadlineExceeded

	recorder := request(t, hydrator, "/api/v1/articles/"+id.String())
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if failure := decodeFailure(t, recorder); failure.Error.Code != "internal_error" {
		t.Fatalf("code = %q, want internal_error", failure.Error.Code)
	}
}
