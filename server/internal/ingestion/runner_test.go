package ingestion

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	articlemodels "github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/mapping"
	"github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/google/uuid"
)

type fakeConfigurationSource struct {
	configurations []models.SourceConfigurationWithSource
	err            error
	markErr        error
	marked         [][]uuid.UUID
}

func (source *fakeConfigurationSource) ListSourceConfigurationsWithSource(
	context.Context,
) ([]models.SourceConfigurationWithSource, error) {
	if source.err != nil {
		return nil, source.err
	}
	return source.configurations, nil
}

func (source *fakeConfigurationSource) MarkSourceConfigurationsRun(
	_ context.Context,
	ids []uuid.UUID,
	runAt time.Time,
) error {
	if source.markErr != nil {
		return source.markErr
	}
	source.marked = append(source.marked, append([]uuid.UUID(nil), ids...))
	for i := range source.configurations {
		for _, id := range ids {
			if source.configurations[i].ID == id {
				timestamp := runAt
				source.configurations[i].LastRunAt = &timestamp
			}
		}
	}
	return nil
}

type fakeFetchResult struct {
	result Result
	err    error
}

type fakeSourceFetcher struct {
	mu    sync.Mutex
	calls []uuid.UUID
	byID  map[uuid.UUID]fakeFetchResult
}

func (fetcher *fakeSourceFetcher) Fetch(
	_ context.Context,
	configuration models.SourceConfigurationWithSource,
) (Result, error) {
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	fetcher.calls = append(fetcher.calls, configuration.ID)
	outcome, ok := fetcher.byID[configuration.ID]
	if !ok {
		return Result{Attempts: 1}, nil
	}
	return outcome.result, outcome.err
}

func (fetcher *fakeSourceFetcher) fetchedIDs() []uuid.UUID {
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	return append([]uuid.UUID(nil), fetcher.calls...)
}

func (fetcher *fakeSourceFetcher) fetchCount() int {
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	return len(fetcher.calls)
}

type fakeDictionaryLoader struct {
	dict  mapping.Dictionary
	err   error
	calls int
}

func (loader *fakeDictionaryLoader) Load(context.Context) (mapping.Dictionary, error) {
	loader.calls++
	return loader.dict, loader.err
}

type fakeArticleStore struct {
	mu      sync.Mutex
	err     error
	stats   articlemodels.PersistStats
	batches [][]articlemodels.PersistEntry
}

func (store *fakeArticleStore) PersistArticles(
	_ context.Context,
	entries []articlemodels.PersistEntry,
) (articlemodels.PersistStats, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.err != nil {
		return articlemodels.PersistStats{}, store.err
	}
	store.batches = append(store.batches, entries)
	return store.stats, nil
}

func (store *fakeArticleStore) stored() [][]articlemodels.PersistEntry {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.batches
}

func configuration(id uuid.UUID, name, sourceType, configType string) models.SourceConfigurationWithSource {
	return models.SourceConfigurationWithSource{
		SourceConfiguration: models.SourceConfiguration{
			ID:     id,
			Type:   configType,
			Config: []byte(`{"url":"https://example.com"}`),
		},
		SourceName: name,
		SourceType: sourceType,
	}
}

func newTestRunner(
	source *fakeConfigurationSource,
	fetcher *fakeSourceFetcher,
	loader *fakeDictionaryLoader,
) (*Runner, *time.Time) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	runner := NewRunner(
		source, fetcher, loader, &fakeArticleStore{}, discardLogger(), DefaultCadences(),
	)
	runner.now = func() time.Time { return now }
	return runner, &now
}

func testArticleStore(runner *Runner) *fakeArticleStore {
	return runner.articles.(*fakeArticleStore)
}

func TestRunnerFetchesEverythingOnFirstPass(t *testing.T) {
	news := configuration(uuid.New(), "CoinDesk", "news", "rss")
	official := configuration(uuid.New(), "BLS", "official", "api")
	centralBank := configuration(uuid.New(), "Federal Reserve", "official", "rss")

	source := &fakeConfigurationSource{
		configurations: []models.SourceConfigurationWithSource{news, official, centralBank},
	}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{}}
	runner, _ := newTestRunner(source, fetcher, &fakeDictionaryLoader{})

	runner.runDue(context.Background())

	if fetcher.fetchCount() != 3 {
		t.Fatalf("fetched = %d, want 3 (boot pass runs all due work)", fetcher.fetchCount())
	}
}

