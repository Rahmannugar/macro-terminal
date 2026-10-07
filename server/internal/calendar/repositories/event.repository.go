package repositories

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	calendardb "github.com/Rahmannugar/macro-terminal/server/internal/calendar/repositories/generated"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventRepository struct {
	pool    *pgxpool.Pool
	queries *calendardb.Queries
	store   *cache.JSONStore
}

func NewEventRepository(pool *pgxpool.Pool, store *cache.JSONStore) *EventRepository {
	return &EventRepository{pool: pool, queries: calendardb.New(pool), store: store}
}

// PersistEvents stores one configuration's classified events and their
// entity links in a single transaction. Re-running the same pass changes
// no rows: events upsert on source, indicator, and schedule; links
// conflict on their own keys. A value refresh overwrites only the fields
// the provider actually sent — a missing actual never erases a release,
// and a release timestamp is set once.
func (repository *EventRepository) PersistEvents(
	ctx context.Context,
	entries []models.PersistEntry,
) (models.PersistStats, error) {
	stats := models.PersistStats{}
	if len(entries) == 0 {
		return stats, nil
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return stats, fmt.Errorf("begin persist transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repository.queries.WithTx(tx)

	stored := make([]models.StoredEvent, 0, len(entries))
	for _, entry := range entries {
		previous, err := numericValue(entry.Previous)
		if err != nil {
			return stats, err
		}
		consensus, err := numericValue(entry.Consensus)
		if err != nil {
			return stats, err
		}
		actual, err := numericValue(entry.Actual)
		if err != nil {
			return stats, err
		}

		event, err := queries.UpsertCalendarEvent(ctx, calendardb.UpsertCalendarEventParams{
			ID:          uuid.New(),
			SourceID:    entry.SourceID,
			IndicatorID: entry.IndicatorID,
			ScheduledAt: timestampValueUnchecked(entry.ScheduledAt),
			ReleasedAt:  timestampValue(entry.ReleasedAt),
			Previous:    previous,
			Consensus:   consensus,
			Actual:      actual,
		})
		if err != nil {
			return stats, fmt.Errorf("upsert calendar event: %w", err)
		}
		if record, err := storedEventFromRow(event); err == nil {
			stored = append(stored, record)
		}

		links, err := queries.UpsertCalendarEventEntity(ctx, calendardb.UpsertCalendarEventEntityParams{
			CalendarEventID: event.ID,
			EntityID:        entry.EntityID,
		})
		if err != nil {
			return stats, fmt.Errorf("upsert calendar event entity: %w", err)
		}
		stats.Stored++
		stats.LinksAdded += int(links)
	}

	if err := tx.Commit(ctx); err != nil {
		return stats, fmt.Errorf("commit persist transaction: %w", err)
	}
	for _, record := range stored {
		_ = repository.store.Set(ctx, cache.CalendarEventKey(record.ID), record)
	}
	return stats, nil
}

// CreateCalendarEvent stores an administrator-created event and its
// entity links in one transaction, then refreshes the cache entry the
// read path uses.
func (repository *EventRepository) CreateCalendarEvent(
	ctx context.Context,
	entry models.CreateEventEntry,
) (models.EventRecord, error) {
	previous, err := numericValue(entry.Previous)
	if err != nil {
		return models.EventRecord{}, err
	}
	consensus, err := numericValue(entry.Consensus)
	if err != nil {
		return models.EventRecord{}, err
	}
	actual, err := numericValue(entry.Actual)
	if err != nil {
		return models.EventRecord{}, err
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return models.EventRecord{}, fmt.Errorf("begin create calendar event: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repository.queries.WithTx(tx)

	event, err := queries.CreateCalendarEvent(ctx, calendardb.CreateCalendarEventParams{
		ID:          uuid.New(),
		SourceID:    entry.SourceID,
		IndicatorID: entry.IndicatorID,
		ScheduledAt: timestampValueUnchecked(entry.ScheduledAt),
		ReleasedAt:  timestampValue(entry.ReleasedAt),
		Previous:    previous,
		Consensus:   consensus,
		Actual:      actual,
	})
	if err != nil {
		return models.EventRecord{}, fmt.Errorf("create calendar event: %w", err)
	}
	for _, entityID := range entry.EntityIDs {
		if _, err := queries.UpsertCalendarEventEntity(ctx, calendardb.UpsertCalendarEventEntityParams{
			CalendarEventID: event.ID,
			EntityID:        entityID,
		}); err != nil {
			return models.EventRecord{}, fmt.Errorf("link calendar event entity: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return models.EventRecord{}, fmt.Errorf("commit create calendar event: %w", err)
	}

	record, err := eventRecordFromRow(event)
	if err != nil {
		return models.EventRecord{}, fmt.Errorf("read created calendar event: %w", err)
	}
	if stored, err := storedEventFromRow(event); err == nil {
		_ = repository.store.Set(ctx, cache.CalendarEventKey(stored.ID), stored)
	}
	return record, nil
}

func (repository *EventRepository) ListCalendarEventsPage(
	ctx context.Context,
	cursor *paging.Cursor,
	limit int32,
) ([]models.EventRecord, *paging.Cursor, error) {
	params := calendardb.ListCalendarEventsPageParams{PageSize: limit + 1}
	if cursor != nil {
		params.CursorCreatedAt = pgtype.Timestamptz{Time: cursor.At, Valid: true}
		params.CursorID = pgtype.UUID{Bytes: cursor.ID, Valid: true}
	}
	rows, err := repository.queries.ListCalendarEventsPage(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list calendar events page: %w", err)
	}
	events := make([]models.EventRecord, 0, min(len(rows), int(limit)))
	for index, row := range rows {
		if int32(index) == limit {
			last := events[len(events)-1]
			return events, &paging.Cursor{At: last.CreatedAt, ID: last.ID}, nil
		}
		record, err := eventRecordFromRow(row)
		if err != nil {
			return nil, nil, err
		}
		events = append(events, record)
	}
	return events, nil, nil
}

func (repository *EventRepository) CalendarEvent(ctx context.Context, id uuid.UUID) (models.EventContext, bool, error) {
	row, err := repository.queries.GetCalendarEvent(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.EventContext{}, false, nil
	}
	if err != nil {
		return models.EventContext{}, false, fmt.Errorf("get calendar event: %w", err)
	}
	scheduledAt := timeValue(row.ScheduledAt)
	if scheduledAt == nil {
		return models.EventContext{}, false, fmt.Errorf("calendar event %s has no scheduled time", row.ID)
	}
	previous, err := numericFloat(row.Previous)
	if err != nil {
		return models.EventContext{}, false, err
	}
	consensus, err := numericFloat(row.Consensus)
	if err != nil {
		return models.EventContext{}, false, err
	}
	actual, err := numericFloat(row.Actual)
	if err != nil {
		return models.EventContext{}, false, err
	}
	return models.EventContext{
		StoredEvent: models.StoredEvent{
			ID:          row.ID,
			SourceID:    row.SourceID,
			IndicatorID: row.IndicatorID,
			ScheduledAt: *scheduledAt,
			ReleasedAt:  timeValue(row.ReleasedAt),
			Previous:    previous,
			Consensus:   consensus,
			Actual:      actual,
		},
		IndicatorName: row.IndicatorName,
		IndicatorType: row.IndicatorType,
	}, true, nil
}

// storedEventFromRow flattens the driver-specific row into the plain
// shape the cache stores, so cached events decode without pgtype.
func storedEventFromRow(row calendardb.CalendarEvent) (models.StoredEvent, error) {
	scheduledAt := timeValue(row.ScheduledAt)
	if scheduledAt == nil {
		return models.StoredEvent{}, fmt.Errorf("calendar event %s has no scheduled time", row.ID)
	}
	previous, err := numericFloat(row.Previous)
	if err != nil {
		return models.StoredEvent{}, err
	}
	consensus, err := numericFloat(row.Consensus)
	if err != nil {
		return models.StoredEvent{}, err
	}
	actual, err := numericFloat(row.Actual)
	if err != nil {
		return models.StoredEvent{}, err
	}
	return models.StoredEvent{
		ID:          row.ID,
		SourceID:    row.SourceID,
		IndicatorID: row.IndicatorID,
		ScheduledAt: *scheduledAt,
		ReleasedAt:  timeValue(row.ReleasedAt),
		Previous:    previous,
		Consensus:   consensus,
		Actual:      actual,
	}, nil
}

func eventRecordFromRow(row calendardb.CalendarEvent) (models.EventRecord, error) {
	stored, err := storedEventFromRow(row)
	if err != nil {
		return models.EventRecord{}, err
	}
	return models.EventRecord{
		ID:          stored.ID,
		SourceID:    stored.SourceID,
		IndicatorID: stored.IndicatorID,
		ScheduledAt: stored.ScheduledAt,
		ReleasedAt:  stored.ReleasedAt,
		Previous:    stored.Previous,
		Consensus:   stored.Consensus,
		Actual:      stored.Actual,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}, nil
}

func timeValue(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func numericFloat(value pgtype.Numeric) (*float64, error) {
	if !value.Valid {
		return nil, nil
	}
	raw, err := value.Value()
	if err != nil {
		return nil, fmt.Errorf("decode numeric: %w", err)
	}
	var text string
	switch decoded := raw.(type) {
	case string:
		text = decoded
	case []byte:
		text = string(decoded)
	default:
		return nil, fmt.Errorf("decode numeric %T", raw)
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, fmt.Errorf("decode numeric %q: %w", text, err)
	}
	return &parsed, nil
}

// numericValue hands PostgreSQL the value as text, which the numeric
// codec parses back without a float round-trip through the driver.
func numericValue(value *float64) (pgtype.Numeric, error) {
	if value == nil {
		return pgtype.Numeric{}, nil
	}
	var numeric pgtype.Numeric
	text := strconv.FormatFloat(*value, 'f', -1, 64)
	if err := numeric.Scan(text); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("encode numeric %q: %w", text, err)
	}
	return numeric, nil
}

func timestampValue(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return timestampValueUnchecked(*value)
}

func timestampValueUnchecked(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}
