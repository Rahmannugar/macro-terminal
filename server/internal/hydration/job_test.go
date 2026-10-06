package hydration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/hydration/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/ingestion"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type failureRecord struct {
	id          uuid.UUID
	maxAttempts int32
	cause       string
}

type fakeRepository struct {
	claimed    []models.ClaimedJob
	claimCalls int
	targets    map[uuid.UUID]models.ContentTarget
	targetErr  error
	stored     map[uuid.UUID]string
	storeErr   error
	completed  []uuid.UUID
	failures   []failureRecord
	permanents []failureRecord
}

func (repository *fakeRepository) EnqueueMissingContentJobs(context.Context, int32) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) ReclaimStaleContentJobs(context.Context) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) ClaimBatch(context.Context, int32) ([]models.ClaimedJob, error) {
	repository.claimCalls++
	return repository.claimed, nil
}

func (repository *fakeRepository) Complete(_ context.Context, id uuid.UUID) error {
	repository.completed = append(repository.completed, id)
	return nil
}

func (repository *fakeRepository) Fail(_ context.Context, id uuid.UUID, maxAttempts int32, cause string) error {
	repository.failures = append(repository.failures, failureRecord{id: id, maxAttempts: maxAttempts, cause: cause})
	return nil
}

func (repository *fakeRepository) FailPermanently(_ context.Context, id uuid.UUID, cause string) error {
	repository.permanents = append(repository.permanents, failureRecord{id: id, cause: cause})
	return nil
}

func (repository *fakeRepository) ContentTarget(_ context.Context, id uuid.UUID) (models.ContentTarget, error) {
	if repository.targetErr != nil {
		return models.ContentTarget{}, repository.targetErr
	}
	target, ok := repository.targets[id]
	if !ok {
		return models.ContentTarget{}, fmt.Errorf("target %s: %w", id, pgx.ErrNoRows)
	}
	return target, nil
}

func (repository *fakeRepository) StoreContent(_ context.Context, id uuid.UUID, content string) (int64, error) {
	if repository.storeErr != nil {
		return 0, repository.storeErr
	}
	if repository.stored == nil {
		repository.stored = map[uuid.UUID]string{}
	}
	repository.stored[id] = content
	return 1, nil
}

type fakeFetcher struct {
	body []byte
	err  error
	seen []sourcemodels.SourceConfigurationWithSource
}

func (fetcher *fakeFetcher) Fetch(_ context.Context, configuration sourcemodels.SourceConfigurationWithSource) (ingestion.Result, error) {
	fetcher.seen = append(fetcher.seen, configuration)
	if fetcher.err != nil {
		return ingestion.Result{}, fetcher.err
	}
	return ingestion.Result{Body: fetcher.body}, nil
}

type blockedError struct{}

func (blockedError) Error() string   { return "request blocked" }
func (blockedError) Retryable() bool { return false }
func (blockedError) RetryAfter() time.Duration {
	return 0
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func configuredTarget(contentSelector string) models.ContentTarget {
	config := map[string]any{
		"url": "https://example.com/listing",
		"selectors": map[string]any{
			"item":  "li",
			"title": "a",
		},
	}
	if contentSelector != "" {
		config["selectors"].(map[string]any)["content"] = contentSelector
	}
	encoded, _ := json.Marshal(config)
	return models.ContentTarget{
		ArticleID:         uuid.New(),
		ArticleURL:        "https://example.com/article",
		SourceID:          uuid.New(),
		ConfigurationID:   uuid.New(),
		ConfigurationType: "web",
		Config:            encoded,
		SourceName:        "Test Source",
		SourceType:        "news",
	}
}

func TestCycleHydratesClaimedArticle(t *testing.T) {
	workID := uuid.New()
	target := configuredTarget(".body")
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, ArticleID: target.ArticleID, Attempts: 1}},
		targets: map[uuid.UUID]models.ContentTarget{target.ArticleID: target},
	}
	fetcher := &fakeFetcher{body: []byte(`<html><body><div class="body"><p>Full story.</p></div></body></html>`)}

	err := NewJob(repository, fetcher, testLogger()).cycle(t.Context())
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if got := repository.stored[target.ArticleID]; got != "<p>Full story.</p>" {
		t.Errorf("stored content = %q, want the selected body", got)
	}
	if len(repository.completed) != 1 || repository.completed[0] != workID {
		t.Errorf("completed = %v, want job %s", repository.completed, workID)
	}
	if len(repository.failures) != 0 || len(repository.permanents) != 0 {
		t.Errorf("failures = %d, permanents = %d, want none", len(repository.failures), len(repository.permanents))
	}

	request := fetcher.seen[0]
	if request.SourceName != "Test Source" || request.Type != "web" {
		t.Errorf("fetch configuration = %+v, want the source's web configuration", request)
	}
	var document map[string]any
	if err := json.Unmarshal(request.Config, &document); err != nil {
		t.Fatalf("fetch config JSON: %v", err)
	}
	if document["url"] != target.ArticleURL {
		t.Errorf("request url = %v, want the article URL", document["url"])
	}
}

