//go:build integration

package integration_test

import (
	"testing"
	"time"

	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	calendarepositories "github.com/Rahmannugar/macro-terminal/server/internal/calendar/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	entityrepositories "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPersistCalendarEventsIsIdempotentAndRefreshesValues(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Calendar Persistence Test Source", Type: "calendar",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	entityRepository := entityrepositories.NewEntityRepository(pool)
	dollar, err := entityRepository.UpsertEntity(t.Context(), entitymodels.Entity{
		ID: testID(t), Code: "CALP", Name: "Calendar Dollar (test)", Type: "currency",
	})
	if err != nil {
		t.Fatalf("create entity: %v", err)
	}
	indicator, err := entityRepository.UpsertIndicator(t.Context(), entitymodels.Indicator{
		ID: testID(t), Name: "Test CPI", EntityID: dollar.ID, Type: "inflation",
	})
	if err != nil {
		t.Fatalf("create indicator: %v", err)
	}

	eventRepository := calendarepositories.NewEventRepository(pool)
	scheduled := time.Date(2026, 10, 2, 12, 30, 0, 0, time.UTC)
	released := time.Date(2026, 10, 2, 12, 31, 0, 0, time.UTC)
	previous, consensus, actual := 3.0, 2.9, 2.8
	first := calendarmodels.PersistEntry{
		SourceID: source.ID, IndicatorID: indicator.ID, EntityID: dollar.ID,
		ScheduledAt: scheduled, ReleasedAt: &released,
		Previous: &previous, Consensus: &consensus, Actual: &actual,
	}

	stats, err := eventRepository.PersistEvents(t.Context(), []calendarmodels.PersistEntry{first})
	if err != nil {
		t.Fatalf("persist first pass: %v", err)
	}
	if stats.Stored != 1 || stats.LinksAdded != 1 {
		t.Fatalf("first pass stats = %+v, want {Stored:1 LinksAdded:1}", stats)
	}
	assertCalendarCounts(t, pool, 1, 1)
	eventID := calendarEventID(t, pool, source.ID, indicator.ID, scheduled)

	// Repeated delivery of the same event changes no rows and adds no links.
	stats, err = eventRepository.PersistEvents(t.Context(), []calendarmodels.PersistEntry{first})
	if err != nil {
		t.Fatalf("persist repeat pass: %v", err)
	}
	if stats.Stored != 1 || stats.LinksAdded != 0 {
		t.Fatalf("repeat pass stats = %+v, want {Stored:1 LinksAdded:0}", stats)
	}
	assertCalendarCounts(t, pool, 1, 1)

	// A refresh overwrites the fields the provider sent and leaves the
	// rest alone: a missing actual keeps the stored one, and the first
	// release timestamp wins over later refreshes.
	refreshReleased := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	refreshedActual := 3.1
	refresh := calendarmodels.PersistEntry{
		SourceID: source.ID, IndicatorID: indicator.ID, EntityID: dollar.ID,
		ScheduledAt: scheduled, ReleasedAt: &refreshReleased, Actual: &refreshedActual,
	}
	if _, err := eventRepository.PersistEvents(
		t.Context(), []calendarmodels.PersistEntry{refresh},
	); err != nil {
		t.Fatalf("persist refresh pass: %v", err)
	}
	var storedPrevious, storedConsensus, storedActual *float64
	var storedReleased time.Time
	err = pool.QueryRow(t.Context(),
		`SELECT previous, consensus, actual, released_at
		 FROM calendar_events WHERE id = $1`, eventID,
	).Scan(&storedPrevious, &storedConsensus, &storedActual, &storedReleased)
	if err != nil {
		t.Fatalf("read stored event: %v", err)
	}
	if storedActual == nil || *storedActual != refreshedActual {
		t.Fatalf("actual = %v, want %v (latest non-null wins)", storedActual, refreshedActual)
	}
	if storedConsensus == nil || *storedConsensus != consensus {
		t.Fatalf("consensus = %v, want %v (a missing value must not erase it)", storedConsensus, consensus)
	}
	if storedPrevious == nil || *storedPrevious != previous {
		t.Fatalf("previous = %v, want %v (a missing value must not erase it)", storedPrevious, previous)
	}
	if !storedReleased.Equal(released) {
		t.Fatalf("released_at = %v, want %v (the first release timestamp wins)", storedReleased, released)
	}

	// The same event linked to a second entity grows the link table only.
	secondEntity, err := entityRepository.UpsertEntity(t.Context(), entitymodels.Entity{
		ID: testID(t), Code: "CALE", Name: "Calendar Euro (test)", Type: "currency",
	})
	if err != nil {
		t.Fatalf("create second entity: %v", err)
	}
	stats, err = eventRepository.PersistEvents(t.Context(), []calendarmodels.PersistEntry{{
		SourceID: source.ID, IndicatorID: indicator.ID, EntityID: secondEntity.ID,
		ScheduledAt: scheduled, Actual: &refreshedActual,
	}})
	if err != nil {
		t.Fatalf("persist second link: %v", err)
	}
	if stats.Stored != 1 || stats.LinksAdded != 1 {
		t.Fatalf("second link stats = %+v, want {Stored:1 LinksAdded:1}", stats)
	}
	assertCalendarCounts(t, pool, 1, 2)
	if secondID := calendarEventID(t, pool, source.ID, indicator.ID, scheduled); secondID != eventID {
		t.Fatalf("event ID changed after relinking: %v -> %v", eventID, secondID)
	}
}

func testID(t *testing.T) uuid.UUID {
	t.Helper()

	id, err := ids.New()
	if err != nil {
		t.Fatalf("generate ID: %v", err)
	}
	return id
}

func assertCalendarCounts(t *testing.T, pool *pgxpool.Pool, events, links int) {
	t.Helper()

	for _, check := range []struct {
		query string
		want  int
	}{
		{`SELECT count(*) FROM calendar_events`, events},
		{`SELECT count(*) FROM calendar_event_entities`, links},
	} {
		var got int
		if err := pool.QueryRow(t.Context(), check.query).Scan(&got); err != nil {
			t.Fatalf("count rows (%s): %v", check.query, err)
		}
		if got != check.want {
			t.Fatalf("count for %q = %d, want %d", check.query, got, check.want)
		}
	}
}

func calendarEventID(
	t *testing.T,
	pool *pgxpool.Pool,
	sourceID, indicatorID uuid.UUID,
	scheduled time.Time,
) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(t.Context(),
		`SELECT id FROM calendar_events
		 WHERE source_id = $1 AND indicator_id = $2 AND scheduled_at = $3`,
		sourceID, indicatorID, scheduled,
	).Scan(&id)
	if err != nil {
		t.Fatalf("read event ID: %v", err)
	}
	return id
}
