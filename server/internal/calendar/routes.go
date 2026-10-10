package calendar

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/authentication"
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

// EventReader serves both sides of the calendar: upcoming rows from now
// forward and released rows before now, under the same filters.
type EventReader interface {
	UpcomingEventsPage(
		ctx context.Context,
		query calendarmodels.EventPageQuery,
		cursor *paging.Cursor,
		limit int32,
	) ([]calendarmodels.EventRow, *paging.Cursor, error)
	ReleasedEventsPage(
		ctx context.Context,
		query calendarmodels.EventPageQuery,
		cursor *paging.Cursor,
		limit int32,
	) ([]calendarmodels.EventRow, *paging.Cursor, error)
}

// RegisterRoutes mounts the calendar list on the signed-in user's group.
func RegisterRoutes(router gin.IRoutes, events EventReader) {
	router.GET("/calendar-events", listEvents(events))
}

var countryCodeList = regexp.MustCompile(`^[A-Z]{2}(,[A-Z]{2})*$`)

type sourceJSON struct {
	ID   uuid.UUID `json:"id" example:"3f8b2a60-9d1e-4a63-8f6d-2b9c1f4e7a55"`
	Name string    `json:"name" example:"FinanceCalendar"`
}

type indicatorJSON struct {
	ID   uuid.UUID `json:"id" example:"6b5f8e2d-4c9a-4f1e-b8d7-3a2c6e9f1b40"`
	Name string    `json:"name" example:"CPI"`
}

type eventJSON struct {
	ID          uuid.UUID     `json:"id" example:"7d3b9f1e-5a4c-4e8b-9f2d-1c6a8b0e4f73"`
	ScheduledAt time.Time     `json:"scheduledAt" example:"2026-10-06T12:30:00Z"`
	ReleasedAt  *time.Time    `json:"releasedAt" example:"2026-10-06T12:30:00Z"`
	Previous    *float64      `json:"previous" example:"250000"`
	Consensus   *float64      `json:"consensus" example:"255000"`
	Actual      *float64      `json:"actual" example:"261000"`
	CountryCode string        `json:"countryCode" example:"US"`
	Currency    string        `json:"currency" example:"USD"`
	Importance  string        `json:"importance" example:"high"`
	Revision    int32         `json:"revision" example:"0"`
	Indicator   indicatorJSON `json:"indicator"`
	Name        string        `json:"name" example:"US CPI"`
	Source      sourceJSON    `json:"source"`
}

type eventListResponse struct {
	CalendarEvents []eventJSON `json:"calendarEvents"`
	NextCursor     *string     `json:"nextCursor"`
}

// @Summary List calendar events.
// @Description Returns upcoming events from now forward (the default) or released events before now, soonest first within each side. Pass the returned nextCursor to fetch the next page. Filters combine with AND.
// @Tags calendar
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Param scope query string false "Which side to return: upcoming (default) or released."
// @Param country query string false "Comma-separated ISO country codes to include, for example US,JP."
// @Param importance query string false "Comma-separated importance levels to include: high, medium, low."
// @Param watch query bool false "Return only events linked to pairs on the signed-in user's watch list."
// @Success 200 {object} eventListResponse "The events and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "A query parameter is invalid."
// @Failure 401 {object} openapi.Error "The watch filter requires a signed-in account."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/calendar-events [get]
func listEvents(events EventReader) gin.HandlerFunc {
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
		scope := strings.TrimSpace(ctx.Query("scope"))
		if scope == "" {
			scope = "upcoming"
		}
		if scope != "upcoming" && scope != "released" {
			writeFailure(ctx, http.StatusBadRequest, "invalid_scope", "Scope must be upcoming or released.")
			return
		}
		countries := strings.TrimSpace(ctx.Query("country"))
		if countries != "" && (!countryCodeList.MatchString(countries) || strings.Count(countries, ",") >= 24) {
			writeFailure(ctx, http.StatusBadRequest, "invalid_country", "Country must be comma-separated two-letter codes like US,JP.")
			return
		}
		importances, err := parseImportances(ctx.Query("importance"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_importance", "Importance must be comma-separated levels from high, medium, low.")
			return
		}
		watcher, err := parseWatcher(ctx)
		if errors.Is(err, errUnauthenticated) {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to filter the calendar by your watch list.")
			return
		}
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_watch", "Watch must be 1 or 0.")
			return
		}

		query := calendarmodels.EventPageQuery{
			Now:         time.Now().UTC(),
			Countries:   splitCodes(countries),
			Importances: importances,
			Watcher:     watcher,
		}
		var (
			rows []calendarmodels.EventRow
			next *paging.Cursor
		)
		if scope == "released" {
			rows, next, err = events.ReleasedEventsPage(ctx.Request.Context(), query, cursor, limit)
		} else {
			rows, next, err = events.UpcomingEventsPage(ctx.Request.Context(), query, cursor, limit)
		}
		if err != nil {
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		response := eventListResponse{CalendarEvents: make([]eventJSON, 0, len(rows))}
		for _, row := range rows {
			response.CalendarEvents = append(response.CalendarEvents, eventJSON{
				ID:          row.ID,
				ScheduledAt: row.ScheduledAt,
				ReleasedAt:  row.ReleasedAt,
				Previous:    row.Previous,
				Consensus:   row.Consensus,
				Actual:      row.Actual,
				CountryCode: row.CountryCode,
				Currency:    row.Currency,
				Importance:  row.Importance,
				Revision:    row.Revision,
				Name:        row.Name,
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

var (
	errUnauthenticated   = errors.New("unauthenticated")
	errInvalidWatch      = errors.New("invalid watch")
	errInvalidImportance = errors.New("invalid importance")
)

func parseImportances(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	if len(parts) > 5 {
		return nil, errInvalidImportance
	}
	levels := make([]string, 0, len(parts))
	for _, part := range parts {
		level := strings.TrimSpace(part)
		switch level {
		case "high", "medium", "low":
			levels = append(levels, level)
		default:
			return nil, errInvalidImportance
		}
	}
	return levels, nil
}

func parseWatcher(ctx *gin.Context) (*uuid.UUID, error) {
	switch strings.TrimSpace(ctx.Query("watch")) {
	case "", "0", "false":
		return nil, nil
	case "1", "true":
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			return nil, errUnauthenticated
		}
		return &account.User.ID, nil
	default:
		return nil, errInvalidWatch
	}
}

func splitCodes(codes string) []string {
	if codes == "" {
		return nil
	}
	return strings.Split(codes, ",")
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
