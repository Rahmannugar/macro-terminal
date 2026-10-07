package adminconfig

import (
	"context"
	"net/http"
	"time"

	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type calendarEventJSON struct {
	ID          uuid.UUID  `json:"id" example:"7d3b9f1e-5a4c-4e8b-9f2d-1c6a8b0e4f73"`
	SourceID    uuid.UUID  `json:"sourceId" example:"5a1c3e7b-9d2f-4c8a-b6e1-7f4d2a9c3e50"`
	IndicatorID uuid.UUID  `json:"indicatorId" example:"6b5f8e2d-4c9a-4f1e-b8d7-3a2c6e9f1b40"`
	ScheduledAt time.Time  `json:"scheduledAt" example:"2026-10-06T12:30:00Z"`
	ReleasedAt  *time.Time `json:"releasedAt" example:"2026-10-06T12:30:00Z"`
	Previous    *float64   `json:"previous" example:"250000"`
	Consensus   *float64   `json:"consensus" example:"255000"`
	Actual      *float64   `json:"actual" example:"261000"`
	CreatedAt   time.Time  `json:"createdAt" example:"2026-10-06T09:00:00Z"`
	UpdatedAt   time.Time  `json:"updatedAt" example:"2026-10-06T09:00:00Z"`
}

type calendarEventListResponse struct {
	CalendarEvents []calendarEventJSON `json:"calendarEvents"`
	NextCursor     *string             `json:"nextCursor"`
}

type calendarEventResponse struct {
	CalendarEvent calendarEventJSON `json:"calendarEvent"`
}

type createCalendarEventRequest struct {
	SourceID    uuid.UUID  `json:"sourceId" example:"5a1c3e7b-9d2f-4c8a-b6e1-7f4d2a9c3e50"`
	IndicatorID uuid.UUID  `json:"indicatorId" example:"6b5f8e2d-4c9a-4f1e-b8d7-3a2c6e9f1b40"`
	ScheduledAt time.Time  `json:"scheduledAt" example:"2026-10-06T12:30:00Z"`
	ReleasedAt  *time.Time `json:"releasedAt" example:"2026-10-06T12:30:00Z"`
	Previous    *float64   `json:"previous" example:"250000"`
	Consensus   *float64   `json:"consensus" example:"255000"`
	Actual      *float64   `json:"actual" example:"261000"`
	EntityCodes []string   `json:"entityCodes" example:"US"`
}

func newCalendarEventJSON(record calendarmodels.EventRecord) calendarEventJSON {
	return calendarEventJSON{
		ID:          record.ID,
		SourceID:    record.SourceID,
		IndicatorID: record.IndicatorID,
		ScheduledAt: record.ScheduledAt,
		ReleasedAt:  record.ReleasedAt,
		Previous:    record.Previous,
		Consensus:   record.Consensus,
		Actual:      record.Actual,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
}

// @Summary List calendar events.
// @Description Returns stored events newest first. Pass the returned nextCursor to fetch the next page.
// @Tags admin
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} calendarEventListResponse "The events and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/calendar-events [get]
func listCalendarEvents(events Events) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit, cursor, ok := parseListQuery(ctx)
		if !ok {
			return
		}
		rows, next, err := events.ListCalendarEventsPage(ctx.Request.Context(), cursor, limit)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		response := calendarEventListResponse{CalendarEvents: make([]calendarEventJSON, 0, len(rows))}
		for _, row := range rows {
			response.CalendarEvents = append(response.CalendarEvents, newCalendarEventJSON(row))
		}
		response.NextCursor = encodeNextCursor(next)
		ctx.JSON(http.StatusOK, response)
	}
}

// @Summary Create a calendar event.
// @Description Stores an event under an existing source and indicator, optionally linked to entities. Values are optional; the scheduled time is required. The same source, indicator, and scheduled time may appear only once.
// @Tags admin
// @Param body body createCalendarEventRequest true "The event to store."
// @Success 200 {object} calendarEventResponse "The stored event."
// @Failure 400 {object} openapi.Error "A field is missing or a reference is unknown."
// @Failure 404 {object} openapi.Error "The source, indicator, or an entity code does not exist."
// @Failure 409 {object} openapi.Error "An event with that source, indicator, and scheduled time already exists."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/calendar-events [post]
func createCalendarEvent(entities Entities, sources Sources, events Events) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request createCalendarEventRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		record, err := storeCalendarEvent(ctx.Request.Context(), entities, sources, events, request)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, calendarEventResponse{CalendarEvent: newCalendarEventJSON(record)})
	}
}

// storeCalendarEvent resolves every reference before writing, so an
// unknown source, indicator, or entity code is refused with a 404
// instead of failing on the foreign key.
func storeCalendarEvent(
	ctx context.Context,
	entities Entities,
	sources Sources,
	events Events,
	input createCalendarEventRequest,
) (calendarmodels.EventRecord, error) {
	if input.ScheduledAt.IsZero() {
		return calendarmodels.EventRecord{}, ErrEventScheduledAtRequired
	}
	if _, err := sources.SourceByID(ctx, input.SourceID); err != nil {
		return calendarmodels.EventRecord{}, err
	}
	if _, err := entities.IndicatorByID(ctx, input.IndicatorID); err != nil {
		return calendarmodels.EventRecord{}, err
	}
	entityIDs := make([]uuid.UUID, 0, len(input.EntityCodes))
	for _, code := range input.EntityCodes {
		entity, err := entities.EntityByCode(ctx, code)
		if err != nil {
			return calendarmodels.EventRecord{}, err
		}
		entityIDs = append(entityIDs, entity.ID)
	}
	return events.CreateCalendarEvent(ctx, calendarmodels.CreateEventEntry{
		SourceID:    input.SourceID,
		IndicatorID: input.IndicatorID,
		ScheduledAt: input.ScheduledAt,
		ReleasedAt:  input.ReleasedAt,
		Previous:    input.Previous,
		Consensus:   input.Consensus,
		Actual:      input.Actual,
		EntityIDs:   entityIDs,
	})
}
