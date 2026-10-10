package adminconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	entityservices "github.com/Rahmannugar/macro-terminal/server/internal/entities/services"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/safehttp"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourceservices "github.com/Rahmannugar/macro-terminal/server/internal/sources/services"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeEntities struct {
	entities   []entitymodels.Entity
	pairs      []entitymodels.EntityPair
	indicators []entitymodels.Indicator
	terms      []entitymodels.KnowledgeTerm
	next       *paging.Cursor

	entity    entitymodels.Entity
	pair      entitymodels.EntityPair
	indicator entitymodels.Indicator
	term      entitymodels.KnowledgeTerm

	createErr error
	pageErr   error
	lookupErr error

	gotLimit      int32
	gotCursor     *paging.Cursor
	gotTermName   string
	gotTermEntity string
	gotTermIndic  *uuid.UUID
}

func (fake *fakeEntities) CreateEntity(context.Context, string, string, string) (entitymodels.Entity, error) {
	if fake.createErr != nil {
		return entitymodels.Entity{}, fake.createErr
	}
	return fake.entity, nil
}

func (fake *fakeEntities) CreateEntityPair(context.Context, string, string, string) (entitymodels.EntityPair, error) {
	if fake.createErr != nil {
		return entitymodels.EntityPair{}, fake.createErr
	}
	return fake.pair, nil
}

func (fake *fakeEntities) CreateIndicator(context.Context, string, string, string) (entitymodels.Indicator, error) {
	if fake.createErr != nil {
		return entitymodels.Indicator{}, fake.createErr
	}
	return fake.indicator, nil
}

func (fake *fakeEntities) CreateKnowledgeTerm(
	_ context.Context,
	name, _, entityCode string,
	indicatorID *uuid.UUID,
) (entitymodels.KnowledgeTerm, error) {
	fake.gotTermName, fake.gotTermEntity, fake.gotTermIndic = name, entityCode, indicatorID
	if fake.createErr != nil {
		return entitymodels.KnowledgeTerm{}, fake.createErr
	}
	return fake.term, nil
}

func (fake *fakeEntities) EntityByCode(context.Context, string) (entitymodels.Entity, error) {
	return fake.entity, fake.lookupErr
}

func (fake *fakeEntities) IndicatorByID(context.Context, uuid.UUID) (entitymodels.Indicator, error) {
	return fake.indicator, fake.lookupErr
}

func (fake *fakeEntities) ListEntitiesPage(
	_ context.Context, cursor *paging.Cursor, limit int32,
) ([]entitymodels.Entity, *paging.Cursor, error) {
	fake.gotLimit, fake.gotCursor = limit, cursor
	return fake.entities, fake.next, fake.pageErr
}

func (fake *fakeEntities) ListEntityPairsPage(
	_ context.Context, cursor *paging.Cursor, limit int32,
) ([]entitymodels.EntityPair, *paging.Cursor, error) {
	fake.gotLimit, fake.gotCursor = limit, cursor
	return fake.pairs, fake.next, fake.pageErr
}

func (fake *fakeEntities) ListIndicatorsPage(
	_ context.Context, cursor *paging.Cursor, limit int32,
) ([]entitymodels.Indicator, *paging.Cursor, error) {
	fake.gotLimit, fake.gotCursor = limit, cursor
	return fake.indicators, fake.next, fake.pageErr
}

func (fake *fakeEntities) ListKnowledgeTermsPage(
	_ context.Context, cursor *paging.Cursor, limit int32,
) ([]entitymodels.KnowledgeTerm, *paging.Cursor, error) {
	fake.gotLimit, fake.gotCursor = limit, cursor
	return fake.terms, fake.next, fake.pageErr
}

