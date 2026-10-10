// Package adminconfig serves the administrator's create, list, and
// update endpoints for reference configuration behind the admin guard.
package adminconfig

import (
	"context"
	"encoding/json"
	"errors"

	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/google/uuid"
)

var ErrEventScheduledAtRequired = errors.New("calendar event scheduled time is required")

type Entities interface {
	CreateEntity(ctx context.Context, code, name, entityType string) (entitymodels.Entity, error)
	CreateEntityPair(ctx context.Context, baseCode, quoteCode, symbol string) (entitymodels.EntityPair, error)
	CreateIndicator(ctx context.Context, name, indicatorType, entityCode string) (entitymodels.Indicator, error)
	CreateKnowledgeTerm(ctx context.Context, name, termType, entityCode string, indicatorID *uuid.UUID) (entitymodels.KnowledgeTerm, error)
	EntityByCode(ctx context.Context, code string) (entitymodels.Entity, error)
	IndicatorByID(ctx context.Context, id uuid.UUID) (entitymodels.Indicator, error)
	ListEntitiesPage(ctx context.Context, cursor *paging.Cursor, limit int32) ([]entitymodels.Entity, *paging.Cursor, error)
	ListEntityPairsPage(ctx context.Context, cursor *paging.Cursor, limit int32) ([]entitymodels.EntityPair, *paging.Cursor, error)
	ListIndicatorsPage(ctx context.Context, cursor *paging.Cursor, limit int32) ([]entitymodels.Indicator, *paging.Cursor, error)
	ListKnowledgeTermsPage(ctx context.Context, cursor *paging.Cursor, limit int32) ([]entitymodels.KnowledgeTerm, *paging.Cursor, error)
}

type Sources interface {
	CreateSource(ctx context.Context, name, sourceType string) (sourcemodels.Source, error)
	CreateSourceConfiguration(ctx context.Context, sourceID uuid.UUID, configType string, config json.RawMessage) (sourcemodels.SourceConfiguration, error)
	UpdateSourceConfiguration(ctx context.Context, id uuid.UUID, config json.RawMessage) (sourcemodels.SourceConfiguration, error)
	SourceByID(ctx context.Context, id uuid.UUID) (sourcemodels.Source, error)
	ListSourcesPage(ctx context.Context, cursor *paging.Cursor, limit int32) ([]sourcemodels.Source, *paging.Cursor, error)
	ListSourceConfigurationsPage(ctx context.Context, cursor *paging.Cursor, limit int32) ([]sourcemodels.SourceConfiguration, *paging.Cursor, error)
}

type Events interface {
	CreateCalendarEvent(ctx context.Context, entry calendarmodels.CreateEventEntry) (calendarmodels.EventRecord, error)
	ListCalendarEventsPage(ctx context.Context, cursor *paging.Cursor, limit int32) ([]calendarmodels.EventRecord, *paging.Cursor, error)
	UpdateCalendarEvent(ctx context.Context, entry calendarmodels.UpdateEventEntry) (calendarmodels.EventRecord, error)
	ArchiveCalendarEvent(ctx context.Context, id uuid.UUID) error
	RestoreCalendarEvent(ctx context.Context, id uuid.UUID) error
}
