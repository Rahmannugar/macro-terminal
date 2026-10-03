package clustering

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func pageService() (*Service, uuid.UUID) {
	self := uuid.New()
	mate := uuid.New()
	past := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	return NewService(
		&fakeClusterReader{mates: map[uuid.UUID][]uuid.UUID{self: {self, mate}}},
		&fakeSearcher{},
		&fakeHydrator{articles: map[uuid.UUID]models.StoredArticle{
			self: storedArticle(self, "Fed holds rates", past),
			mate: storedArticle(mate, "Rate decision", past),
		}},
	), self
}

func TestRelatedArticlesReturnsPage(t *testing.T) {
	service, self := pageService()

	recorder := request(t, service, "/api/v1/articles/"+self.String()+"/related")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var page relatedResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Articles) != 1 {
		t.Fatalf("articles = %d, want 1 cluster mate", len(page.Articles))
	}
	if page.Articles[0].Source.Name == "" || page.Articles[0].URL == "" {
		t.Errorf("article = %+v, want the hydrated row with its source", page.Articles[0])
	}
}

func TestRelatedArticlesRejectsMalformedRequests(t *testing.T) {
	service, _ := pageService()
	for _, testCase := range []struct {
		target string
		code   string
	}{
		{"/api/v1/articles/not-a-uuid/related", "invalid_id"},
		{"/api/v1/articles/" + uuid.New().String() + "/related?limit=0", "invalid_limit"},
		{"/api/v1/articles/" + uuid.New().String() + "/related?limit=abc", "invalid_limit"},
		{"/api/v1/articles/" + uuid.New().String() + "/related?limit=101", "invalid_limit"},
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

func TestRelatedArticlesReportsMissingArticle(t *testing.T) {
	service := NewService(&fakeClusterReader{}, &fakeSearcher{}, &fakeHydrator{})

	recorder := request(t, service, "/api/v1/articles/"+uuid.New().String()+"/related")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	if failure := decodeFailure(t, recorder); failure.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", failure.Error.Code)
	}
}

func TestRelatedArticlesReportsSearcherFailure(t *testing.T) {
	self := uuid.New()
	past := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	service := NewService(
		&fakeClusterReader{},
		&fakeSearcher{err: errors.New("index unreachable")},
		&fakeHydrator{articles: map[uuid.UUID]models.StoredArticle{
			self: storedArticle(self, "Fed holds rates", past),
		}},
	)

	recorder := request(t, service, "/api/v1/articles/"+self.String()+"/related")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s, want 503", recorder.Code, recorder.Body.String())
	}
	if failure := decodeFailure(t, recorder); failure.Error.Code != "search_unavailable" {
		t.Errorf("code = %q, want search_unavailable", failure.Error.Code)
	}
}
