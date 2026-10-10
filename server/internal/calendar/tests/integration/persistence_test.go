//go:build integration

package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	calendarmodels "github.com/Rahmannugar/macro-terminal/server/internal/calendar/models"
	calendarepositories "github.com/Rahmannugar/macro-terminal/server/internal/calendar/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	entityrepositories "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
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

	eventRepository := calendarepositories.NewEventRepository(pool, nil)
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
	storedRevision := calendarRevision(t, pool, eventID)
	if storedRevision != 1 {
		t.Fatalf("revision = %d, want 1 (the actual changed after release)", storedRevision)
	}

	// Repeating the refreshed values is not another revision.
	if _, err := eventRepository.PersistEvents(
		t.Context(), []calendarmodels.PersistEntry{refresh},
	); err != nil {
		t.Fatalf("persist unchanged pass: %v", err)
	}
	if storedRevision = calendarRevision(t, pool, eventID); storedRevision != 1 {
		t.Fatalf("revision = %d, want 1 (unchanged values must not bump)", storedRevision)
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

func calendarRevision(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID) int32 {
	t.Helper()

	var revision int32
	if err := pool.QueryRow(t.Context(),
		`SELECT revision FROM calendar_events WHERE id = $1`, eventID,
	).Scan(&revision); err != nil {
		t.Fatalf("read revision: %v", err)
	}
	return revision
}

func TestEventCacheWriteThrough(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Calendar Cache Test Source", Type: "calendar",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	entityRepository := entityrepositories.NewEntityRepository(pool)
	dollar, err := entityRepository.UpsertEntity(t.Context(), entitymodels.Entity{
		ID: testID(t), Code: "CACE", Name: "Calendar Cache Dollar (test)", Type: "currency",
	})
	if err != nil {
		t.Fatalf("create entity: %v", err)
	}
	indicator, err := entityRepository.UpsertIndicator(t.Context(), entitymodels.Indicator{
		ID: testID(t), Name: "Test Cached CPI", EntityID: dollar.ID, Type: "inflation",
	})
	if err != nil {
		t.Fatalf("create indicator: %v", err)
	}

	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(server.Close)
	store := cache.NewJSONStore(redis.NewClient(&redis.Options{Addr: server.Addr()}))
	eventRepository := calendarepositories.NewEventRepository(pool, store)

	scheduled := time.Date(2026, 10, 3, 12, 30, 0, 0, time.UTC)
	released := time.Date(2026, 10, 3, 12, 31, 0, 0, time.UTC)
	previous, consensus, actual := 3.1, 3.0, 2.9
	if _, err := eventRepository.PersistEvents(t.Context(), []calendarmodels.PersistEntry{{
		SourceID: source.ID, IndicatorID: indicator.ID, EntityID: dollar.ID,
		ScheduledAt: scheduled, ReleasedAt: &released,
		Previous: &previous, Consensus: &consensus, Actual: &actual,
	}}); err != nil {
		t.Fatalf("persist event: %v", err)
	}

	id := calendarEventID(t, pool, source.ID, indicator.ID, scheduled)
	key := cache.CalendarEventKey(id)
	raw, err := server.Get(key)
	if err != nil {
		t.Fatalf("read cached event: %v", err)
	}
	var cached calendarmodels.StoredEvent
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		t.Fatalf("decode cached event %q: %v", raw, err)
	}
	if cached.ID != id || !cached.ScheduledAt.Equal(scheduled) {
		t.Fatalf("cached event = %+v, want the stored row", cached)
	}
	if cached.Previous == nil || *cached.Previous != 3.1 {
		t.Fatalf("previous = %v, want 3.1", cached.Previous)
	}
	if cached.Actual == nil || *cached.Actual != 2.9 {
		t.Fatalf("actual = %v, want 2.9", cached.Actual)
	}
	if cached.ReleasedAt == nil || !cached.ReleasedAt.Equal(released) {
		t.Fatalf("releasedAt = %v, want %s", cached.ReleasedAt, released)
	}
}
