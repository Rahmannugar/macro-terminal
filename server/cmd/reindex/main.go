package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/config"
	indexingrepositories "github.com/Rahmannugar/macro-terminal/server/internal/indexing/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database"
)

const (
	reindexTimeout = 60 * time.Second
	enqueueBatch   = 500
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("reindex failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), reindexTimeout)
	defer cancel()

	pool, err := database.Open(ctx, cfg.Database.ConnectionString(), 5)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	repository := indexingrepositories.NewOutboxRepository(pool)
	reopened, err := repository.ReopenIndexJobs(ctx)
	if err != nil {
		return err
	}

	var enqueued int64
	for {
		queued, err := repository.EnqueueMissingIndexJobs(ctx, enqueueBatch)
		if err != nil {
			return err
		}
		enqueued += queued
		if queued == 0 {
			break
		}
	}

	logger := slog.Default()
	logger.InfoContext(ctx,
		"Article index work reset for rebuild",
		"event", "indexing.reindex",
		"reopened", reopened,
		"enqueued", enqueued,
	)
	return nil
}
