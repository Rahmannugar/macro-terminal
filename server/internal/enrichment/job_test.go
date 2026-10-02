package enrichment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
	"github.com/Rahmannugar/macro-terminal/server/internal/enrichment/models"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type failureRecord struct {
	id          uuid.UUID
	maxAttempts int32
	cause       string
}

type fakeRepository struct {
	claimed      []models.ClaimedJob
	claimCalls   int
	articles     map[uuid.UUID]models.Article
	articleErr   error
	stored       bool
	storedResult json.RawMessage
	storedModel  string
	storedCalls  int
	completed    []uuid.UUID
	failures     []failureRecord
	permanents   []failureRecord
}

func (repository *fakeRepository) EnqueueMissingArticles(context.Context, int32) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) ReclaimStaleEnrichmentJobs(context.Context) (int64, error) {
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

func (repository *fakeRepository) Article(_ context.Context, id uuid.UUID) (models.Article, error) {
	if repository.articleErr != nil {
		return models.Article{}, repository.articleErr
	}
	article, ok := repository.articles[id]
	if !ok {
		return models.Article{}, fmt.Errorf("article %s: %w", id, pgx.ErrNoRows)
	}
	return article, nil
}

func (repository *fakeRepository) StoreEnrichment(
	_ context.Context,
	articleID uuid.UUID,
	model string,
	result json.RawMessage,
) (bool, error) {
	repository.storedCalls++
	repository.storedResult = result
	repository.storedModel = model
	return repository.stored, nil
}

type fakeEntities struct {
	entities []entitymodels.Entity
	err      error
}

func (lister *fakeEntities) ListEntities(context.Context) ([]entitymodels.Entity, error) {
	return lister.entities, lister.err
}

type fakeEnricher struct {
	result ai.Enrichment
	err    error
	calls  int
	last   ai.EnrichInput
}

func (enricher *fakeEnricher) Enrich(_ context.Context, input ai.EnrichInput) (ai.Enrichment, error) {
	enricher.calls++
	enricher.last = input
	return enricher.result, enricher.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func newTestJob(repository *fakeRepository, enricher *fakeEnricher) *Job {
	return NewJob(
		repository,
		&fakeEntities{entities: []entitymodels.Entity{{Code: "USD"}, {Code: "EUR"}}},
		enricher,
		"gemini-3.1-flash-lite",
		true,
		testLogger(),
	)
}

func TestCycleEnrichesClaimedArticle(t *testing.T) {
	workID := uuid.New()
	articleID := uuid.New()
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, ArticleID: articleID, Attempts: 1}},
		articles: map[uuid.UUID]models.Article{
			articleID: {ID: articleID, Title: "Fed holds rates", Content: "The Federal Reserve kept rates unchanged."},
		},
		stored: true,
	}
	enricher := &fakeEnricher{result: ai.Enrichment{
		Entities: []string{"usd", "XYZ"},
		Topics:   []string{"monetary_policy"},
		Concepts: []string{"rate decision"},
	}}

	if err := newTestJob(repository, enricher).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if enricher.calls != 1 {
		t.Fatalf("enrich calls = %d, want 1", enricher.calls)
	}
	if len(enricher.last.EntityCodes) != 2 || enricher.last.EntityCodes[0] != "USD" {
		t.Errorf("prompt entity codes = %v, want the known universe", enricher.last.EntityCodes)
	}
	var stored ai.Enrichment
	if err := json.Unmarshal(repository.storedResult, &stored); err != nil {
		t.Fatalf("decode stored result: %v", err)
	}
	if len(stored.Entities) != 1 || stored.Entities[0] != "USD" {
		t.Errorf("stored entities = %v, want only the known USD code", stored.Entities)
	}
	if repository.storedModel != "gemini-3.1-flash-lite" {
		t.Errorf("stored model = %q, want the configured model", repository.storedModel)
	}
	if len(repository.completed) != 1 || repository.completed[0] != workID {
		t.Errorf("completed = %v, want [%v]", repository.completed, workID)
	}
	if len(repository.failures) != 0 || len(repository.permanents) != 0 {
		t.Errorf("failures = %v permanents = %v, want none", repository.failures, repository.permanents)
	}
}