type fakeSources struct {
	sources []sourcemodels.Source
	configs []sourcemodels.SourceConfiguration
	next    *paging.Cursor

	source        sourcemodels.Source
	configuration sourcemodels.SourceConfiguration
	createdSource sourcemodels.Source

	sourceLookupErr error
	createSourceErr error
	createConfigErr error
	updateConfigErr error
	pageErr         error

	gotLimit        int32
	gotCursor       *paging.Cursor
	gotUpdateID     uuid.UUID
	gotUpdateConfig json.RawMessage
}

func (fake *fakeSources) CreateSource(context.Context, string, string) (sourcemodels.Source, error) {
	if fake.createSourceErr != nil {
		return sourcemodels.Source{}, fake.createSourceErr
	}
	return fake.createdSource, nil
}

func (fake *fakeSources) CreateSourceConfiguration(
	context.Context, uuid.UUID, string, json.RawMessage,
) (sourcemodels.SourceConfiguration, error) {
	if fake.createConfigErr != nil {
		return sourcemodels.SourceConfiguration{}, fake.createConfigErr
	}
	return fake.configuration, nil
}

func (fake *fakeSources) UpdateSourceConfiguration(
	_ context.Context, id uuid.UUID, config json.RawMessage,
) (sourcemodels.SourceConfiguration, error) {
	fake.gotUpdateID, fake.gotUpdateConfig = id, config
	if fake.updateConfigErr != nil {
		return sourcemodels.SourceConfiguration{}, fake.updateConfigErr
	}
	return fake.configuration, nil
}

func (fake *fakeSources) SourceByID(context.Context, uuid.UUID) (sourcemodels.Source, error) {
	return fake.source, fake.sourceLookupErr
}

func (fake *fakeSources) ListSourcesPage(
	_ context.Context, cursor *paging.Cursor, limit int32,
) ([]sourcemodels.Source, *paging.Cursor, error) {
	fake.gotLimit, fake.gotCursor = limit, cursor
	return fake.sources, fake.next, fake.pageErr
}

func (fake *fakeSources) ListSourceConfigurationsPage(
	_ context.Context, cursor *paging.Cursor, limit int32,
) ([]sourcemodels.SourceConfiguration, *paging.Cursor, error) {
	fake.gotLimit, fake.gotCursor = limit, cursor
	return fake.configs, fake.next, fake.pageErr
}

type fakeEvents struct {
	records []calendarmodels.EventRecord
	next    *paging.Cursor
	record  calendarmodels.EventRecord

	createErr error
	pageErr   error

	gotLimit  int32
	gotCursor *paging.Cursor
	gotEntry  *calendarmodels.CreateEventEntry

	gotUpdate    *calendarmodels.UpdateEventEntry
	updateErr    error
	gotArchiveID *uuid.UUID
	archiveErr   error
	gotRestoreID *uuid.UUID
	restoreErr   error
}

func (fake *fakeEvents) CreateCalendarEvent(
	_ context.Context,
	entry calendarmodels.CreateEventEntry,
) (calendarmodels.EventRecord, error) {
	fake.gotEntry = &entry
	if fake.createErr != nil {
		return calendarmodels.EventRecord{}, fake.createErr
	}
	return fake.record, nil
}

func (fake *fakeEvents) ListCalendarEventsPage(
	_ context.Context, cursor *paging.Cursor, limit int32,
) ([]calendarmodels.EventRecord, *paging.Cursor, error) {
	fake.gotLimit, fake.gotCursor = limit, cursor
	return fake.records, fake.next, fake.pageErr
}

func (fake *fakeEvents) UpdateCalendarEvent(
	_ context.Context,
	entry calendarmodels.UpdateEventEntry,
) (calendarmodels.EventRecord, error) {
	fake.gotUpdate = &entry
	if fake.updateErr != nil {
		return calendarmodels.EventRecord{}, fake.updateErr
	}
	return fake.record, nil
}

func (fake *fakeEvents) ArchiveCalendarEvent(_ context.Context, id uuid.UUID) error {
	fake.gotArchiveID = &id
	return fake.archiveErr
}

