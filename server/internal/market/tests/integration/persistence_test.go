//go:build integration

package integration_test

import (
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	entityrepositories "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	marketmodels "github.com/Rahmannugar/macro-terminal/server/internal/market/models"
	marketrepositories "github.com/Rahmannugar/macro-terminal/server/internal/market/repositories"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/google/uuid"
)

func TestUpsertCandlesIsIdempotentAndPagesDescending(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)

	source, err := sourcerepositories.NewSourceRepository(pool).UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Market Persistence Test Source", Type: "candles",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	entityRepository := entityrepositories.NewEntityRepository(pool)
	baseEntity, err := entityRepository.UpsertEntity(t.Context(), entitymodels.Entity{
		ID: testID(t), Code: "MKTEUR", Name: "Market Euro (test)", Type: "currency",
	})
	if err != nil {
		t.Fatalf("create base entity: %v", err)
	}
	quoteEntity, err := entityRepository.UpsertEntity(t.Context(), entitymodels.Entity{
		ID: testID(t), Code: "MKTUSD", Name: "Market Dollar (test)", Type: "currency",
	})
	if err != nil {
		t.Fatalf("create quote entity: %v", err)
	}
	pair, err := entityRepository.UpsertEntityPair(t.Context(), entitymodels.EntityPair{
		ID: testID(t), BaseEntityID: baseEntity.ID, QuoteEntityID: quoteEntity.ID, Symbol: "EUR/USD",
	})
	if err != nil {
		t.Fatalf("create pair: %v", err)
	}

	repository := marketrepositories.NewCandleRepository(pool)
	start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	window := []marketmodels.PersistCandle{
		candle(t, source.ID, pair.ID, start, 1.10),
		candle(t, source.ID, pair.ID, start.Add(time.Minute), 1.11),
		candle(t, source.ID, pair.ID, start.Add(2*time.Minute), 1.12),
	}
	upserted, err := repository.UpsertCandles(t.Context(), window)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if upserted != 3 {
		t.Fatalf("upserted = %d, want 3", upserted)
	}

	firstPage, next, err := repository.CandlesPage(t.Context(), pair.ID, "1min", nil, 2)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(firstPage) != 2 || next == nil {
		t.Fatalf("first page rows = %d next = %v, want 2 rows and a cursor", len(firstPage), next)
	}
	if !firstPage[0].Timestamp.Equal(start.Add(2 * time.Minute)) {
		t.Errorf("newest row = %s, want the latest bar", firstPage[0].Timestamp)
	}

	secondPage, next, err := repository.CandlesPage(t.Context(), pair.ID, "1min", next, 2)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(secondPage) != 1 || next != nil {
		t.Fatalf("second page rows = %d next = %v, want the last row and no cursor", len(secondPage), next)
	}
	if !secondPage[0].Timestamp.Equal(start) {
		t.Errorf("oldest row = %s, want the earliest bar", secondPage[0].Timestamp)
	}

	refreshed := window[0]
	refreshed.Close = 1.20
	if _, err := repository.UpsertCandles(t.Context(), []marketmodels.PersistCandle{refreshed}); err != nil {
		t.Fatalf("repeat upsert: %v", err)
	}
	page, _, err := repository.CandlesPage(t.Context(), pair.ID, "1min", nil, 100)
	if err != nil {
		t.Fatalf("page after refresh: %v", err)
	}
	if len(page) != 3 {
		t.Fatalf("rows after refresh = %d, want 3 (upsert must not duplicate)", len(page))
	}
	if page[2].Close != 1.20 {
		t.Errorf("refreshed close = %v, want 1.20", page[2].Close)
	}
}