func TestCycleSendsFailedClaimBackWithBackoff(t *testing.T) {
	workID := uuid.New()
	articleID := uuid.New()
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, ArticleID: articleID, Attempts: 3}},
		articles: map[uuid.UUID]models.Article{
			articleID: {ID: articleID, Title: "title", Content: "content"},
		},
	}
	enricher := &fakeEnricher{err: errors.New("quota exceeded")}

	if err := newTestJob(repository, enricher).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if len(repository.failures) != 1 {
		t.Fatalf("failures = %v, want one backoff failure", repository.failures)
	}
	failure := repository.failures[0]
	if failure.id != workID || failure.maxAttempts != maxAttempts {
		t.Errorf("failure = %+v, want the claimed row and the attempt budget", failure)
	}
	if failure.cause != "quota exceeded" {
		t.Errorf("cause = %q, want the provider error", failure.cause)
	}
	if repository.storedCalls != 0 || len(repository.completed) != 0 || len(repository.permanents) != 0 {
		t.Errorf("stored = %d completed = %v permanents = %v, want none",
			repository.storedCalls, repository.completed, repository.permanents)
	}
}

func TestCyclePermanentlyFailsMissingArticleWithoutCallingProvider(t *testing.T) {
	workID := uuid.New()
	articleID := uuid.New()
	repository := &fakeRepository{
		claimed:    []models.ClaimedJob{{ID: workID, ArticleID: articleID, Attempts: 1}},
		articleErr: fmt.Errorf("article %s: %w", articleID, pgx.ErrNoRows),
	}
	enricher := &fakeEnricher{}

	if err := newTestJob(repository, enricher).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if enricher.calls != 0 {
		t.Errorf("enrich calls = %d, want 0", enricher.calls)
	}
	if len(repository.permanents) != 1 || repository.permanents[0].id != workID {
		t.Errorf("permanents = %v, want the claimed row", repository.permanents)
	}
	if len(repository.failures) != 0 {
		t.Errorf("failures = %v, want none", repository.failures)
	}
}

func TestCycleRetriesTransientArticleErrors(t *testing.T) {
	workID := uuid.New()
	articleID := uuid.New()
	repository := &fakeRepository{
		claimed:    []models.ClaimedJob{{ID: workID, ArticleID: articleID, Attempts: 1}},
		articleErr: errors.New("connection reset"),
	}

	if err := newTestJob(repository, &fakeEnricher{}).cycle(t.Context()); err == nil {
		t.Fatal("cycle succeeded, want the article load error reported")
	}

	if len(repository.failures) != 1 || repository.failures[0].id != workID ||
		repository.failures[0].maxAttempts != maxAttempts {
		t.Errorf("failures = %v, want the claimed row sent back with the attempt budget",
			repository.failures)
	}
	if len(repository.permanents) != 0 {
		t.Errorf("permanents = %v, want none for a transient error", repository.permanents)
	}
}

func TestCycleCompletesWhenEnrichmentAlreadyStored(t *testing.T) {
	workID := uuid.New()
	articleID := uuid.New()
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, ArticleID: articleID, Attempts: 1}},
		articles: map[uuid.UUID]models.Article{
			articleID: {ID: articleID, Title: "title", Content: "content"},
		},
		stored: false,
	}

	if err := newTestJob(repository, &fakeEnricher{result: ai.Enrichment{}}).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if len(repository.completed) != 1 || repository.completed[0] != workID {
		t.Errorf("completed = %v, want the claimed row marked done", repository.completed)
	}
	if len(repository.failures) != 0 {
		t.Errorf("failures = %v, want none", repository.failures)
	}
}

func TestRunDisabledNeverClaimsWork(t *testing.T) {
	repository := &fakeRepository{claimed: []models.ClaimedJob{{ID: uuid.New(), ArticleID: uuid.New()}}}
	var logs bytes.Buffer
	job := NewJob(
		repository,
		&fakeEntities{},
		&fakeEnricher{},
		"gemini-3.1-flash-lite",
		false,
		slog.New(slog.NewTextHandler(&logs, nil)),
	)

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- job.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
	if repository.claimCalls != 0 {
		t.Errorf("claim calls = %d, want 0", repository.claimCalls)
	}
	if !bytes.Contains(logs.Bytes(), []byte("missing_secret")) {
		t.Errorf("logs = %s, want the missing_secret skip", logs.String())
	}
}

func TestWakeNeverBlocksWhenACycleIsAlreadyPending(t *testing.T) {
	job := NewJob(&fakeRepository{}, &fakeEntities{}, &fakeEnricher{}, "model", true, testLogger())
	job.Wake()
	job.Wake()
	if len(job.wake) != 1 {
		t.Fatalf("wake buffer = %d, want a single absorbed signal", len(job.wake))
	}
}
