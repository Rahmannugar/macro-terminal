package retention

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepository struct {
	expired      [][]uuid.UUID
	listErr      error
	deleteErr    error
	deleted      [][]uuid.UUID
	donePurged   int64
	failedPurged int64
	notifsPurged int64
}

func (repository *fakeRepository) ListExpiredArticleIDs(_ context.Context, _ time.Time, limit int32) ([]uuid.UUID, error) {
	if repository.listErr != nil {
		return nil, repository.listErr
	}
	if len(repository.expired) == 0 {
		return nil, nil
	}
	batch := repository.expired[0]
	if int32(len(batch)) > limit {
		batch = batch[:limit]
		repository.expired[0] = batch
	} else {
		repository.expired = repository.expired[1:]
	}
	return batch, nil
}

func (repository *fakeRepository) DeleteArticles(_ context.Context, ids []uuid.UUID) error {
	if repository.deleteErr != nil {
		return repository.deleteErr
	}
	repository.deleted = append(repository.deleted, ids)
	return nil
}

func (repository *fakeRepository) PurgeDoneOutboxRows(context.Context, time.Time) (int64, error) {
	return repository.donePurged, nil
}

func (repository *fakeRepository) PurgeFailedOutboxRows(context.Context, time.Time) (int64, error) {
	return repository.failedPurged, nil
}

func (repository *fakeRepository) PurgeReadNotifications(context.Context, time.Time) (int64, error) {
	return repository.notifsPurged, nil
}

type fakeVectors struct {
	err     error
	batches [][]uuid.UUID
}

func (vectors *fakeVectors) DeleteArticles(_ context.Context, ids []uuid.UUID) error {
	if vectors.err != nil {
		return vectors.err
	}
	vectors.batches = append(vectors.batches, ids)
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func TestExpireArticlesDeletesVectorsBeforeRows(t *testing.T) {
	first := uuid.New()
	second := uuid.New()
	repository := &fakeRepository{expired: [][]uuid.UUID{{first, second}}}
	vectors := &fakeVectors{}
	job := &Job{
		repository:  repository,
		vectors:     vectors,
		articleDays: 183,
		logger:      testLogger(),
		now:         func() time.Time { return time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC) },
	}

	deleted := job.expireArticles(t.Context(), job.now().AddDate(0, 0, -job.articleDays))

	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	if len(vectors.batches) != 1 || len(vectors.batches[0]) != 2 {
		t.Fatalf("vector batches = %v, want one batch of two", vectors.batches)
	}
	if len(repository.deleted) != 1 || len(repository.deleted[0]) != 2 {
		t.Fatalf("row batches = %v, want one batch of two", repository.deleted)
	}
}

func TestExpireArticlesStopsWhenVectorDeleteFails(t *testing.T) {
	repository := &fakeRepository{expired: [][]uuid.UUID{{uuid.New()}}}
	vectors := &fakeVectors{err: errors.New("index unavailable")}
	job := &Job{
		repository:  repository,
		vectors:     vectors,
		articleDays: 183,
		logger:      testLogger(),
		now:         time.Now,
	}

	if deleted := job.expireArticles(t.Context(), job.now()); deleted != 0 {
		t.Errorf("deleted = %d, want 0 when the vector delete fails", deleted)
	}
	if len(repository.deleted) != 0 {
		t.Errorf("deleted rows = %v, want none so the next pass can retry", repository.deleted)
	}
}

func TestExpireArticlesContinuesWithoutVectors(t *testing.T) {
	repository := &fakeRepository{expired: [][]uuid.UUID{{uuid.New()}}}
	job := &Job{
		repository:  repository,
		vectors:     nil,
		articleDays: 183,
		logger:      testLogger(),
		now:         time.Now,
	}

	if deleted := job.expireArticles(t.Context(), job.now()); deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
}

func TestRunSkipsWhenDisabled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	job := &Job{repository: &fakeRepository{}, articleDays: 0, logger: testLogger(), now: time.Now}

	done := make(chan error, 1)
	go func() { done <- job.Run(ctx) }()
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want context.Canceled", err)
	}
}
