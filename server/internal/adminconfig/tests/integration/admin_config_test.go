//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminconfig "github.com/Rahmannugar/macro-terminal/server/internal/adminconfig"
	calendarepositories "github.com/Rahmannugar/macro-terminal/server/internal/calendar/repositories"
	entityrepositories "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories"
	entityservices "github.com/Rahmannugar/macro-terminal/server/internal/entities/services"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	sourceservices "github.com/Rahmannugar/macro-terminal/server/internal/sources/services"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func adminConfigRouter(t *testing.T, pool *pgxpool.Pool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminconfig.RegisterAdminConfigRoutes(
		router.Group("/api/v1/admin"),
		entityservices.NewEntityService(entityrepositories.NewEntityRepository(pool)),
		sourceservices.NewSourceService(sourcerepositories.NewSourceRepository(pool)),
		calendarepositories.NewEventRepository(pool, nil),
	)
	return router
}

func call(t *testing.T, router *gin.Engine, method, target, body string) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var decoded map[string]any
	if len(response.Body.Bytes()) > 0 {
		if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode %s %s response: %v (%s)", method, target, err, response.Body.String())
		}
	}
	return response.Code, decoded
}

func failureCode(body map[string]any) string {
	failure, _ := body["error"].(map[string]any)
	code, _ := failure["code"].(string)
	return code
}

func field(t *testing.T, body map[string]any, object, field string) string {
	t.Helper()
	nested, ok := body[object].(map[string]any)
	if !ok {
		t.Fatalf("response has no %q object: %v", object, body)
	}
	value, ok := nested[field].(string)
	if !ok {
		t.Fatalf("%s.%s is not a string: %v", object, field, nested)
	}
	return value
}

