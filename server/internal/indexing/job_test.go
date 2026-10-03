package indexing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/indexing/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/vector"
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
	batches    [][]models.ClaimedJob
	claimCalls int
	articles   map[uuid.UUID]models.Article
	articleErr error
	completed  []uuid.UUID
	failures   []failureRecord
	permanents []failureRecord
}

func (repository *fakeRepository) EnqueueMissingIndexJobs(context.Context, int32) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) ReclaimStaleIndexJobs(context.Context) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) ClaimBatch(context.Context, int32) ([]models.ClaimedJob, error) {
	repository.claimCalls++
	if len(repository.batches) > 0 {
		batch := repository.batches[0]
		repository.batches = repository.batches[1:]
		return batch, nil
	}
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

type fakeIndexer struct {
	err   error
	calls int
	last  vector.Article
}

func (indexer *fakeIndexer) StoreArticle(_ context.Context, article vector.Article) error {
	indexer.calls++
	indexer.last = article
	return indexer.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func newTestJob(repository *fakeRepository, indexer *fakeIndexer) *Job {
	return NewJob(repository, indexer, true, testLogger())
}

func TestCycleIndexesClaimedArticle(t *testing.T) {
	workID := uuid.New()
	articleID := uuid.New()
	publishedAt := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, ArticleID: articleID, Attempts: 1}},
		articles: map[uuid.UUID]models.Article{
			articleID: {
				ID:          articleID,
				Title:       "Fed holds rates",
				Content:     "The Federal Reserve kept rates unchanged.",
				PublishedAt: &publishedAt,
			},
		},
	}
	indexer := &fakeIndexer{}

	if err := newTestJob(repository, indexer).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if indexer.calls != 1 {
		t.Fatalf("store calls = %d, want 1", indexer.calls)
	}
	if indexer.last.ID != articleID || indexer.last.Title != "Fed holds rates" ||
		indexer.last.Content != "The Federal Reserve kept rates unchanged." ||
		!indexer.last.PublishedAt.Equal(publishedAt) {
		t.Errorf("stored article = %+v, want the loaded article", indexer.last)
	}
	if len(repository.completed) != 1 || repository.completed[0] != workID {
		t.Errorf("completed = %v, want [%v]", repository.completed, workID)
	}
	if len(repository.failures) != 0 || len(repository.permanents) != 0 {
		t.Errorf("failures = %v permanents = %v, want none", repository.failures, repository.permanents)
	}
}

func TestCycleTreatsMissingPublishDateAsUnknown(t *testing.T) {
	articleID := uuid.New()
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: uuid.New(), ArticleID: articleID, Attempts: 1}},
		articles: map[uuid.UUID]models.Article{
			articleID: {ID: articleID, Title: "title", Content: "content"},
		},
	}
	indexer := &fakeIndexer{}

	if err := newTestJob(repository, indexer).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if !indexer.last.PublishedAt.IsZero() {
		t.Errorf("published at = %v, want the zero time for a missing date", indexer.last.PublishedAt)
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
	indexer := &fakeIndexer{err: errors.New("indexer unavailable")}

	if err := newTestJob(repository, indexer).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if len(repository.failures) != 1 {
		t.Fatalf("failures = %v, want one backoff failure", repository.failures)
	}
	failure := repository.failures[0]
	if failure.id != workID || failure.maxAttempts != maxAttempts {
		t.Errorf("failure = %+v, want the claimed row and the attempt budget", failure)
	}
	if failure.cause != "indexer unavailable" {
		t.Errorf("cause = %q, want the indexer error", failure.cause)
	}
	if len(repository.completed) != 0 || len(repository.permanents) != 0 {
		t.Errorf("completed = %v permanents = %v, want none", repository.completed, repository.permanents)
	}
}

func TestCyclePermanentlyFailsMissingArticleWithoutCallingIndexer(t *testing.T) {
	workID := uuid.New()
	articleID := uuid.New()
	repository := &fakeRepository{
		claimed:    []models.ClaimedJob{{ID: workID, ArticleID: articleID, Attempts: 1}},
		articleErr: fmt.Errorf("article %s: %w", articleID, pgx.ErrNoRows),
	}
	indexer := &fakeIndexer{}

	if err := newTestJob(repository, indexer).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if indexer.calls != 0 {
		t.Errorf("store calls = %d, want 0", indexer.calls)
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

	if err := newTestJob(repository, &fakeIndexer{}).cycle(t.Context()); err == nil {
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

func TestCycleDrainsBackToBackBatches(t *testing.T) {
	articles := map[uuid.UUID]models.Article{}
	fill := func(n int) []models.ClaimedJob {
		batch := make([]models.ClaimedJob, 0, n)
		for i := 0; i < n; i++ {
			articleID := uuid.New()
			batch = append(batch, models.ClaimedJob{ID: uuid.New(), ArticleID: articleID, Attempts: 1})
			articles[articleID] = models.Article{ID: articleID, Title: "title", Content: "content"}
		}
		return batch
	}
	repository := &fakeRepository{
		batches:  [][]models.ClaimedJob{fill(batchSize), fill(2)},
		articles: articles,
	}
	indexer := &fakeIndexer{}

	if err := newTestJob(repository, indexer).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if repository.claimCalls != 2 {
		t.Errorf("claim calls = %d, want 2 (full batch drains again, partial batch stops)", repository.claimCalls)
	}
	if indexer.calls != batchSize+2 {
		t.Errorf("store calls = %d, want %d", indexer.calls, batchSize+2)
	}
	if len(repository.completed) != batchSize+2 {
		t.Errorf("completed = %d, want %d", len(repository.completed), batchSize+2)
	}
}

func TestCycleStopsDrainingWhenQueueIsEmptyAfterAFullBatch(t *testing.T) {
	articles := map[uuid.UUID]models.Article{}
	batch := make([]models.ClaimedJob, 0, batchSize)
	for i := 0; i < batchSize; i++ {
		articleID := uuid.New()
		batch = append(batch, models.ClaimedJob{ID: uuid.New(), ArticleID: articleID, Attempts: 1})
		articles[articleID] = models.Article{ID: articleID, Title: "title", Content: "content"}
	}
	repository := &fakeRepository{
		batches:  [][]models.ClaimedJob{batch, nil},
		articles: articles,
	}
	indexer := &fakeIndexer{}

	if err := newTestJob(repository, indexer).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if repository.claimCalls != 2 {
		t.Errorf("claim calls = %d, want 2 (reclaim an empty queue and stop)", repository.claimCalls)
	}
	if indexer.calls != batchSize {
		t.Errorf("store calls = %d, want %d", indexer.calls, batchSize)
	}
}

func TestRunDisabledNeverClaimsWork(t *testing.T) {
	repository := &fakeRepository{claimed: []models.ClaimedJob{{ID: uuid.New(), ArticleID: uuid.New()}}}
	var logs bytes.Buffer
	job := NewJob(
		repository,
		&fakeIndexer{},
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
	if !bytes.Contains(logs.Bytes(), []byte("missing_config")) {
		t.Errorf("logs = %s, want the missing_config skip", logs.String())
	}
}

func TestWakeNeverBlocksWhenACycleIsAlreadyPending(t *testing.T) {
	job := NewJob(&fakeRepository{}, &fakeIndexer{}, true, testLogger())
	job.Wake()
	job.Wake()
	if len(job.wake) != 1 {
		t.Fatalf("wake buffer = %d, want a single absorbed signal", len(job.wake))
	}
}