func TestRunnerRespectsCadences(t *testing.T) {
	news := configuration(uuid.New(), "CoinDesk", "news", "rss")
	official := configuration(uuid.New(), "BLS", "official", "api")
	centralBank := configuration(uuid.New(), "Federal Reserve", "official", "rss")

	source := &fakeConfigurationSource{
		configurations: []models.SourceConfigurationWithSource{news, official, centralBank},
	}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{}}
	runner, now := newTestRunner(source, fetcher, &fakeDictionaryLoader{})

	runner.runDue(context.Background())
	if fetcher.fetchCount() != 3 {
		t.Fatalf("first pass fetched = %d, want 3", fetcher.fetchCount())
	}

	runner.runDue(context.Background())
	if fetcher.fetchCount() != 3 {
		t.Fatalf("immediate second pass fetched again: %d, want 3", fetcher.fetchCount())
	}

	*now = now.Add(5 * time.Minute)
	runner.runDue(context.Background())
	if fetcher.fetchCount() != 4 {
		t.Fatalf("after 5m fetched = %d, want 4 (news only)", fetcher.fetchCount())
	}
	if last := fetcher.fetchedIDs()[3]; last != news.ID {
		t.Fatalf("due source after 5m = %v, want news %v", last, news.ID)
	}

	*now = now.Add(10 * time.Minute) // 15m total
	runner.runDue(context.Background())
	if fetcher.fetchCount() != 6 {
		t.Fatalf("after 15m fetched = %d, want 6 (news + central bank)", fetcher.fetchCount())
	}

	*now = now.Add(15 * time.Minute) // 30m total
	runner.runDue(context.Background())
	if fetcher.fetchCount() != 9 {
		t.Fatalf("after 30m fetched = %d, want 9 (all cadences elapsed)", fetcher.fetchCount())
	}
}

func TestRunnerContinuesAfterFetchFailure(t *testing.T) {
	failing := configuration(uuid.New(), "Broken API", "news", "api")
	working := configuration(uuid.New(), "CoinDesk", "news", "rss")

	source := &fakeConfigurationSource{
		configurations: []models.SourceConfigurationWithSource{failing, working},
	}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{
		failing.ID: {err: errors.New("provider exploded")},
	}}
	runner, _ := newTestRunner(source, fetcher, &fakeDictionaryLoader{})

	runner.runDue(context.Background())

	if fetcher.fetchCount() != 2 {
		t.Fatalf("fetched = %d, want 2 (one failure must not stop the others)", fetcher.fetchCount())
	}
}

func TestRunnerCadenceForCentralBankSource(t *testing.T) {
	cadences := DefaultCadences()
	if got := cadences.For("Federal Reserve", "official"); got != 15*time.Minute {
		t.Fatalf("central bank cadence = %v, want 15m", got)
	}
	if got := cadences.For("BLS", "official"); got != 30*time.Minute {
		t.Fatalf("official cadence = %v, want 30m", got)
	}
	if got := cadences.For("CoinDesk", "news"); got != 5*time.Minute {
		t.Fatalf("news cadence = %v, want 5m", got)
	}
	if got := cadences.For("FinanceCalendar", "calendar"); got != 30*time.Minute {
		t.Fatalf("calendar cadence = %v, want 30m", got)
	}
	if got := cadences.For("Mystery", "unknown"); got != cadences.Default {
		t.Fatalf("unknown type cadence = %v, want default", got)
	}
}

func TestRunnerLogsLoadFailureWithoutCrashing(t *testing.T) {
	source := &fakeConfigurationSource{err: errors.New("database down")}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{}}
	runner, _ := newTestRunner(source, fetcher, &fakeDictionaryLoader{})

	runner.runDue(context.Background()) // must not panic or hang

	if fetcher.fetchCount() != 0 {
		t.Fatalf("fetched = %d, want 0", fetcher.fetchCount())
	}
}

