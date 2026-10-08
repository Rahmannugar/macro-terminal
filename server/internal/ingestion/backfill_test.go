package ingestion

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	marketmodels "github.com/Rahmannugar/macro-terminal/server/internal/market/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/google/uuid"
)

type fakeBackfillStore struct {
	mu       sync.Mutex
	gaps     []marketmodels.TimeWindow
	checks   []marketmodels.TimeWindow
	upserted [][]marketmodels.PersistCandle
	recorded []marketmodels.TimeWindow
}

func (store *fakeBackfillStore) UpsertCandles(
	_ context.Context,
	candles []marketmodels.PersistCandle,
) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.upserted = append(store.upserted, append([]marketmodels.PersistCandle(nil), candles...))
	return int64(len(candles)), nil
}

func (store *fakeBackfillStore) CandleGaps(
	context.Context,
	uuid.UUID,
	string,
	time.Time,
	time.Time,
	time.Duration,
) ([]marketmodels.TimeWindow, error) {
	return store.gaps, nil
}

func (store *fakeBackfillStore) BackfillChecks(
	context.Context,
	uuid.UUID,
	string,
	time.Time,
	time.Time,
) ([]marketmodels.TimeWindow, error) {
	return store.checks, nil
}

func (store *fakeBackfillStore) RecordBackfillCheck(
	_ context.Context,
	_ uuid.UUID,
	_ string,
	window marketmodels.TimeWindow,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.recorded = append(store.recorded, window)
	return nil
}

type fakeWindowFetcher struct {
	mu      sync.Mutex
	configs []models.SourceConfigurationWithSource
	bodies  [][]byte
	errs    []error
}

func (fetcher *fakeWindowFetcher) Fetch(
	_ context.Context,
	configuration models.SourceConfigurationWithSource,
) (Result, error) {
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	fetcher.configs = append(fetcher.configs, configuration)
	index := len(fetcher.configs) - 1
	if index < len(fetcher.errs) && fetcher.errs[index] != nil {
		return Result{}, fetcher.errs[index]
	}
	var body []byte
	if index < len(fetcher.bodies) {
		body = fetcher.bodies[index]
	}
	return Result{Body: body, Attempts: 1, StatusCode: 200}, nil
}

func (fetcher *fakeWindowFetcher) fetchCount() int {
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	return len(fetcher.configs)
}

func (fetcher *fakeWindowFetcher) requestURL(t *testing.T, index int) *url.URL {
	t.Helper()
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	if index >= len(fetcher.configs) {
		t.Fatalf("fetch %d not issued, issued %d", index, len(fetcher.configs))
	}
	var document map[string]any
	if err := json.Unmarshal(fetcher.configs[index].Config, &document); err != nil {
		t.Fatalf("decode windowed configuration: %v", err)
	}
	rawURL, _ := document["url"].(string)
	target, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse windowed url %q: %v", rawURL, err)
	}
	return target
}

