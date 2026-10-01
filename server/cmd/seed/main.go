package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/config"
	entityrepositories "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories"
	entityservices "github.com/Rahmannugar/macro-terminal/server/internal/entities/services"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database"
	"github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/sources/services"
)

const seedTimeout = 60 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), seedTimeout)
	defer cancel()

	// The seed runs sequentially, so a small pool is enough.
	pool, err := database.Open(ctx, cfg.Database.ConnectionString(), 5)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	sourceService := services.NewSourceService(repositories.NewSourceRepository(pool))
	entityService := entityservices.NewEntityService(entityrepositories.NewEntityRepository(pool))

	var sourcesUpserted, configurationsEnsured int
	for _, seed := range seedUniverse {
		source, err := sourceService.UpsertSource(ctx, seed.name, seed.sourceType)
		if err != nil {
			return fmt.Errorf("seed source %q: %w", seed.name, err)
		}
		sourcesUpserted++

		for _, configuration := range seed.configurations {
			if _, err := sourceService.EnsureSourceConfiguration(
				ctx,
				source.ID,
				configuration.kind,
				[]byte(configuration.config),
			); err != nil {
				return fmt.Errorf("seed configuration for %q: %w", seed.name, err)
			}
			configurationsEnsured++
		}
	}

	var entitiesUpserted, pairsUpserted, termsUpserted int
	for _, seed := range seedEntities {
		if _, err := entityService.EnsureEntity(ctx, seed.code, seed.name, seed.kind); err != nil {
			return fmt.Errorf("seed entity %q: %w", seed.code, err)
		}
		entitiesUpserted++
	}
	for _, seed := range seedPairs {
		if _, err := entityService.EnsureEntityPair(ctx, seed.base, seed.quote, seed.symbol); err != nil {
			return fmt.Errorf("seed entity pair %q: %w", seed.symbol, err)
		}
		pairsUpserted++
	}
	for _, seed := range seedKnowledgeTerms {
		if _, err := entityService.EnsureKnowledgeTerm(
			ctx, seed.name, seed.kind, seed.entity,
		); err != nil {
			return fmt.Errorf("seed knowledge term %q: %w", seed.name, err)
		}
		termsUpserted++
	}

	slog.Info(
		"seed complete",
		"sources_upserted", sourcesUpserted,
		"configurations_ensured", configurationsEnsured,
		"entities_upserted", entitiesUpserted,
		"pairs_upserted", pairsUpserted,
		"knowledge_terms_upserted", termsUpserted,
	)
	return nil
}
