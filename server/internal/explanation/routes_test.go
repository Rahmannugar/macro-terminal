package explanation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func performRequest(t *testing.T, service *Service, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, service)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, target, nil))
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

func TestExplainArticleRouteReturnsExplanation(t *testing.T) {
	fixture := newFixture(t)

	recorder := performRequest(t, fixture.service, "/api/v1/articles/"+fixture.articleID.String()+"/explain")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var response explanationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if response.Explanation.Text != "article explanation" {
		t.Fatalf("text = %q, want the generated answer", response.Explanation.Text)
	}
}

func TestExplainArticleRouteRejectsMalformedID(t *testing.T) {
	fixture := newFixture(t)

	recorder := performRequest(t, fixture.service, "/api/v1/articles/not-a-uuid/explain")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if failure := decodeFailure(t, recorder); failure.Error.Code != "invalid_id" {
		t.Fatalf("code = %q, want invalid_id", failure.Error.Code)
	}
}

func TestExplainArticleRouteAnswersNotFound(t *testing.T) {
	fixture := newFixture(t)

	recorder := performRequest(t, fixture.service, "/api/v1/articles/"+uuid.NewString()+"/explain")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if failure := decodeFailure(t, recorder); failure.Error.Code != "not_found" {
		t.Fatalf("code = %q, want not_found", failure.Error.Code)
	}
}

func TestExplainArticleRouteReportsUnavailableAI(t *testing.T) {
	fixture := newFixture(t)
	fixture.explainer.err = ai.ErrMissingKey

	recorder := performRequest(t, fixture.service, "/api/v1/articles/"+fixture.articleID.String()+"/explain")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if failure := decodeFailure(t, recorder); failure.Error.Code != "explanation_unavailable" {
		t.Fatalf("code = %q, want explanation_unavailable", failure.Error.Code)
	}
}

func TestExplainCalendarEventRouteReturnsExplanation(t *testing.T) {
	fixture := newFixture(t)

	recorder := performRequest(t, fixture.service, "/api/v1/calendar-events/"+fixture.eventID.String()+"/explain")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var response explanationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if response.Explanation.Text != "event explanation" {
		t.Fatalf("text = %q, want the generated answer", response.Explanation.Text)
	}
}

func TestExplainCalendarEventRouteRejectsMalformedID(t *testing.T) {
	fixture := newFixture(t)

	recorder := performRequest(t, fixture.service, "/api/v1/calendar-events/nope/explain")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if failure := decodeFailure(t, recorder); failure.Error.Code != "invalid_id" {
		t.Fatalf("code = %q, want invalid_id", failure.Error.Code)
	}
}
