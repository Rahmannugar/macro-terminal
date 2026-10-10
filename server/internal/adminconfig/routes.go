package adminconfig

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	entityservices "github.com/Rahmannugar/macro-terminal/server/internal/entities/services"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/safehttp"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourceservices "github.com/Rahmannugar/macro-terminal/server/internal/sources/services"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	adminDefaultPageSize int32 = 25
	adminMaximumPageSize int32 = 100
)

func RegisterAdminConfigRoutes(router gin.IRoutes, entities Entities, sources Sources, events Events) {
	router.GET("/entities", listEntities(entities))
	router.POST("/entities", createEntity(entities))
	router.GET("/entity-pairs", listEntityPairs(entities))
	router.POST("/entity-pairs", createEntityPair(entities))
	router.GET("/indicators", listIndicators(entities))
	router.POST("/indicators", createIndicator(entities))
	router.GET("/knowledge-terms", listKnowledgeTerms(entities))
	router.POST("/knowledge-terms", createKnowledgeTerm(entities))
	router.GET("/sources", listSources(sources))
	router.POST("/sources", createSource(sources))
	router.GET("/source-configurations", listSourceConfigurations(sources))
	router.POST("/source-configurations", createSourceConfiguration(sources))
	router.PATCH("/source-configurations/:id", updateSourceConfiguration(sources))
	router.GET("/calendar-events", listCalendarEvents(events))
	router.POST("/calendar-events", createCalendarEvent(entities, sources, events))
	router.PATCH("/calendar-events/:id", updateCalendarEvent(events))
	router.POST("/calendar-events/:id/archive", archiveCalendarEvent(events))
	router.POST("/calendar-events/:id/restore", restoreCalendarEvent(events))
}

func parseListQuery(ctx *gin.Context) (int32, *paging.Cursor, bool) {
	limit, ok := parseAdminLimit(ctx.Query("limit"))
	if !ok {
		writeFailure(ctx, http.StatusBadRequest, "invalid_limit", "Limit must be a number between 1 and 100.")
		return 0, nil, false
	}
	cursor, err := paging.DecodeCursor(ctx.Query("cursor"))
	if err != nil {
		writeFailure(ctx, http.StatusBadRequest, "invalid_cursor", "Use the next cursor returned by the previous page.")
		return 0, nil, false
	}
	return limit, cursor, true
}

