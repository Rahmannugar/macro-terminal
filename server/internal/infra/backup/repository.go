package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DatabaseState struct {
	LastArchived  time.Time
	ArchiveFailed int64
	PendingWAL    int64
	WALBytes      int64
	DatabaseBytes int64
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (repository *Repository) State(ctx context.Context) (DatabaseState, error) {
	var state DatabaseState
	var archived *time.Time
	err := repository.pool.QueryRow(ctx, `SELECT last_archived_time, failed_count,
        (SELECT count(*) FROM pg_ls_dir('pg_wal/archive_status') AS file WHERE file LIKE '%.ready'),
        (SELECT coalesce(sum(size), 0)::bigint FROM pg_ls_waldir()),
        pg_database_size(current_database())
        FROM pg_stat_archiver`).Scan(&archived, &state.ArchiveFailed, &state.PendingWAL, &state.WALBytes, &state.DatabaseBytes)
	if err != nil {
		return DatabaseState{}, fmt.Errorf("read PostgreSQL recovery state: %w", err)
	}
	if archived != nil {
		state.LastArchived = archived.UTC()
	}
	return state, nil
}