func backfillJobFixture(
	configurations []models.SourceConfigurationWithSource,
	store *fakeBackfillStore,
	fetcher *fakeWindowFetcher,
	now time.Time,
) *BackfillJob {
	job := NewBackfillJob(
		&fakeConfigurationSource{configurations: configurations},
		&fakePairSource{pairs: []entitymodels.EntityPair{{
			ID:     uuid.New(),
			Symbol: "BTC/USDT",
		}}},
		store,
		fetcher,
		2,
		1,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	job.now = func() time.Time { return now }
	return job
}

func binanceConfiguration(id uuid.UUID) models.SourceConfigurationWithSource {
	config, _ := json.Marshal(map[string]any{
		"url": "https://data-api.binance.vision/api/v3/klines?symbol=BTCUSDT&interval=1m&limit=6",
		"candle": map[string]string{
			"provider":    "binance",
			"pair_symbol": "BTC/USDT",
			"timeframe":   "1min",
		},
	})
	return models.SourceConfigurationWithSource{
		SourceConfiguration: models.SourceConfiguration{
			ID:       id,
			SourceID: uuid.New(),
			Type:     "api",
			Config:   config,
		},
		SourceName: "Binance Vision",
		SourceType: "candles",
	}
}

func binanceWindowBody(openMillis int64) []byte {
	return []byte(`[[` + strconv.FormatInt(openMillis, 10) +
		`,"1.05","1.07","1.04","1.06","100",` + strconv.FormatInt(openMillis+3599999, 10) + `,"0","0","0","0","0"]]`)
}

func TestBackfillFetchesLargestGapInWindows(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	start := now.Add(-2000 * time.Minute)
	store := &fakeBackfillStore{gaps: []marketmodels.TimeWindow{{From: start, To: now}}}
	fetcher := &fakeWindowFetcher{bodies: [][]byte{
		binanceWindowBody(start.UnixMilli()),
		binanceWindowBody(start.Add(1000 * time.Minute).UnixMilli()),
	}}
	job := backfillJobFixture([]models.SourceConfigurationWithSource{binanceConfiguration(uuid.New())}, store, fetcher, now)

	job.backfill(t.Context())

	if fetcher.fetchCount() != 2 {
		t.Fatalf("fetches = %d, want 2 (two 1000-bar windows)", fetcher.fetchCount())
	}
	if len(store.upserted) != 2 {
		t.Fatalf("upsert batches = %d, want 2", len(store.upserted))
	}
	if len(store.upserted[0]) != 1 || len(store.upserted[1]) != 1 {
		t.Fatalf("upserted candles = %d and %d, want one bar per window",
			len(store.upserted[0]), len(store.upserted[1]))
	}
	first := fetcher.requestURL(t, 0)
	if got, want := first.Query().Get("startTime"), strconv.FormatInt(start.UnixMilli(), 10); got != want {
		t.Errorf("first startTime = %s, want %s", got, want)
	}
	if got := first.Query().Get("limit"); got != "1000" {
		t.Errorf("first limit = %s, want 1000", got)
	}
	second := fetcher.requestURL(t, 1)
	if got, want := second.Query().Get("startTime"),
		strconv.FormatInt(start.Add(1000*time.Minute).UnixMilli(), 10); got != want {
		t.Errorf("second startTime = %s, want %s", got, want)
	}
	if got, want := second.Query().Get("endTime"), strconv.FormatInt(now.UnixMilli(), 10); got != want {
		t.Errorf("endTime = %s, want %s (now)", got, want)
	}
	if len(store.recorded) != 0 {
		t.Fatalf("recorded checks = %d, want 0", len(store.recorded))
	}
}

func TestBackfillRecordsCheckForEmptyWindow(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	start := now.Add(-30 * time.Minute)
	store := &fakeBackfillStore{gaps: []marketmodels.TimeWindow{{From: start, To: now}}}
	fetcher := &fakeWindowFetcher{bodies: [][]byte{[]byte(`[]`)}}
	job := backfillJobFixture([]models.SourceConfigurationWithSource{binanceConfiguration(uuid.New())}, store, fetcher, now)

	job.backfill(t.Context())

	if fetcher.fetchCount() != 1 {
		t.Fatalf("fetches = %d, want 1", fetcher.fetchCount())
	}
	if len(store.upserted) != 0 {
		t.Fatalf("upsert batches = %d, want 0", len(store.upserted))
	}
	if len(store.recorded) != 1 {
		t.Fatalf("recorded checks = %d, want 1", len(store.recorded))
	}
	if !store.recorded[0].From.Equal(start) || !store.recorded[0].To.Equal(now) {
		t.Errorf("recorded check = %v, want [%s, %s)", store.recorded[0], start, now)
	}
}

func TestBackfillSkipsFullyCheckedGap(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	start := now.Add(-30 * time.Minute)
	store := &fakeBackfillStore{
		gaps:   []marketmodels.TimeWindow{{From: start, To: now}},
		checks: []marketmodels.TimeWindow{{From: start, To: now}},
	}
	fetcher := &fakeWindowFetcher{}
	job := backfillJobFixture([]models.SourceConfigurationWithSource{binanceConfiguration(uuid.New())}, store, fetcher, now)

	job.backfill(t.Context())

	if fetcher.fetchCount() != 0 {
		t.Fatalf("fetches = %d, want 0 (gap fully checked)", fetcher.fetchCount())
	}
}

func TestBackfillFetchErrorStopsConfiguration(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	start := now.Add(-2000 * time.Minute)
	store := &fakeBackfillStore{gaps: []marketmodels.TimeWindow{{From: start, To: now}}}
	fetcher := &fakeWindowFetcher{errs: []error{ErrMalformed}}
	job := backfillJobFixture([]models.SourceConfigurationWithSource{binanceConfiguration(uuid.New())}, store, fetcher, now)

	job.backfill(t.Context())

	if fetcher.fetchCount() != 1 {
		t.Fatalf("fetches = %d, want 1 (stop after failure)", fetcher.fetchCount())
	}
	if len(store.upserted) != 0 || len(store.recorded) != 0 {
		t.Fatalf("upserts = %d checks = %d, want none after a failed fetch",
			len(store.upserted), len(store.recorded))
	}
}

func TestBackfillIgnoresUnknownTimeframe(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	configuration := binanceConfiguration(uuid.New())
	var document map[string]any
	_ = json.Unmarshal(configuration.Config, &document)
	document["candle"].(map[string]any)["timeframe"] = "1hour"
	configuration.Config, _ = json.Marshal(document)

	store := &fakeBackfillStore{gaps: []marketmodels.TimeWindow{{From: now.Add(-time.Hour), To: now}}}
	fetcher := &fakeWindowFetcher{}
	job := backfillJobFixture([]models.SourceConfigurationWithSource{configuration}, store, fetcher, now)

	job.backfill(t.Context())

	if fetcher.fetchCount() != 0 {
		t.Fatalf("fetches = %d, want 0", fetcher.fetchCount())
	}
}

func TestWindowedConfigurationOandaRewritesToFromTo(t *testing.T) {
	configuration := models.SourceConfigurationWithSource{
		SourceConfiguration: models.SourceConfiguration{
			ID:       uuid.New(),
			SourceID: uuid.New(),
			Type:     "api",
			Config: []byte(`{
				"url": "https://api-fxpractice.oanda.com/v3/instruments/EUR_USD/candles?granularity=M1&count=6&price=M",
				"bearer_env": "MACRO_TERMINAL_OANDA_TOKEN",
				"candle": {"provider": "oanda", "pair_symbol": "EUR/USD", "timeframe": "1min"}
			}`),
		},
		SourceName: "OANDA Practice",
		SourceType: "candles",
	}
	from := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	to := from.Add(5000 * time.Minute)

	windowed, err := windowedConfiguration(configuration, "oanda", from, to)
	if err != nil {
		t.Fatalf("windowed configuration: %v", err)
	}
	if windowed.ID != configuration.ID {
		t.Errorf("windowed ID = %s, want the original %s", windowed.ID, configuration.ID)
	}
	var document map[string]any
	if err := json.Unmarshal(windowed.Config, &document); err != nil {
		t.Fatalf("decode windowed configuration: %v", err)
	}
	if _, present := document["bearer_env"]; !present {
		t.Error("bearer_env must survive the rewrite")
	}
	if _, present := document["candle"]; !present {
		t.Error("candle metadata must survive the rewrite")
	}
	target, err := url.Parse(document["url"].(string))
	if err != nil {
		t.Fatalf("parse windowed url: %v", err)
	}
	query := target.Query()
	if _, present := query["count"]; present {
		t.Error("count must be removed when from/to are set")
	}
	if got := query.Get("from"); got != "2026-10-08T00:00:00Z" {
		t.Errorf("from = %s, want RFC 3339", got)
	}
	if got := query.Get("to"); got != "2026-10-11T11:20:00Z" {
		t.Errorf("to = %s, want RFC 3339", got)
	}
	if query.Get("granularity") != "M1" || query.Get("price") != "M" {
		t.Errorf("granularity/price must be kept, got %v", query)
	}
}

func TestLargestUncoveredPicksWidestStretch(t *testing.T) {
	base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	gaps := []marketmodels.TimeWindow{
		{From: base, To: base.Add(2 * time.Hour)},
		{From: base.Add(4 * time.Hour), To: base.Add(4*time.Hour + 30*time.Minute)},
	}
	checks := []marketmodels.TimeWindow{{From: base.Add(30 * time.Minute), To: base.Add(time.Hour)}}

	best, ok := largestUncovered(gaps, checks)
	if !ok {
		t.Fatal("expected an uncovered window")
	}
	if !best.From.Equal(base.Add(time.Hour)) || !best.To.Equal(base.Add(2*time.Hour)) {
		t.Errorf("best = %v, want the widest uncovered stretch [1h, 2h)", best)
	}
}

func TestLargestUncoveredRejectsFullyCheckedGaps(t *testing.T) {
	base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	gaps := []marketmodels.TimeWindow{{From: base, To: base.Add(time.Hour)}}
	checks := []marketmodels.TimeWindow{{From: base, To: base.Add(time.Hour)}}

	if _, ok := largestUncovered(gaps, checks); ok {
		t.Fatal("a fully checked gap must yield no window")
	}
}

func TestSubtractCheckSplitsAroundCoverage(t *testing.T) {
	base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	segments := subtractCheck(
		[]marketmodels.TimeWindow{{From: base, To: base.Add(3 * time.Hour)}},
		marketmodels.TimeWindow{From: base.Add(time.Hour), To: base.Add(2 * time.Hour)},
	)
	if len(segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(segments))
	}
	if !segments[0].From.Equal(base) || !segments[0].To.Equal(base.Add(time.Hour)) {
		t.Errorf("first segment = %v, want [0h, 1h)", segments[0])
	}
	if !segments[1].From.Equal(base.Add(2*time.Hour)) || !segments[1].To.Equal(base.Add(3*time.Hour)) {
		t.Errorf("second segment = %v, want [2h, 3h)", segments[1])
	}
}
