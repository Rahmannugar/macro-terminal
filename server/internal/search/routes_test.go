package search

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func request(t *testing.T, service *Service, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, service)

	request := httptest.NewRequest(http.MethodGet, target, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
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

func TestSearchArticlesReturnsPage(t *testing.T) {
	articleID := uuid.New()
	service := NewService(
		&fakeSearcher{ids: []uuid.UUID{articleID}},
		&fakeHydrator{articles: map[uuid.UUID]models.StoredArticle{
			articleID: {
				ID:         articleID,
				Title:      "Fed holds rates",
				Content:    "The Federal Reserve kept rates unchanged.",
				URL:        "https://example.com/fed",
				SourceName: "Reuters",
			},
		}},
	)

	recorder := request(t, service, "/api/v1/search/articles?q=rates&limit=25")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var page searchResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Articles) != 1 {
		t.Fatalf("articles = %d, want 1", len(page.Articles))
	}
	if page.Articles[0].ID != articleID || page.Articles[0].Content == "" || page.Articles[0].Source.Name != "Reuters" {
		t.Errorf("article = %+v, want the hydrated row with its source", page.Articles[0])
	}
	if page.NextCursor != nil {
		t.Errorf("nextCursor = %q, want nil when the ranking ends", *page.NextCursor)
	}
}

func TestSearchArticlesRejectsMalformedRequests(t *testing.T) {
	service := NewService(&fakeSearcher{ids: rankedIDs(1)}, hydratorFor(rankedIDs(1)))
	for _, testCase := range []struct {
		target string
		code   string
	}{
		{"/api/v1/search/articles", "invalid_query"},
		{"/api/v1/search/articles?q=rates&limit=0", "invalid_limit"},
		{"/api/v1/search/articles?q=rates&limit=abc", "invalid_limit"},
		{"/api/v1/search/articles?q=rates&limit=101", "invalid_limit"},
		{"/api/v1/search/articles?q=rates&cursor=not-base64!!", "invalid_cursor"},
		{"/api/v1/search/articles?q=rates&cursor=" + EncodeCursor(Cursor{Offset: maximumWindow}), "invalid_cursor"},
	} {
		recorder := request(t, service, testCase.target)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d body = %s, want 400", testCase.target, recorder.Code, recorder.Body.String())
			continue
		}
		if failure := decodeFailure(t, recorder); failure.Error.Code != testCase.code {
			t.Errorf("%s code = %q, want %q", testCase.target, failure.Error.Code, testCase.code)
		}
	}
}
