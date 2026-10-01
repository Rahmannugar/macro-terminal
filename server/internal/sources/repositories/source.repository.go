package repositories

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcedb "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SourceRepository struct {
	queries *sourcedb.Queries
}

func NewSourceRepository(pool *pgxpool.Pool) *SourceRepository {
	return &SourceRepository{queries: sourcedb.New(pool)}
}

func (repository *SourceRepository) ListSources(ctx context.Context) ([]models.Source, error) {
	rows, err := repository.queries.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	sources := make([]models.Source, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, mapSource(row))
	}
	return sources, nil
}

func (repository *SourceRepository) SourceByID(ctx context.Context, id uuid.UUID) (models.Source, error) {
	row, err := repository.queries.GetSourceByID(ctx, id)
	if err != nil {
		return models.Source{}, fmt.Errorf("get source by ID: %w", err)
	}
	return mapSource(row), nil
}

func (repository *SourceRepository) SourceByName(ctx context.Context, name string) (models.Source, error) {
	row, err := repository.queries.GetSourceByName(ctx, name)
	if err != nil {
		return models.Source{}, fmt.Errorf("get source by name: %w", err)
	}
	return mapSource(row), nil
}

func (repository *SourceRepository) UpsertSource(ctx context.Context, source models.Source) (models.Source, error) {
	row, err := repository.queries.UpsertSource(ctx, sourcedb.UpsertSourceParams{
		ID:   source.ID,
		Name: source.Name,
		Type: source.Type,
	})
	if err != nil {
		return models.Source{}, fmt.Errorf("upsert source: %w", err)
	}
	return mapSource(row), nil
}

func (repository *SourceRepository) ConfigurationsBySource(ctx context.Context, sourceID uuid.UUID) ([]models.SourceConfiguration, error) {
	rows, err := repository.queries.ListSourceConfigurations(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("list source configurations: %w", err)
	}
	configurations := make([]models.SourceConfiguration, 0, len(rows))
	for _, row := range rows {
		configurations = append(configurations, mapConfiguration(row))
	}
	return configurations, nil
}

func (repository *SourceRepository) ConfigurationsByType(
	ctx context.Context,
	sourceID uuid.UUID,
	configType string,
) ([]models.SourceConfiguration, error) {
	rows, err := repository.queries.SourceConfigurationsByType(ctx, sourcedb.SourceConfigurationsByTypeParams{
		SourceID:   sourceID,
		ConfigType: configType,
	})
	if err != nil {
		return nil, fmt.Errorf("list source configurations by type: %w", err)
	}
	configurations := make([]models.SourceConfiguration, 0, len(rows))
	for _, row := range rows {
		configurations = append(configurations, mapConfiguration(row))
	}
	return configurations, nil
}

func (repository *SourceRepository) ListSourceConfigurationsWithSource(
	ctx context.Context,
) ([]models.SourceConfigurationWithSource, error) {
	rows, err := repository.queries.ListSourceConfigurationsWithSource(ctx)
	if err != nil {
		return nil, fmt.Errorf("list source configurations with source: %w", err)
	}
	configurations := make([]models.SourceConfigurationWithSource, 0, len(rows))
	for _, row := range rows {
		configurations = append(configurations, models.SourceConfigurationWithSource{
			SourceConfiguration: models.SourceConfiguration{
				ID:        row.ID,
				SourceID:  row.SourceID,
				Type:      row.Type,
				Config:    row.Config,
				CreatedAt: row.CreatedAt.Time,
				UpdatedAt: row.UpdatedAt.Time,
				LastRunAt: nullableTime(row.LastRunAt),
			},
			SourceName: row.SourceName,
			SourceType: row.SourceType,
		})
	}
	return configurations, nil
}

func (repository *SourceRepository) MarkSourceConfigurationsRun(
	ctx context.Context,
	ids []uuid.UUID,
	runAt time.Time,
) error {
	err := repository.queries.MarkSourceConfigurationsRun(ctx, sourcedb.MarkSourceConfigurationsRunParams{
		RunAt: pgtype.Timestamptz{Time: runAt, Valid: true},
		Ids:   ids,
	})
	if err != nil {
		return fmt.Errorf("mark source configurations run: %w", err)
	}
	return nil
}

func (repository *SourceRepository) SourceConfigurationByURL(
	ctx context.Context,
	sourceID uuid.UUID,
	configType string,
	configURL string,
) (models.SourceConfiguration, error) {
	row, err := repository.queries.GetSourceConfigurationByURL(ctx, sourcedb.GetSourceConfigurationByURLParams{
		SourceID:   sourceID,
		ConfigType: configType,
		ConfigUrl:  configURL,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return models.SourceConfiguration{}, fmt.Errorf(
			"get source configuration by URL: %w",
			models.ErrSourceConfigurationNotFound,
		)
	}
	if err != nil {
		return models.SourceConfiguration{}, fmt.Errorf("get source configuration by URL: %w", err)
	}
	return mapConfiguration(row), nil
}

func (repository *SourceRepository) CreateSourceConfiguration(
	ctx context.Context,
	configuration models.SourceConfiguration,
) (models.SourceConfiguration, error) {
	row, err := repository.queries.CreateSourceConfiguration(ctx, sourcedb.CreateSourceConfigurationParams{
		ID:       configuration.ID,
		SourceID: configuration.SourceID,
		Type:     configuration.Type,
		Config:   configuration.Config,
	})
	if err != nil {
		return models.SourceConfiguration{}, fmt.Errorf("create source configuration: %w", err)
	}
	return mapConfiguration(row), nil
}

func (repository *SourceRepository) UpdateSourceConfiguration(
	ctx context.Context,
	configuration models.SourceConfiguration,
) (models.SourceConfiguration, error) {
	row, err := repository.queries.UpdateSourceConfiguration(ctx, sourcedb.UpdateSourceConfigurationParams{
		ID:     configuration.ID,
		Config: configuration.Config,
	})
	if err != nil {
		return models.SourceConfiguration{}, fmt.Errorf("update source configuration: %w", err)
	}
	return mapConfiguration(row), nil
}

func mapSource(row sourcedb.Source) models.Source {
	return models.Source{
		ID:        row.ID,
		Name:      row.Name,
		Type:      row.Type,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

func mapConfiguration(row sourcedb.SourceConfiguration) models.SourceConfiguration {
	return models.SourceConfiguration{
		ID:        row.ID,
		SourceID:  row.SourceID,
		Type:      row.Type,
		Config:    row.Config,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
		LastRunAt: nullableTime(row.LastRunAt),
	}
}

func nullableTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := value.Time
	return &timestamp
}
