package repositories

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	calendardb "github.com/Rahmannugar/macro-terminal/server/internal/calendar/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventRepository struct {
	pool    *pgxpool.Pool
	queries *calendardb.Queries
}

func NewEventRepository(pool *pgxpool.Pool) *EventRepository {
	return &EventRepository{pool: pool, queries: calendardb.New(pool)}
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
	return stats, nil
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