func TestRunnerLoadsMappingDictionaryOncePerPass(t *testing.T) {
	news := configuration(uuid.New(), "CoinDesk", "news", "rss")
	official := configuration(uuid.New(), "BLS", "official", "api")
	source := &fakeConfigurationSource{
		configurations: []models.SourceConfigurationWithSource{news, official},
	}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{}}
	loader := &fakeDictionaryLoader{}
	runner, _ := newTestRunner(source, fetcher, loader)

	runner.runDue(context.Background())
	runner.runDue(context.Background())

	if loader.calls != 1 {
		t.Fatalf("dictionary loads = %d, want 1 (one load per scheduling pass)", loader.calls)
	}
}

func TestRunnerFetchesWhenDictionaryLoadFails(t *testing.T) {
	news := configuration(uuid.New(), "CoinDesk", "news", "rss")
	source := &fakeConfigurationSource{
		configurations: []models.SourceConfigurationWithSource{news},
	}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{}}
	loader := &fakeDictionaryLoader{err: errors.New("database down")}
	runner, _ := newTestRunner(source, fetcher, loader)

	runner.runDue(context.Background())

	if fetcher.fetchCount() != 1 {
		t.Fatalf("fetched = %d, want 1 (mapping failure must not block fetching)", fetcher.fetchCount())
	}
}

func TestRunnerDurableStateSurvivesRestart(t *testing.T) {
	news := configuration(uuid.New(), "CoinDesk", "news", "rss")
	source := &fakeConfigurationSource{
		configurations: []models.SourceConfigurationWithSource{news},
	}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{}}

	firstRunner, _ := newTestRunner(source, fetcher, &fakeDictionaryLoader{})
	firstRunner.runDue(context.Background())
	if fetcher.fetchCount() != 1 {
		t.Fatalf("first runner fetched = %d, want 1", fetcher.fetchCount())
	}

	secondRunner, _ := newTestRunner(source, fetcher, &fakeDictionaryLoader{})
	secondRunner.runDue(context.Background())
	if fetcher.fetchCount() != 1 {
		t.Fatalf("after restart fetched = %d, want 1 (schedule state lives in the database)", fetcher.fetchCount())
	}
}

func TestRunnerSkipsRecentlyRunConfigurations(t *testing.T) {
	news := configuration(uuid.New(), "CoinDesk", "news", "rss")
	recent := time.Date(2026, 9, 30, 11, 58, 0, 0, time.UTC)
	news.LastRunAt = &recent

	source := &fakeConfigurationSource{
		configurations: []models.SourceConfigurationWithSource{news},
	}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{}}
	runner, _ := newTestRunner(source, fetcher, &fakeDictionaryLoader{})

	runner.runDue(context.Background())

	if fetcher.fetchCount() != 0 {
		t.Fatalf("fetched = %d, want 0 (2 minutes since last run is inside the 5m news cadence)", fetcher.fetchCount())
	}
}

func TestRunnerAbortsPassWhenScheduleRecordingFails(t *testing.T) {
	news := configuration(uuid.New(), "CoinDesk", "news", "rss")
	source := &fakeConfigurationSource{
		configurations: []models.SourceConfigurationWithSource{news},
		markErr:        errors.New("database down"),
	}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{}}
	runner, _ := newTestRunner(source, fetcher, &fakeDictionaryLoader{})

	runner.runDue(context.Background())

	if fetcher.fetchCount() != 0 {
		t.Fatalf("fetched = %d, want 0 (unrecorded dispatch must not fetch, or a crash would refetch the world)", fetcher.fetchCount())
	}
}

func TestRunnerWakeCoalesces(t *testing.T) {
	source := &fakeConfigurationSource{}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{}}
	runner, _ := newTestRunner(source, fetcher, &fakeDictionaryLoader{})

	runner.Wake()
	runner.Wake()

	select {
	case <-runner.wake:
	default:
		t.Fatal("expected one buffered wake after Wake calls")
	}
	select {
	case <-runner.wake:
		t.Fatal("wakes must coalesce into a single buffered slot")
	default:
	}
}