func TestCyclePermanentlyFailsMissingArticle(t *testing.T) {
	workID := uuid.New()
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, ArticleID: uuid.New(), Attempts: 1}},
	}

	err := NewJob(repository, &fakeFetcher{}, testLogger()).cycle(t.Context())
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(repository.permanents) != 1 || repository.permanents[0].id != workID {
		t.Fatalf("permanents = %+v, want the claimed job", repository.permanents)
	}
	if len(repository.failures) != 0 {
		t.Errorf("failures = %+v, want none", repository.failures)
	}
}

func TestCyclePermanentlyFailsWithoutContentSelector(t *testing.T) {
	workID := uuid.New()
	target := configuredTarget("")
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, ArticleID: target.ArticleID, Attempts: 1}},
		targets: map[uuid.UUID]models.ContentTarget{target.ArticleID: target},
	}

	err := NewJob(repository, &fakeFetcher{body: []byte("<html></html>")}, testLogger()).cycle(t.Context())
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(repository.permanents) != 1 {
		t.Fatalf("permanents = %+v, want one", repository.permanents)
	}
}

func TestCyclePermanentlyFailsWhenSelectorMatchesNothing(t *testing.T) {
	workID := uuid.New()
	target := configuredTarget(".body")
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, ArticleID: target.ArticleID, Attempts: 1}},
		targets: map[uuid.UUID]models.ContentTarget{target.ArticleID: target},
	}

	err := NewJob(repository, &fakeFetcher{body: []byte(`<html><body><p>Markup changed.</p></body></html>`)}, testLogger()).cycle(t.Context())
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(repository.permanents) != 1 {
		t.Fatalf("permanents = %+v, want one", repository.permanents)
	}
	if len(repository.completed) != 0 {
		t.Errorf("completed = %v, want none", repository.completed)
	}
}

func TestCycleSendsFetchFailureBackWithBackoff(t *testing.T) {
	workID := uuid.New()
	target := configuredTarget(".body")
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, ArticleID: target.ArticleID, Attempts: 2}},
		targets: map[uuid.UUID]models.ContentTarget{target.ArticleID: target},
	}

	err := NewJob(repository, &fakeFetcher{err: errors.New("provider returned status 503")}, testLogger()).cycle(t.Context())
	if err == nil {
		t.Fatal("cycle error = nil, want the fetch failure joined with the backoff record")
	}
	if len(repository.failures) != 1 {
		t.Fatalf("failures = %+v, want one", repository.failures)
	}
	if repository.failures[0].maxAttempts != maxAttempts {
		t.Errorf("maxAttempts = %d, want %d", repository.failures[0].maxAttempts, maxAttempts)
	}
	if len(repository.permanents) != 0 {
		t.Errorf("permanents = %+v, want none", repository.permanents)
	}
}

func TestCycleFailsBlockedFetchPermanently(t *testing.T) {
	workID := uuid.New()
	target := configuredTarget(".body")
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, ArticleID: target.ArticleID, Attempts: 1}},
		targets: map[uuid.UUID]models.ContentTarget{target.ArticleID: target},
	}

	err := NewJob(repository, &fakeFetcher{err: blockedError{}}, testLogger()).cycle(t.Context())
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(repository.permanents) != 1 {
		t.Fatalf("permanents = %+v, want one", repository.permanents)
	}
	if len(repository.failures) != 0 {
		t.Errorf("failures = %+v, want none", repository.failures)
	}
}

func TestWakeNeverBlocksWhenACycleIsAlreadyPending(t *testing.T) {
	job := NewJob(&fakeRepository{}, &fakeFetcher{}, testLogger())
	job.Wake()
	job.Wake()
}

func TestRunStopsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := NewJob(&fakeRepository{}, &fakeFetcher{}, testLogger()).Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
}