func parseAdminLimit(value string) (int32, bool) {
	if value == "" {
		return adminDefaultPageSize, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > int(adminMaximumPageSize) {
		return 0, false
	}
	return int32(limit), true
}

func encodeNextCursor(next *paging.Cursor) *string {
	if next == nil {
		return nil
	}
	encoded := paging.EncodeCursor(*next)
	return &encoded
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}

func writeConfigFailure(ctx *gin.Context, err error) {
	if failure, ok := classifyConfigFailure(err); ok {
		writeFailure(ctx, failure.status, failure.code, failure.message)
		return
	}
	slog.Default().ErrorContext(ctx.Request.Context(), "admin configuration failed", "error", err)
	writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
}

type configFailure struct {
	status  int
	code    string
	message string
}

func classifyConfigFailure(err error) (configFailure, bool) {
	var uniqueViolation *pgconn.PgError
	switch {
	case errors.Is(err, entityservices.ErrEntityCodeRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "Entity code is required."}, true
	case errors.Is(err, entityservices.ErrEntityNameRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "Entity name is required."}, true
	case errors.Is(err, entityservices.ErrEntityTypeRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "Entity type is required."}, true
	case errors.Is(err, entityservices.ErrPairSymbolRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "Entity pair symbol is required."}, true
	case errors.Is(err, entityservices.ErrPairEntitiesMustDiffer):
		return configFailure{http.StatusBadRequest, "invalid_request", "An entity pair needs two different entities."}, true
	case errors.Is(err, entityservices.ErrIndicatorNameRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "Indicator name is required."}, true
	case errors.Is(err, entityservices.ErrIndicatorTypeRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "Indicator type is required."}, true
	case errors.Is(err, entityservices.ErrKnowledgeTermNameRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "Knowledge term name is required."}, true
	case errors.Is(err, entityservices.ErrKnowledgeTermTypeRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "Knowledge term type is required."}, true
	case errors.Is(err, entityservices.ErrKnowledgeTermLinkRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "A knowledge term must link to an entity or an indicator."}, true
	case errors.Is(err, entityservices.ErrKnowledgeTermEntityMismatch):
		return configFailure{http.StatusBadRequest, "invalid_request", "The entity must belong to the indicator's entity."}, true
	case errors.Is(err, sourceservices.ErrSourceNameRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "Source name is required."}, true
	case errors.Is(err, sourceservices.ErrSourceTypeRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "Source type is required."}, true
	case errors.Is(err, ErrEventScheduledAtRequired):
		return configFailure{http.StatusBadRequest, "invalid_request", "A calendar event needs a scheduled time."}, true
	case errors.Is(err, calendarmodels.ErrEventNotFound):
		return configFailure{http.StatusNotFound, "event_not_found", "That calendar event does not exist."}, true

	case errors.Is(err, sourceservices.ErrConfigurationTypeInvalid):
		return configFailure{http.StatusBadRequest, "invalid_config", "Configuration type must be api, rss, or web."}, true
	case errors.Is(err, sourceservices.ErrConfigurationNotObject):
		return configFailure{http.StatusBadRequest, "invalid_config", "Configuration must be a JSON object."}, true
	case errors.Is(err, sourceservices.ErrConfigurationURLRequired):
		return configFailure{http.StatusBadRequest, "invalid_config", "Configuration needs a url."}, true
	case errors.Is(err, sourceservices.ErrConfigurationURLInvalid):
		return configFailure{http.StatusBadRequest, "invalid_config", "Configuration url must be an absolute http or https URL."}, true
	case errors.Is(err, sourceservices.ErrConfigurationIntervalInvalid):
		return configFailure{http.StatusBadRequest, "invalid_config", "min_interval_s must be a positive integer."}, true
	case errors.Is(err, sourceservices.ErrConfigurationSecretInConfig):
		return configFailure{http.StatusBadRequest, "invalid_config", "Configuration must not store secret values; reference an environment variable through an *_env key instead."}, true

	case errors.Is(err, safehttp.ErrBlockedAddress):
		return configFailure{http.StatusBadRequest, "destination_refused", "The url points at an address this server may not fetch."}, true

	case errors.Is(err, entityservices.ErrEntityNotFound):
		return configFailure{http.StatusNotFound, "not_found", "No entity has that code."}, true
	case errors.Is(err, entityservices.ErrIndicatorNotFound):
		return configFailure{http.StatusNotFound, "not_found", "No indicator has that id."}, true
	case errors.Is(err, sourceservices.ErrSourceNotFound):
		return configFailure{http.StatusNotFound, "not_found", "No source has that id."}, true
	case errors.Is(err, sourcemodels.ErrSourceConfigurationNotFound):
		return configFailure{http.StatusNotFound, "not_found", "No source configuration has that id."}, true
	case errors.Is(err, pgx.ErrNoRows):
		return configFailure{http.StatusNotFound, "not_found", "The referenced record does not exist."}, true

	case errors.Is(err, entityservices.ErrEntityCodeExists):
		return configFailure{http.StatusConflict, "already_exists", "An entity with that code already exists."}, true
	case errors.Is(err, entityservices.ErrPairSymbolExists):
		return configFailure{http.StatusConflict, "already_exists", "An entity pair with that symbol already exists."}, true
	case errors.Is(err, sourceservices.ErrSourceNameExists):
		return configFailure{http.StatusConflict, "already_exists", "A source with that name already exists."}, true
	case errors.Is(err, sourceservices.ErrConfigurationExists):
		return configFailure{http.StatusConflict, "already_exists", "That source already has a configuration for this url."}, true
	case errors.As(err, &uniqueViolation) && uniqueViolation.Code == "23505":
		return configFailure{http.StatusConflict, "already_exists", "A record with the same unique values already exists."}, true
	}
	return configFailure{}, false
}