func TestRunnerStoresMappingOutcomes(t *testing.T) {
	entityID := uuid.New()
	news := configuration(uuid.New(), "CoinDesk", "news", "rss")
	source := &fakeConfigurationSource{
		configurations: []models.SourceConfigurationWithSource{news},
	}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{
		news.ID: {result: Result{
			BaseURL: "https://example.com",
			Items: []Item{
				{Title: "Federal Reserve holds rates", URL: "https://example.com/fed"},
				{Title: "Sunny weather reported", URL: "https://example.com/weather"},
			},
		}},
	}}
	loader := &fakeDictionaryLoader{dict: mapping.Build(
		[]entitymodels.Entity{{
			ID: entityID, Code: "FED", Name: "Federal Reserve", Type: "institution",
		}},
		nil,
		nil,
	)}
	runner, _ := newTestRunner(source, fetcher, loader)

	runner.runDue(context.Background())

	batches := testArticleStore(runner).stored()
	if len(batches) != 1 {
		t.Fatalf("persist batches = %d, want 1", len(batches))
	}
	entries := batches[0]
	if len(entries) != 2 {
		t.Fatalf("stored entries = %d, want 2", len(entries))
	}
	if len(entries[0].EntityIDs) != 1 || entries[0].EntityIDs[0] != entityID {
		t.Fatalf("mapped entry entities = %v, want [%v]", entries[0].EntityIDs, entityID)
	}
	if entries[0].QueueUnmapped {
		t.Fatal("mapped entry must not be queued as unmapped")
	}
	if !entries[1].QueueUnmapped {
		t.Fatal("unmatched entry must be queued as unmapped")
	}
	if len(entries[1].EntityIDs) != 0 {
		t.Fatalf("unmatched entry entities = %v, want none", entries[1].EntityIDs)
	}
}

func TestRunnerStoresCandidatesWhenDictionaryUnavailable(t *testing.T) {
	news := configuration(uuid.New(), "CoinDesk", "news", "rss")
	source := &fakeConfigurationSource{
		configurations: []models.SourceConfigurationWithSource{news},
	}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{
		news.ID: {result: Result{
			BaseURL: "https://example.com",
			Items: []Item{
				{Title: "Federal Reserve holds rates", URL: "https://example.com/fed"},
			},
		}},
	}}
	loader := &fakeDictionaryLoader{err: errors.New("database down")}
	runner, _ := newTestRunner(source, fetcher, loader)

	runner.runDue(context.Background())

	batches := testArticleStore(runner).stored()
	if len(batches) != 1 {
		t.Fatalf("persist batches = %d, want 1 (a vocabulary outage still stores fetched articles)", len(batches))
	}
	entry := batches[0][0]
	if entry.QueueUnmapped || len(entry.EntityIDs) != 0 {
		t.Fatalf(
			"unclassified entry mapped=%v entities=%v, want no classification without a dictionary",
			!entry.QueueUnmapped, entry.EntityIDs,
		)
	}
}

func TestRunnerContinuesWhenPersistFails(t *testing.T) {
	failing := configuration(uuid.New(), "Broken API", "news", "api")
	working := configuration(uuid.New(), "CoinDesk", "news", "rss")
	source := &fakeConfigurationSource{
		configurations: []models.SourceConfigurationWithSource{failing, working},
	}
	fetcher := &fakeSourceFetcher{byID: map[uuid.UUID]fakeFetchResult{
		failing.ID: {result: Result{
			BaseURL: "https://example.com",
			Items:   []Item{{Title: "Stored nowhere", URL: "https://example.com/a"}},
		}},
		working.ID: {result: Result{
			BaseURL: "https://example.com",
			Items:   []Item{{Title: "Still fetched", URL: "https://example.com/b"}},
		}},
	}}
	runner, _ := newTestRunner(source, fetcher, &fakeDictionaryLoader{})
	testArticleStore(runner).err = errors.New("database down")

	runner.runDue(context.Background())

	if fetcher.fetchCount() != 2 {
		t.Fatalf("fetched = %d, want 2 (persist failure must not stop the pass)", fetcher.fetchCount())
	}
	if batches := testArticleStore(runner).stored(); len(batches) != 0 {
		t.Fatalf("stored batches = %d, want 0", len(batches))
	}
}