func (fake *fakeEvents) RestoreCalendarEvent(_ context.Context, id uuid.UUID) error {
	fake.gotRestoreID = &id
	return fake.restoreErr
}

func configRouter(t *testing.T, entities Entities, sources Sources, events Events) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterAdminConfigRoutes(router.Group("/api/v1/admin"), entities, sources, events)
	return router
}

func perform(router *gin.Engine, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) openapi.Error {
	t.Helper()
	var body openapi.Error
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failure: %v", err)
	}
	return body
}

func TestListEntitiesAppliesPaging(t *testing.T) {
	next := &paging.Cursor{At: time.Unix(0, 1759750000000000000).UTC(), ID: uuid.New()}
	fake := &fakeEntities{
		entities: []entitymodels.Entity{{ID: uuid.New(), Code: "EUR", Name: "Euro", Type: "currency"}},
		next:     next,
	}
	router := configRouter(t, fake, &fakeSources{}, &fakeEvents{})

	recorder := perform(router, http.MethodGet, "/api/v1/admin/entities?limit=1", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body entityListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Entities) != 1 || body.Entities[0].Code != "EUR" {
		t.Errorf("body = %+v, want one EUR entity", body)
	}
	if body.NextCursor == nil || *body.NextCursor != paging.EncodeCursor(*next) {
		t.Errorf("nextCursor = %v, want the encoded next cursor", body.NextCursor)
	}
	if fake.gotLimit != 1 || fake.gotCursor != nil {
		t.Errorf("limit/cursor = %d/%v, want 1/none", fake.gotLimit, fake.gotCursor)
	}

	encoded := paging.EncodeCursor(*next)
	recorder = perform(router, http.MethodGet, "/api/v1/admin/entities?cursor="+encoded, "")
	if recorder.Code != http.StatusOK || fake.gotCursor == nil {
		t.Fatalf("second page status/cursor = %d/%v, want 200 with a cursor", recorder.Code, fake.gotCursor)
	}
	if fake.gotCursor.ID != next.ID {
		t.Errorf("cursor id = %v, want %v", fake.gotCursor.ID, next.ID)
	}
}