func TestAdminConfigurationRoundTrip(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	router := adminConfigRouter(t, pool)

	status, body := call(t, router, http.MethodPost, "/api/v1/admin/entities", `{"code":"EUR","name":"Euro","type":"currency"}`)
	if status != http.StatusOK {
		t.Fatalf("create EUR status = %d, want 200 (%v)", status, body)
	}
	eurID := field(t, body, "entity", "id")

	status, body = call(t, router, http.MethodPost, "/api/v1/admin/entities", `{"code":"USD","name":"US Dollar","type":"currency"}`)
	if status != http.StatusOK {
		t.Fatalf("create USD status = %d, want 200 (%v)", status, body)
	}
	field(t, body, "entity", "id")

	status, body = call(t, router, http.MethodPost, "/api/v1/admin/entities", `{"code":"EUR","name":"Euro duplicate","type":"currency"}`)
	if status != http.StatusConflict || failureCode(body) != "already_exists" {
		t.Errorf("duplicate entity = %d/%q, want 409 already_exists", status, failureCode(body))
	}

	firstStatus, first := call(t, router, http.MethodGet, "/api/v1/admin/entities?limit=1", "")
	if firstStatus != http.StatusOK {
		t.Fatalf("first entity page status = %d, want 200", firstStatus)
	}
	firstRows, _ := first["entities"].([]any)
	firstCursor, _ := first["nextCursor"].(string)
	if len(firstRows) != 1 || firstCursor == "" {
		t.Fatalf("first page = %d rows / cursor %q, want one row and a cursor", len(firstRows), firstCursor)
	}
	secondStatus, second := call(t, router, http.MethodGet, "/api/v1/admin/entities?limit=1&cursor="+firstCursor, "")
	if secondStatus != http.StatusOK {
		t.Fatalf("second entity page status = %d, want 200", secondStatus)
	}
	secondRows, _ := second["entities"].([]any)
	secondCursor, _ := second["nextCursor"].(string)
	if len(secondRows) != 1 || secondCursor != "" {
		t.Fatalf("second page = %d rows / cursor %q, want the last row and no cursor", len(secondRows), secondCursor)
	}
	if firstRows[0].(map[string]any)["id"] == secondRows[0].(map[string]any)["id"] {
		t.Errorf("both pages returned the same entity")
	}

	status, body = call(t, router, http.MethodGet, "/api/v1/admin/entities?cursor=not-base64!!", "")
	if status != http.StatusBadRequest || failureCode(body) != "invalid_cursor" {
		t.Errorf("bad cursor = %d/%q, want 400 invalid_cursor", status, failureCode(body))
	}
	status, body = call(t, router, http.MethodGet, "/api/v1/admin/entities?limit=0", "")
	if status != http.StatusBadRequest || failureCode(body) != "invalid_limit" {
		t.Errorf("bad limit = %d/%q, want 400 invalid_limit", status, failureCode(body))
	}

	status, body = call(t, router, http.MethodPost, "/api/v1/admin/entity-pairs", `{"baseCode":"EUR","quoteCode":"USD","symbol":"EURUSD"}`)
	if status != http.StatusOK {
		t.Fatalf("create pair status = %d, want 200 (%v)", status, body)
	}
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/entity-pairs", `{"baseCode":"EUR","quoteCode":"MISSING","symbol":"EURMISS"}`)
	if status != http.StatusNotFound || failureCode(body) != "not_found" {
		t.Errorf("unknown quote = %d/%q, want 404 not_found", status, failureCode(body))
	}
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/entity-pairs", `{"baseCode":"EUR","quoteCode":"USD","symbol":"EURUSD"}`)
	if status != http.StatusConflict || failureCode(body) != "already_exists" {
		t.Errorf("duplicate pair = %d/%q, want 409 already_exists", status, failureCode(body))
	}

	status, body = call(t, router, http.MethodPost, "/api/v1/admin/indicators", `{"name":"Nonfarm Payrolls","type":"labor","entityCode":"EUR"}`)
	if status != http.StatusOK {
		t.Fatalf("create indicator status = %d, want 200 (%v)", status, body)
	}
	indicatorID := field(t, body, "indicator", "id")
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/indicators", `{"name":"Nonfarm Payrolls","type":"labor","entityCode":"EUR"}`)
	if status != http.StatusConflict || failureCode(body) != "already_exists" {
		t.Errorf("duplicate indicator = %d/%q, want 409 already_exists from the unique key", status, failureCode(body))
	}

	status, body = call(t, router, http.MethodPost, "/api/v1/admin/knowledge-terms", `{"name":"jobs report","type":"alias","entityCode":"EUR"}`)
	if status != http.StatusOK {
		t.Fatalf("create entity term status = %d, want 200 (%v)", status, body)
	}
	if entityID, ok := body["knowledgeTerm"].(map[string]any)["entityId"].(string); !ok || entityID == "" {
		t.Errorf("entity term entityId = %v, want the linked entity", body["knowledgeTerm"].(map[string]any)["entityId"])
	}
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/knowledge-terms", `{"name":"orphan","type":"alias"}`)
	if status != http.StatusBadRequest || failureCode(body) != "invalid_request" {
		t.Errorf("link-less term = %d/%q, want 400 invalid_request", status, failureCode(body))
	}
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/knowledge-terms",
		`{"name":"payrolls","type":"alias","indicatorId":"`+indicatorID+`"}`)
	if status != http.StatusOK {
		t.Fatalf("create indicator term status = %d, want 200 (%v)", status, body)
	}
	if entityID, ok := body["knowledgeTerm"].(map[string]any)["entityId"].(string); !ok || entityID != eurID {
		t.Errorf("indicator term entityId = %v, want the indicator's entity %s", body["knowledgeTerm"].(map[string]any)["entityId"], eurID)
	}
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/knowledge-terms", `{"name":"jobs report","type":"alias","entityCode":"EUR"}`)
	if status != http.StatusConflict || failureCode(body) != "already_exists" {
		t.Errorf("duplicate term = %d/%q, want 409 already_exists from the unique key", status, failureCode(body))
	}

	status, body = call(t, router, http.MethodPost, "/api/v1/admin/sources", `{"name":"BLS","type":"api"}`)
	if status != http.StatusOK {
		t.Fatalf("create source status = %d, want 200 (%v)", status, body)
	}
	sourceID := field(t, body, "source", "id")
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/sources", `{"name":"BLS","type":"api"}`)
	if status != http.StatusConflict || failureCode(body) != "already_exists" {
		t.Errorf("duplicate source = %d/%q, want 409 already_exists", status, failureCode(body))
	}

	status, body = call(t, router, http.MethodPost, "/api/v1/admin/source-configurations",
		`{"sourceId":"`+sourceID+`","type":"rss","config":{"url":"https://example.com/feed"}}`)
	if status != http.StatusOK {
		t.Fatalf("create configuration status = %d, want 200 (%v)", status, body)
	}
	configurationID := field(t, body, "sourceConfiguration", "id")

	status, body = call(t, router, http.MethodPost, "/api/v1/admin/source-configurations",
		`{"sourceId":"`+sourceID+`","type":"rss","config":{"url":"https://example.com/feed"}}`)
	if status != http.StatusConflict || failureCode(body) != "already_exists" {
		t.Errorf("duplicate configuration = %d/%q, want 409 already_exists", status, failureCode(body))
	}
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/source-configurations",
		`{"sourceId":"`+sourceID+`","type":"rss","config":{"url":"http://10.0.0.5/feed"}}`)
	if status != http.StatusBadRequest || failureCode(body) != "destination_refused" {
		t.Errorf("private address = %d/%q, want 400 destination_refused", status, failureCode(body))
	}
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/source-configurations",
		`{"sourceId":"`+sourceID+`","type":"rss","config":{"url":"https://example.com/feed","token":"abc"}}`)
	if status != http.StatusBadRequest || failureCode(body) != "invalid_config" {
		t.Errorf("secret key = %d/%q, want 400 invalid_config", status, failureCode(body))
	}
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/source-configurations",
		`{"sourceId":"`+uuid.NewString()+`","type":"rss","config":{"url":"https://example.com/other"}}`)
	if status != http.StatusNotFound || failureCode(body) != "not_found" {
		t.Errorf("unknown source = %d/%q, want 404 not_found", status, failureCode(body))
	}

	status, body = call(t, router, http.MethodPatch, "/api/v1/admin/source-configurations/"+configurationID,
		`{"config":{"url":"https://example.com/moved","language":"english"}}`)
	if status != http.StatusOK {
		t.Fatalf("patch configuration status = %d, want 200 (%v)", status, body)
	}
	patched, _ := body["sourceConfiguration"].(map[string]any)["config"].(map[string]any)
	if patched["url"] != "https://example.com/moved" {
		t.Errorf("patched config = %v, want the new url", patched)
	}
	status, body = call(t, router, http.MethodPatch, "/api/v1/admin/source-configurations/"+uuid.NewString(), `{"config":{"url":"https://example.com/feed"}}`)
	if status != http.StatusNotFound || failureCode(body) != "not_found" {
		t.Errorf("patch unknown = %d/%q, want 404 not_found", status, failureCode(body))
	}
	status, body = call(t, router, http.MethodPatch, "/api/v1/admin/source-configurations/not-a-uuid", `{"config":{"url":"https://example.com/feed"}}`)
	if status != http.StatusBadRequest || failureCode(body) != "invalid_id" {
		t.Errorf("patch bad id = %d/%q, want 400 invalid_id", status, failureCode(body))
	}

	scheduled := time.Date(2026, 11, 3, 12, 30, 0, 0, time.UTC)
	eventBody := `{"sourceId":"` + sourceID + `","indicatorId":"` + indicatorID +
		`","scheduledAt":"` + scheduled.Format(time.RFC3339) + `","previous":250000,"actual":261000,"entityCodes":["EUR"]}`
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/calendar-events", eventBody)
	if status != http.StatusOK {
		t.Fatalf("create event status = %d, want 200 (%v)", status, body)
	}
	status, body = call(t, router, http.MethodGet, "/api/v1/admin/calendar-events?limit=10", "")
	if status != http.StatusOK {
		t.Fatalf("list events status = %d, want 200", status)
	}
	events, _ := body["calendarEvents"].([]any)
	if len(events) != 1 {
		t.Fatalf("events = %d, want the created event", len(events))
	}
	created, err := time.Parse(time.RFC3339, events[0].(map[string]any)["scheduledAt"].(string))
	if err != nil || !created.Equal(scheduled) {
		t.Errorf("scheduledAt = %v (%v), want %v", events[0].(map[string]any)["scheduledAt"], err, scheduled)
	}

	status, body = call(t, router, http.MethodPost, "/api/v1/admin/calendar-events", eventBody)
	if status != http.StatusConflict || failureCode(body) != "already_exists" {
		t.Errorf("duplicate event = %d/%q, want 409 already_exists from the unique key", status, failureCode(body))
	}
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/calendar-events",
		`{"sourceId":"`+uuid.NewString()+`","indicatorId":"`+indicatorID+`","scheduledAt":"`+scheduled.Format(time.RFC3339)+`"}`)
	if status != http.StatusNotFound || failureCode(body) != "not_found" {
		t.Errorf("unknown source event = %d/%q, want 404 not_found", status, failureCode(body))
	}
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/calendar-events",
		`{"sourceId":"`+sourceID+`","indicatorId":"`+indicatorID+`"}`)
	if status != http.StatusBadRequest || failureCode(body) != "invalid_request" {
		t.Errorf("missing time event = %d/%q, want 400 invalid_request", status, failureCode(body))
	}
	status, body = call(t, router, http.MethodPost, "/api/v1/admin/calendar-events",
		`{"sourceId":"`+sourceID+`","indicatorId":"`+indicatorID+`","scheduledAt":"`+scheduled.Format(time.RFC3339)+`","entityCodes":["MISSING"]}`)
	if status != http.StatusNotFound || failureCode(body) != "not_found" {
		t.Errorf("unknown entity event = %d/%q, want 404 not_found", status, failureCode(body))
	}

	status, body = call(t, router, http.MethodGet, "/api/v1/admin/source-configurations?limit=1", "")
	if status != http.StatusOK {
		t.Fatalf("list configurations status = %d, want 200 (%v)", status, body)
	}
	configurations, _ := body["sourceConfigurations"].([]any)
	if len(configurations) != 1 {
		t.Errorf("configurations = %d, want 1", len(configurations))
	}
}