func TestCandleGapsAndBackfillChecks(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)

	source, err := sourcerepositories.NewSourceRepository(pool).UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Market Gap Test Source", Type: "candles",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	entityRepository := entityrepositories.NewEntityRepository(pool)
	baseEntity, err := entityRepository.UpsertEntity(t.Context(), entitymodels.Entity{
		ID: testID(t), Code: "GAPBASE", Name: "Gap Base (test)", Type: "currency",
	})
	if err != nil {
		t.Fatalf("create base entity: %v", err)
	}
	quoteEntity, err := entityRepository.UpsertEntity(t.Context(), entitymodels.Entity{
		ID: testID(t), Code: "GAPQUOTE", Name: "Gap Quote (test)", Type: "currency",
	})
	if err != nil {
		t.Fatalf("create quote entity: %v", err)
	}
	pair, err := entityRepository.UpsertEntityPair(t.Context(), entitymodels.EntityPair{
		ID: testID(t), BaseEntityID: baseEntity.ID, QuoteEntityID: quoteEntity.ID, Symbol: "GAP/USD",
	})
	if err != nil {
		t.Fatalf("create pair: %v", err)
	}

	repository := marketrepositories.NewCandleRepository(pool)
	start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	window := []marketmodels.PersistCandle{
		candle(t, source.ID, pair.ID, start, 1.10),
		candle(t, source.ID, pair.ID, start.Add(time.Minute), 1.11),
		candle(t, source.ID, pair.ID, start.Add(10*time.Minute), 1.12),
		candle(t, source.ID, pair.ID, start.Add(11*time.Minute), 1.13),
	}
	if _, err := repository.UpsertCandles(t.Context(), window); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	rangeStart := start
	rangeEnd := start.Add(12 * time.Minute)
	gaps, err := repository.CandleGaps(t.Context(), pair.ID, "1min", rangeStart, rangeEnd, time.Minute)
	if err != nil {
		t.Fatalf("candle gaps: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("gaps = %d (%v), want 1 interior gap", len(gaps), gaps)
	}
	if !gaps[0].From.Equal(start.Add(time.Minute)) || !gaps[0].To.Equal(start.Add(10*time.Minute)) {
		t.Errorf("gap = %v, want [%s, %s)", gaps[0], start.Add(time.Minute), start.Add(10*time.Minute))
	}

	checkWindow := marketmodels.TimeWindow{From: start.Add(time.Minute), To: start.Add(10 * time.Minute)}
	if err := repository.RecordBackfillCheck(t.Context(), pair.ID, "1min", checkWindow); err != nil {
		t.Fatalf("record check: %v", err)
	}
	checks, err := repository.BackfillChecks(t.Context(), pair.ID, "1min", rangeStart, rangeEnd)
	if err != nil {
		t.Fatalf("backfill checks: %v", err)
	}
	if len(checks) != 1 {
		t.Fatalf("checks = %d (%v), want 1", len(checks), checks)
	}
	if !checks[0].From.Equal(checkWindow.From) || !checks[0].To.Equal(checkWindow.To) {
		t.Errorf("check = %v, want %v", checks[0], checkWindow)
	}

	emptyStart := start.Add(time.Hour)
	emptyEnd := start.Add(time.Hour + 30*time.Minute)
	gaps, err = repository.CandleGaps(t.Context(), pair.ID, "1min", emptyStart, emptyEnd, time.Minute)
	if err != nil {
		t.Fatalf("empty-range gaps: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("empty-range gaps = %d (%v), want 1 span", len(gaps), gaps)
	}
	if !gaps[0].From.Equal(emptyStart) || !gaps[0].To.Equal(emptyEnd) {
		t.Errorf("empty-range gap = %v, want the whole range", gaps[0])
	}
}

func candle(t *testing.T, sourceID, pairID uuid.UUID, timestamp time.Time, close float64) marketmodels.PersistCandle {
	return marketmodels.PersistCandle{
		ID:           testID(t),
		SourceID:     sourceID,
		EntityPairID: pairID,
		Timeframe:    "1min",
		Timestamp:    timestamp,
		Open:         close - 0.01,
		High:         close + 0.01,
		Low:          close - 0.02,
		Close:        close,
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
