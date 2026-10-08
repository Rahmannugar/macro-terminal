package main

import (
	"context"
	"flag"
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

	var adminUsername, adminEmail, adminPassword string
	flag.StringVar(&adminUsername, "username", "", "admin display username (optional)")
	flag.StringVar(&adminEmail, "email", "", "admin email")
	flag.StringVar(&adminPassword, "password", "", "admin password")
	flag.Parse()

	if err := run(adminUsername, adminEmail, adminPassword); err != nil {
		logger.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run(adminUsername string, adminEmail string, adminPassword string) error {
	if (adminEmail == "") != (adminPassword == "") {
		return fmt.Errorf("seed admin email and password must be provided together")
	}
	if adminUsername != "" && adminEmail == "" {
		return fmt.Errorf("seed admin username requires email and password")
	}
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

	sourceRepository := repositories.NewSourceRepository(pool)
	sourceService := services.NewSourceService(sourceRepository)
	entityService := entityservices.NewEntityService(entityrepositories.NewEntityRepository(pool))

	var sourcesUpserted, configurationsEnsured int
	for _, seed := range append(seedUniverse, seedCandleSources...) {
		source, err := sourceService.UpsertSource(ctx, seed.name, seed.sourceType)
		if err != nil {
			return fmt.Errorf("seed source %q: %w", seed.name, err)
		}
		sourcesUpserted++

		for _, configuration := range seed.configurations {
			if seed.sourceType == "candles" {
				if err := ensureCandleConfiguration(ctx, sourceRepository, source.ID, configuration); err != nil {
					return fmt.Errorf("seed configuration for %q: %w", seed.name, err)
				}
			} else if _, err := sourceService.EnsureSourceConfiguration(
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

	var indicatorsUpserted, indicatorTermsUpserted int
	for _, seed := range seedIndicators {
		indicator, err := entityService.EnsureIndicator(ctx, seed.name, seed.kind, seed.entity)
		if err != nil {
			return fmt.Errorf("seed indicator %q: %w", seed.name, err)
		}
		indicatorsUpserted++
		for _, term := range seed.terms {
			if _, err := entityService.EnsureIndicatorTerm(ctx, term, "topic", indicator); err != nil {
				return fmt.Errorf("seed indicator term %q for %q: %w", term, seed.name, err)
			}
			indicatorTermsUpserted++
		}
	}

	if adminEmail != "" {
		normalizedEmail, err := ensureAdmin(ctx, pool, adminUsername, adminEmail, adminPassword)
		if err != nil {
			return err
		}
		slog.Info("seed admin ready", "email", normalizedEmail)
	}

	slog.Info(
		"seed complete",
		"sources_upserted", sourcesUpserted,
		"configurations_ensured", configurationsEnsured,
		"entities_upserted", entitiesUpserted,
		"pairs_upserted", pairsUpserted,
		"knowledge_terms_upserted", termsUpserted,
		"indicators_upserted", indicatorsUpserted,
		"indicator_terms_upserted", indicatorTermsUpserted,
	)
	return nil
}
