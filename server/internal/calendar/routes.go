package calendar

import (
	"context"
	"net/http"
	"strconv"
	"time"

	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	defaultPageSize = 25
	maximumPageSize = 100
)

type UpcomingEvents interface {
	UpcomingEventsPage(
		ctx context.Context,
		notBefore time.Time,
		cursor *paging.Cursor,
		limit int32,
	) ([]calendarmodels.UpcomingEvent, *paging.Cursor, error)
}

func RegisterRoutes(router gin.IRoutes, events UpcomingEvents) {
	router.GET("/api/v1/calendar-events", listUpcoming(events))
}

type sourceJSON struct {
	ID   uuid.UUID `json:"id" example:"3f8b2a60-9d1e-4a63-8f6d-2b9c1f4e7a55"`
	Name string    `json:"name" example:"FinanceCalendar"`
}

type indicatorJSON struct {
	ID   uuid.UUID `json:"id" example:"6b5f8e2d-4c9a-4f1e-b8d7-3a2c6e9f1b40"`
	Name string    `json:"name" example:"CPI"`
}

type upcomingEventJSON struct {
	ID          uuid.UUID     `json:"id" example:"7d3b9f1e-5a4c-4e8b-9f2d-1c6a8b0e4f73"`
	ScheduledAt time.Time     `json:"scheduledAt" example:"2026-10-06T12:30:00Z"`
	ReleasedAt  *time.Time    `json:"releasedAt" example:"2026-10-06T12:30:00Z"`
	Previous    *float64      `json:"previous" example:"250000"`
	Consensus   *float64      `json:"consensus" example:"255000"`
	Actual      *float64      `json:"actual" example:"261000"`
	Indicator   indicatorJSON `json:"indicator"`
	Source      sourceJSON    `json:"source"`
}

type upcomingEventListResponse struct {
	CalendarEvents []upcomingEventJSON `json:"calendarEvents"`
	NextCursor     *string             `json:"nextCursor"`
}

// @Summary List upcoming calendar events.
// @Description Returns scheduled events from now forward, soonest first. Pass the returned nextCursor to fetch the next page.
// @Tags calendar
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} upcomingEventListResponse "The events and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/calendar-events [get]
func listUpcoming(events UpcomingEvents) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit := int32(defaultPageSize)
		if raw := ctx.Query("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > maximumPageSize {
				writeFailure(ctx, http.StatusBadRequest, "invalid_limit", "Limit must be a number between 1 and 100.")
				return
			}
			limit = int32(parsed)
		}
		cursor, err := paging.DecodeCursor(ctx.Query("cursor"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_cursor", "Use the next cursor returned by the previous page.")
			return
		}
		rows, next, err := events.UpcomingEventsPage(ctx.Request.Context(), time.Now().UTC(), cursor, limit)
		if err != nil {
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		response := upcomingEventListResponse{CalendarEvents: make([]upcomingEventJSON, 0, len(rows))}
		for _, row := range rows {
			response.CalendarEvents = append(response.CalendarEvents, upcomingEventJSON{
				ID:          row.ID,
				ScheduledAt: row.ScheduledAt,
				ReleasedAt:  row.ReleasedAt,
				Previous:    row.Previous,
				Consensus:   row.Consensus,
				Actual:      row.Actual,
				Indicator:   indicatorJSON{ID: row.IndicatorID, Name: row.IndicatorName},
				Source:      sourceJSON{ID: row.SourceID, Name: row.SourceName},
			})
		}
		if next != nil {
			encoded := paging.EncodeCursor(*next)
			response.NextCursor = &encoded
		}
		ctx.JSON(http.StatusOK, response)
	}
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
