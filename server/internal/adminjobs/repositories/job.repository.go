package repositories

import (
	"context"
	"fmt"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/adminjobs/models"
	adminjobsdb "github.com/Rahmannugar/macro-terminal/server/internal/adminjobs/repositories/generated"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type JobRepository struct {
	queries *adminjobsdb.Queries
}

func NewJobRepository(pool *pgxpool.Pool) *JobRepository {
	return &JobRepository{queries: adminjobsdb.New(pool)}
}

func (repository *JobRepository) FailedJobs(
	ctx context.Context,
	jobType *string,
	cursor *paging.Cursor,
	limit int32,
) ([]models.Job, *paging.Cursor, error) {
	params := adminjobsdb.ListFailedOutboxJobsParams{
		Type:     jobType,
		PageSize: limit + 1,
	}
	if cursor != nil {
		params.CursorUpdatedAt = pgtype.Timestamptz{Time: cursor.At, Valid: true}
		params.CursorID = pgtype.UUID{Bytes: cursor.ID, Valid: true}
	}
	rows, err := repository.queries.ListFailedOutboxJobs(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list failed jobs: %w", err)
	}
	jobs := make([]models.Job, 0, min(len(rows), int(limit)))
	for index, row := range rows {
		if int32(index) == limit {
			last := jobs[len(jobs)-1]
			return jobs, &paging.Cursor{At: last.UpdatedAt, ID: last.ID}, nil
		}
		jobs = append(jobs, jobFromRow(row.ID, row.Type, row.Status, row.Attempts, row.LastError, row.CreatedAt.Time, row.UpdatedAt.Time))
	}
	return jobs, nil, nil
}

func (repository *JobRepository) JobByID(ctx context.Context, id uuid.UUID) (models.Job, error) {
	row, err := repository.queries.GetOutboxJob(ctx, id)
	if err != nil {
		return models.Job{}, fmt.Errorf("get job: %w", err)
	}
	return jobFromRow(row.ID, row.Type, row.Status, row.Attempts, row.LastError, row.CreatedAt.Time, row.UpdatedAt.Time), nil
}

func (repository *JobRepository) ReplayJob(ctx context.Context, id uuid.UUID) (models.Job, error) {
	row, err := repository.queries.ReplayOutboxJob(ctx, id)
	if err != nil {
		return models.Job{}, fmt.Errorf("replay job: %w", err)
	}
	return jobFromRow(row.ID, row.Type, row.Status, row.Attempts, row.LastError, row.CreatedAt.Time, row.UpdatedAt.Time), nil
}

func jobFromRow(
	id uuid.UUID,
	jobType string,
	status string,
	attempts int32,
	lastError *string,
	createdAt,
	updatedAt time.Time,
) models.Job {
	return models.Job{
		ID:        id,
		Type:      jobType,
		Status:    status,
		Attempts:  attempts,
		LastError: lastError,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}
