package calendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type fakeEvents struct {
	rows []calendarmodels.EventRow
	next *paging.Cursor

	gotUpcoming   calendarmodels.EventPageQuery
	gotReleased   calendarmodels.EventPageQuery
	gotLimit      int32
	upcomingCalls int
	releasedCalls int
}

func (fake *fakeEvents) UpcomingEventsPage(_ context.Context, query calendarmodels.EventPageQuery, _ *paging.Cursor, limit int32) ([]calendarmodels.EventRow, *paging.Cursor, error) {
	fake.gotUpcoming = query
	fake.gotLimit = limit
	fake.upcomingCalls++
	return fake.rows, fake.next, nil
}

func (fake *fakeEvents) ReleasedEventsPage(_ context.Context, query calendarmodels.EventPageQuery, _ *paging.Cursor, limit int32) ([]calendarmodels.EventRow, *paging.Cursor, error) {
	fake.gotReleased = query
	fake.gotLimit = limit
	fake.releasedCalls++
	return fake.rows, fake.next, nil
}

func calendarRouter(t *testing.T, events EventReader) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, events)
	return router
}

func calendarPerform(router *gin.Engine, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func calendarDecodeError(t *testing.T, recorder *httptest.ResponseRecorder) openapi.Error {
	t.Helper()
	var failure openapi.Error
	if err := json.Unmarshal(recorder.Body.Bytes(), &failure); err != nil {
		t.Fatalf("decode failure: %v", err)
	}
	return failure
}

func eventFixture() calendarmodels.EventRow {
	return calendarmodels.EventRow{
		ID:            uuid.New(),
		SourceID:      uuid.New(),
		SourceName:    "Xoomar",
		IndicatorID:   uuid.New(),
		IndicatorName: "CPI",
		ScheduledAt:   time.Date(2026, 10, 7, 12, 30, 0, 0, time.UTC),
		CountryCode:   "US",
		Currency:      "USD",
		Importance:    "high",
		Revision:      1,
	}
}

func TestCalendarRejectsBadQueryParameters(t *testing.T) {
	router := calendarRouter(t, &fakeEvents{})

	cases := []struct {
		target string
		code   string
	}{
		{"/calendar-events?limit=0", "invalid_limit"},
		{"/calendar-events?cursor=not-base64!!", "invalid_cursor"},
		{"/calendar-events?scope=tomorrow", "invalid_scope"},
		{"/calendar-events?country=USA", "invalid_country"},
		{"/calendar-events?importance=extreme", "invalid_importance"},
		{"/calendar-events?watch=maybe", "invalid_watch"},
	}
	for _, testCase := range cases {
		recorder := calendarPerform(router, testCase.target)
		failure := calendarDecodeError(t, recorder)
		if recorder.Code != http.StatusBadRequest || failure.Error.Code != testCase.code {
			t.Errorf("%s → status/code = %d/%q, want 400 %q", testCase.target, recorder.Code, failure.Error.Code, testCase.code)
		}
	}
}

func TestCalendarReturnsUpcomingEventsWithFilters(t *testing.T) {
	event := eventFixture()
	fake := &fakeEvents{
		rows: []calendarmodels.EventRow{event},
		next: &paging.Cursor{At: event.ScheduledAt, ID: event.ID},
	}
	router := calendarRouter(t, fake)
	before := time.Now().UTC().Add(-time.Minute)

	recorder := calendarPerform(router, "/calendar-events?country=US,JP&importance=high,low")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var page eventListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if len(page.CalendarEvents) != 1 {
		t.Fatalf("calendarEvents = %+v, want one event", page.CalendarEvents)
	}
	row := page.CalendarEvents[0]
	if row.ID != event.ID || row.Indicator.Name != "CPI" || row.Source.Name != "Xoomar" {
		t.Errorf("event = %+v, want the stored event with indicator and source names", row)
	}
	if row.CountryCode != "US" || row.Importance != "high" || row.Revision != 1 {
		t.Errorf("provider tags = %q/%q/%d, want US/high/1", row.CountryCode, row.Importance, row.Revision)
	}
	if page.NextCursor == nil {
		t.Error("nextCursor = nil, want the page cursor")
	}
	if fake.gotUpcoming.Now.Before(before) {
		t.Errorf("Now = %s, want anchored at the request time", fake.gotUpcoming.Now)
	}
	if fake.gotUpcoming.Countries == nil || fake.gotUpcoming.Countries[0] != "US" || fake.gotUpcoming.Countries[1] != "JP" {
		t.Errorf("Countries = %v, want [US JP]", fake.gotUpcoming.Countries)
	}
	if fake.gotUpcoming.Importances == nil || fake.gotUpcoming.Importances[0] != "high" {
		t.Errorf("Importances = %v, want [high low]", fake.gotUpcoming.Importances)
	}
	if fake.gotLimit != defaultPageSize {
		t.Errorf("limit = %d, want %d", fake.gotLimit, defaultPageSize)
	}
	if fake.releasedCalls != 0 {
		t.Errorf("released calls = %d, want 0", fake.releasedCalls)
	}
}

func TestCalendarReturnsReleasedEvents(t *testing.T) {
	event := eventFixture()
	fake := &fakeEvents{rows: []calendarmodels.EventRow{event}}
	router := calendarRouter(t, fake)

	recorder := calendarPerform(router, "/calendar-events?scope=released&importance=medium")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if fake.releasedCalls != 1 || fake.upcomingCalls != 0 {
		t.Errorf("calls = upcoming %d released %d, want 0/1", fake.upcomingCalls, fake.releasedCalls)
	}
	if fake.gotReleased.Importances == nil || fake.gotReleased.Importances[0] != "medium" {
		t.Errorf("Importances = %v, want [medium]", fake.gotReleased.Importances)
	}
}

func TestCalendarWatchFilterRequiresSession(t *testing.T) {
	router := calendarRouter(t, &fakeEvents{})

	recorder := calendarPerform(router, "/calendar-events?watch=1")
	if recorder.Code != http.StatusUnauthorized || calendarDecodeError(t, recorder).Error.Code != "unauthenticated" {
		t.Errorf("watch=1 without session → %d/%q, want 401 unauthenticated", recorder.Code, calendarDecodeError(t, recorder).Error.Code)
	}
}