func TestListsRejectBadPageParameters(t *testing.T) {
	router := configRouter(t, &fakeEntities{}, &fakeSources{}, &fakeEvents{})

	for _, target := range []string{
		"/api/v1/admin/entities?limit=abc",
		"/api/v1/admin/entities?limit=0",
		"/api/v1/admin/sources?limit=101",
	} {
		recorder := perform(router, http.MethodGet, target, "")
		if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_limit" {
			t.Errorf("%s → status/code = %d/%q, want 400 invalid_limit", target, recorder.Code, decodeError(t, recorder).Error.Code)
		}
	}

	recorder := perform(router, http.MethodGet, "/api/v1/admin/calendar-events?cursor=not-base64!!", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_cursor" {
		t.Errorf("status/code = %d/%q, want 400 invalid_cursor", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestCreateEntityMapsFailures(t *testing.T) {
	created := entitymodels.Entity{ID: uuid.New(), Code: "EUR", Name: "Euro", Type: "currency"}
	ok := configRouter(t, &fakeEntities{entity: created}, &fakeSources{}, &fakeEvents{})

	recorder := perform(ok, http.MethodPost, "/api/v1/admin/entities", `{"code":"EUR","name":"Euro","type":"currency"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body entityResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Entity.Code != "EUR" {
		t.Errorf("entity = %+v, want EUR", body.Entity)
	}

	recorder = perform(ok, http.MethodPost, "/api/v1/admin/entities", ``)
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_request" {
		t.Errorf("empty body status/code = %d/%q, want 400 invalid_request", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	failureCases := []struct {
		err    error
		status int
		code   string
	}{
		{entityservices.ErrEntityCodeRequired, http.StatusBadRequest, "invalid_request"},
		{entityservices.ErrEntityCodeExists, http.StatusConflict, "already_exists"},
		{&pgconn.PgError{Code: "23505", Message: "duplicate key"}, http.StatusConflict, "already_exists"},
	}
	for _, test := range failureCases {
		router := configRouter(t, &fakeEntities{createErr: test.err}, &fakeSources{}, &fakeEvents{})
		recorder = perform(router, http.MethodPost, "/api/v1/admin/entities", `{"code":"EUR","name":"Euro","type":"currency"}`)
		if recorder.Code != test.status || decodeError(t, recorder).Error.Code != test.code {
			t.Errorf("err %v → status/code = %d/%q, want %d %q", test.err, recorder.Code, decodeError(t, recorder).Error.Code, test.status, test.code)
		}
	}
}

func TestCreateEntityPairMapsUnknownCodeTo404(t *testing.T) {
	router := configRouter(t, &fakeEntities{createErr: entityservices.ErrEntityNotFound}, &fakeSources{}, &fakeEvents{})
	recorder := perform(router, http.MethodPost, "/api/v1/admin/entity-pairs", `{"baseCode":"EUR","quoteCode":"USD","symbol":"EURUSD"}`)
	if recorder.Code != http.StatusNotFound || decodeError(t, recorder).Error.Code != "not_found" {
		t.Errorf("status/code = %d/%q, want 404 not_found", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestCreateSourceConfigurationMapsFailures(t *testing.T) {
	body := `{"sourceId":"5a1c3e7b-9d2f-4c8a-b6e1-7f4d2a9c3e50","type":"rss","config":{"url":"https://example.com/feed"}}`
	failureCases := []struct {
		err    error
		status int
		code   string
	}{
		{safehttp.ErrBlockedAddress, http.StatusBadRequest, "destination_refused"},
		{sourceservices.ErrConfigurationSecretInConfig, http.StatusBadRequest, "invalid_config"},
		{sourceservices.ErrConfigurationExists, http.StatusConflict, "already_exists"},
		{sourceservices.ErrSourceNotFound, http.StatusNotFound, "not_found"},
	}
	for _, test := range failureCases {
		router := configRouter(t, &fakeEntities{}, &fakeSources{createConfigErr: test.err}, &fakeEvents{})
		recorder := perform(router, http.MethodPost, "/api/v1/admin/source-configurations", body)
		if recorder.Code != test.status || decodeError(t, recorder).Error.Code != test.code {
			t.Errorf("err %v → status/code = %d/%q, want %d %q", test.err, recorder.Code, decodeError(t, recorder).Error.Code, test.status, test.code)
		}
	}

	router := configRouter(t, &fakeEntities{}, &fakeSources{
		configuration: sourcemodels.SourceConfiguration{
			ID: uuid.New(), SourceID: uuid.New(), Type: "rss",
			Config: json.RawMessage(`{"url":"https://example.com/feed"}`),
		},
	}, &fakeEvents{})
	recorder := perform(router, http.MethodPost, "/api/v1/admin/source-configurations", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var response sourceConfigurationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.SourceConfiguration.Type != "rss" {
		t.Errorf("configuration = %+v, want type rss", response.SourceConfiguration)
	}
}

func TestUpdateSourceConfiguration(t *testing.T) {
	id := uuid.New()
	router := configRouter(t, &fakeEntities{}, &fakeSources{}, &fakeEvents{})

	recorder := perform(router, http.MethodPatch, "/api/v1/admin/source-configurations/nope", `{"config":{"url":"https://example.com/feed"}}`)
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_id" {
		t.Errorf("status/code = %d/%q, want 400 invalid_id", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	missing := configRouter(t, &fakeEntities{}, &fakeSources{updateConfigErr: sourcemodels.ErrSourceConfigurationNotFound}, &fakeEvents{})
	recorder = perform(missing, http.MethodPatch, "/api/v1/admin/source-configurations/"+id.String(), `{"config":{"url":"https://example.com/feed"}}`)
	if recorder.Code != http.StatusNotFound || decodeError(t, recorder).Error.Code != "not_found" {
		t.Errorf("status/code = %d/%q, want 404 not_found", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	fake := &fakeSources{configuration: sourcemodels.SourceConfiguration{ID: id, Type: "rss", Config: json.RawMessage(`{"url":"https://example.com/moved"}`)}}
	router = configRouter(t, &fakeEntities{}, fake, &fakeEvents{})
	recorder = perform(router, http.MethodPatch, "/api/v1/admin/source-configurations/"+id.String(), `{"config":{"url":"https://example.com/moved"}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if fake.gotUpdateID != id || !strings.Contains(string(fake.gotUpdateConfig), "example.com/moved") {
		t.Errorf("update id/config = %v/%s, want %v with the new payload", fake.gotUpdateID, fake.gotUpdateConfig, id)
	}
}

func TestCreateCalendarEventResolvesReferences(t *testing.T) {
	entityCode := entitymodels.Entity{ID: uuid.New(), Code: "US"}
	indicator := entitymodels.Indicator{ID: uuid.New(), EntityID: entityCode.ID}
	source := sourcemodels.Source{ID: uuid.New(), Name: "BLS", Type: "api"}
	body := `{"sourceId":"` + source.ID.String() + `","indicatorId":"` + indicator.ID.String() +
		`","scheduledAt":"2026-10-06T12:30:00Z","entityCodes":["US"]}`

	fake := &fakeEntities{entity: entityCode, indicator: indicator}
	sources := &fakeSources{source: source}
	events := &fakeEvents{record: calendarmodels.EventRecord{
		ID: uuid.New(), SourceID: source.ID, IndicatorID: indicator.ID,
		ScheduledAt: time.Date(2026, 10, 6, 12, 30, 0, 0, time.UTC),
	}}
	router := configRouter(t, fake, sources, events)

	recorder := perform(router, http.MethodPost, "/api/v1/admin/calendar-events", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var response calendarEventResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.CalendarEvent.IndicatorID != indicator.ID {
		t.Errorf("event indicator = %v, want %v", response.CalendarEvent.IndicatorID, indicator.ID)
	}
	if events.gotEntry == nil || len(events.gotEntry.EntityIDs) != 1 || events.gotEntry.EntityIDs[0] != entityCode.ID {
		t.Errorf("stored entry = %+v, want one resolved entity link", events.gotEntry)
	}

	recorder = perform(router, http.MethodPost, "/api/v1/admin/calendar-events",
		`{"sourceId":"`+source.ID.String()+`","indicatorId":"`+indicator.ID.String()+`"}`)
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_request" {
		t.Errorf("missing scheduledAt status/code = %d/%q, want 400 invalid_request", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	notFound := configRouter(t, fake, &fakeSources{sourceLookupErr: sourceservices.ErrSourceNotFound}, &fakeEvents{})
	recorder = perform(notFound, http.MethodPost, "/api/v1/admin/calendar-events", body)
	if recorder.Code != http.StatusNotFound || decodeError(t, recorder).Error.Code != "not_found" {
		t.Errorf("unknown source status/code = %d/%q, want 404 not_found", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	unknownEntity := configRouter(t, &fakeEntities{entity: entityCode, indicator: indicator, lookupErr: entityservices.ErrEntityNotFound}, sources, &fakeEvents{})
	recorder = perform(unknownEntity, http.MethodPost, "/api/v1/admin/calendar-events", body)
	if recorder.Code != http.StatusNotFound || decodeError(t, recorder).Error.Code != "not_found" {
		t.Errorf("unknown entity status/code = %d/%q, want 404 not_found", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestCreateKnowledgeTermKeepsLinks(t *testing.T) {
	indicatorID := uuid.New()
	fake := &fakeEntities{term: entitymodels.KnowledgeTerm{ID: uuid.New(), Name: "cpi", Type: "alias", IndicatorID: indicatorID}}
	router := configRouter(t, fake, &fakeSources{}, &fakeEvents{})

	recorder := perform(router, http.MethodPost, "/api/v1/admin/knowledge-terms",
		`{"name":"cpi","type":"alias","indicatorId":"`+indicatorID.String()+`"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var response knowledgeTermResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.KnowledgeTerm.EntityID != nil {
		t.Errorf("entityId = %v, want null without an explicit entity", response.KnowledgeTerm.EntityID)
	}
	if fake.gotTermName != "cpi" || fake.gotTermIndic == nil || *fake.gotTermIndic != indicatorID {
		t.Errorf("recorded term args = %q/%v, want cpi with the indicator link", fake.gotTermName, fake.gotTermIndic)
	}
}

func TestUnexpectedFailureReturns500(t *testing.T) {
	router := configRouter(t, &fakeEntities{pageErr: errors.New("boom")}, &fakeSources{}, &fakeEvents{})
	recorder := perform(router, http.MethodGet, "/api/v1/admin/entities", "")
	if recorder.Code != http.StatusInternalServerError || decodeError(t, recorder).Error.Code != "internal_error" {
		t.Errorf("status/code = %d/%q, want 500 internal_error", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestUpdateCalendarEvent(t *testing.T) {
	id := uuid.New()
	router := configRouter(t, &fakeEntities{}, &fakeSources{}, &fakeEvents{})

	recorder := perform(router, http.MethodPatch, "/api/v1/admin/calendar-events/nope", `{}`)
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_event_id" {
		t.Errorf("status/code = %d/%q, want 400 invalid_event_id", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	recorder = perform(router, http.MethodPatch, "/api/v1/admin/calendar-events/"+id.String(), `{"name":"US CPI","scheduledAt":"2026-10-14T12:30:00Z","actual":3.7}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}

	missing := configRouter(t, &fakeEntities{}, &fakeSources{}, &fakeEvents{updateErr: calendarmodels.ErrEventNotFound})
	recorder = perform(missing, http.MethodPatch, "/api/v1/admin/calendar-events/"+id.String(), `{"scheduledAt":"2026-10-14T12:30:00Z"}`)
	if recorder.Code != http.StatusNotFound || decodeError(t, recorder).Error.Code != "event_not_found" {
		t.Errorf("status/code = %d/%q, want 404 event_not_found", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestArchiveAndRestoreCalendarEvent(t *testing.T) {
	id := uuid.New()
	fake := &fakeEvents{}
	router := configRouter(t, &fakeEntities{}, &fakeSources{}, fake)

	recorder := perform(router, http.MethodPost, "/api/v1/admin/calendar-events/"+id.String()+"/archive", "")
	if recorder.Code != http.StatusNoContent || fake.gotArchiveID == nil || *fake.gotArchiveID != id {
		t.Errorf("archive → status %d id %v, want 204 for %v", recorder.Code, fake.gotArchiveID, id)
	}

	recorder = perform(router, http.MethodPost, "/api/v1/admin/calendar-events/"+id.String()+"/restore", "")
	if recorder.Code != http.StatusNoContent || fake.gotRestoreID == nil || *fake.gotRestoreID != id {
		t.Errorf("restore → status %d id %v, want 204 for %v", recorder.Code, fake.gotRestoreID, id)
	}

	missing := configRouter(t, &fakeEntities{}, &fakeSources{}, &fakeEvents{archiveErr: calendarmodels.ErrEventNotFound})
	recorder = perform(missing, http.MethodPost, "/api/v1/admin/calendar-events/"+id.String()+"/archive", "")
	if recorder.Code != http.StatusNotFound || decodeError(t, recorder).Error.Code != "event_not_found" {
		t.Errorf("status/code = %d/%q, want 404 event_not_found", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}
