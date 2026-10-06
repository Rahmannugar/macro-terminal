//go:build integration

package integration_test

import (
	"errors"
	"testing"

	adminjobsrepositories "github.com/Rahmannugar/macro-terminal/server/internal/adminjobs/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedOutboxJob(t *testing.T, pool *pgxpool.Pool, jobType, status string, attempts int32, lastError *string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(
		t.Context(),
		"INSERT INTO outbox (id, type, payload, status, attempts, last_error) VALUES ($1, $2, '{}'::jsonb, $3, $4, $5)",
		id, jobType, status, attempts, lastError,
	)
	if err != nil {
		t.Fatalf("insert outbox job: %v", err)
	}
	return id
}

func TestFailedJobListingAndReplay(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := adminjobsrepositories.NewJobRepository(pool)

	cause := "provider unavailable"
	failedID := seedOutboxJob(t, pool, "article_enrichment", "failed", 8, &cause)
	secondFailedID := seedOutboxJob(t, pool, "email_delivery", "failed", 3, nil)
	seedOutboxJob(t, pool, "article_index", "done", 1, nil)
	seedOutboxJob(t, pool, "asset_notification", "pending", 0, nil)

	firstPage, next, err := repository.FailedJobs(t.Context(), nil, nil, 1)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(firstPage) != 1 || next == nil {
		t.Fatalf("first page = %d rows / next %v, want one row and a cursor to continue with", len(firstPage), next)
	}
	secondPage, afterSecond, err := repository.FailedJobs(t.Context(), nil, next, 1)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(secondPage) != 1 || afterSecond != nil {
		t.Fatalf("second page = %d rows / next %v, want the last row and no cursor", len(secondPage), afterSecond)
	}
	pageIDs := map[uuid.UUID]bool{firstPage[0].ID: true, secondPage[0].ID: true}
	if !pageIDs[failedID] || !pageIDs[secondFailedID] {
		t.Errorf("cursor pages covered %v, want both seeded failed jobs exactly once", pageIDs)
	}

	jobType := "article_index"
	none, _, err := repository.FailedJobs(t.Context(), &jobType, nil, 25)
	if err != nil {
		t.Fatalf("typed list: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("typed list = %d rows, want 0 (done jobs are not failed)", len(none))
	}

	job, err := repository.JobByID(t.Context(), failedID)
	if err != nil {
		t.Fatalf("job by id: %v", err)
	}
	if job.Status != "failed" || job.Attempts != 8 || job.LastError == nil || *job.LastError != cause {
		t.Errorf("job = %+v, want failed with attempts and cause", job)
	}

	replayed, err := repository.ReplayJob(t.Context(), failedID)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replayed.Status != "pending" || replayed.Attempts != 0 || replayed.LastError != nil {
		t.Errorf("replayed job = %+v, want pending, zero attempts, no cause", replayed)
	}

	if _, err := repository.ReplayJob(t.Context(), failedID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("second replay error = %v, want no rows (only failed jobs replay)", err)
	}
	remaining, _, err := repository.FailedJobs(t.Context(), nil, nil, 25)
	if err != nil {
		t.Fatalf("list after replay: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != secondFailedID {
		t.Errorf("failed list after replay = %+v, want just the untouched failed job", remaining)
	}
}
