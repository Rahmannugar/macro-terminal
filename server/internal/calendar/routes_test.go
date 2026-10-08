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

type fakeUpcoming struct {
	rows []calendarmodels.UpcomingEvent
	next *paging.Cursor

	gotNotBefore time.Time
	gotLimit     int32
}

func (fake *fakeUpcoming) UpcomingEventsPage(_ context.Context, notBefore time.Time, _ *paging.Cursor, limit int32) ([]calendarmodels.UpcomingEvent, *paging.Cursor, error) {
	fake.gotNotBefore = notBefore
	fake.gotLimit = limit
	return fake.rows, fake.next, nil
}

func calendarRouter(t *testing.T, events UpcomingEvents) *gin.Engine {
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

func upcomingFixture() calendarmodels.UpcomingEvent {
	return calendarmodels.UpcomingEvent{
		ID:            uuid.New(),
		SourceID:      uuid.New(),
		SourceName:    "FinanceCalendar",
		IndicatorID:   uuid.New(),
		IndicatorName: "CPI",
		ScheduledAt:   time.Date(2026, 10, 7, 12, 30, 0, 0, time.UTC),
	}
}

func TestCalendarRejectsBadPageParameters(t *testing.T) {
	router := calendarRouter(t, &fakeUpcoming{})

	recorder := calendarPerform(router, "/api/v1/calendar-events?limit=0")
	if recorder.Code != http.StatusBadRequest || calendarDecodeError(t, recorder).Error.Code != "invalid_limit" {
		t.Errorf("limit=0 → status/code = %d/%q, want 400 invalid_limit", recorder.Code, calendarDecodeError(t, recorder).Error.Code)
	}

	recorder = calendarPerform(router, "/api/v1/calendar-events?cursor=not-base64!!")
	if recorder.Code != http.StatusBadRequest || calendarDecodeError(t, recorder).Error.Code != "invalid_cursor" {
		t.Errorf("garbage cursor → status/code = %d/%q, want 400 invalid_cursor", recorder.Code, calendarDecodeError(t, recorder).Error.Code)
	}
}

func TestCalendarReturnsUpcomingEvents(t *testing.T) {
	event := upcomingFixture()
	fake := &fakeUpcoming{
		rows: []calendarmodels.UpcomingEvent{event},
		next: &paging.Cursor{At: event.ScheduledAt, ID: event.ID},
	}
	router := calendarRouter(t, fake)
	before := time.Now().UTC().Add(-time.Minute)

	recorder := calendarPerform(router, "/api/v1/calendar-events")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var page upcomingEventListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if len(page.CalendarEvents) != 1 {
		t.Fatalf("calendarEvents = %+v, want one event", page.CalendarEvents)
	}
	row := page.CalendarEvents[0]
	if row.ID != event.ID || row.Indicator.Name != "CPI" || row.Source.Name != "FinanceCalendar" {
		t.Errorf("event = %+v, want the stored event with indicator and source names", row)
	}
	if page.NextCursor == nil {
		t.Error("nextCursor = nil, want the page cursor")
	}
	if fake.gotNotBefore.Before(before) {
		t.Errorf("notBefore = %s, want anchored at the request time", fake.gotNotBefore)
	}
	if fake.gotLimit != defaultPageSize {
		t.Errorf("limit = %d, want %d", fake.gotLimit, defaultPageSize)
	}
}
