package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/safehttp"
	"github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrSourceNameRequired           = errors.New("source name is required")
	ErrSourceTypeRequired           = errors.New("source type is required")
	ErrSourceNameExists             = errors.New("source name already exists")
	ErrSourceNotFound               = errors.New("source not found")
	ErrConfigurationExists          = errors.New("source configuration already exists")
	ErrConfigurationTypeInvalid     = errors.New("configuration type must be api, rss, or web")
	ErrConfigurationNotObject       = errors.New("configuration must be a JSON object")
	ErrConfigurationURLRequired     = errors.New("configuration requires a non-empty url")
	ErrConfigurationURLInvalid      = errors.New("configuration url must be an absolute http or https URL")
	ErrConfigurationIntervalInvalid = errors.New("min_interval_s must be a positive integer")
	ErrConfigurationSecretInConfig  = errors.New("configuration must not contain secret values; reference an environment variable through an *_env key instead")
)

// Secret-looking configuration keys are rejected unless they are environment
// variable references (suffix _env), which carry a name, not a value.
var forbiddenConfigurationKeyEndings = []string{
	"token",
	"secret",
	"password",
	"passwd",
	"credential",
	"credentials",
	"authorization",
	"api_key",
	"apikey",
	"private_key",
	"access_key",
}

type SourceRepository interface {
	ListSources(context.Context) ([]models.Source, error)
	SourceByID(context.Context, uuid.UUID) (models.Source, error)
	SourceByName(context.Context, string) (models.Source, error)
	UpsertSource(context.Context, models.Source) (models.Source, error)
	ConfigurationsBySource(context.Context, uuid.UUID) ([]models.SourceConfiguration, error)
	ConfigurationsByType(context.Context, uuid.UUID, string) ([]models.SourceConfiguration, error)
	SourceConfigurationByURL(context.Context, uuid.UUID, string, string) (models.SourceConfiguration, error)
	CreateSourceConfiguration(context.Context, models.SourceConfiguration) (models.SourceConfiguration, error)
	UpdateSourceConfiguration(context.Context, models.SourceConfiguration) (models.SourceConfiguration, error)
	SourceConfigurationByID(context.Context, uuid.UUID) (models.SourceConfiguration, error)
	CreateSource(context.Context, models.Source) (models.Source, error)
	ListSourcesPage(context.Context, *paging.Cursor, int32) ([]models.Source, *paging.Cursor, error)
	ListSourceConfigurationsPage(context.Context, *paging.Cursor, int32) ([]models.SourceConfiguration, *paging.Cursor, error)
}

type SourceService struct {
	repository SourceRepository
}

func NewSourceService(repository SourceRepository) *SourceService {
	return &SourceService{repository: repository}
}

func (service *SourceService) ListSources(ctx context.Context) ([]models.Source, error) {
	sources, err := service.repository.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	return sources, nil
}

func (service *SourceService) UpsertSource(ctx context.Context, name, sourceType string) (models.Source, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Source{}, ErrSourceNameRequired
	}
	sourceType = strings.TrimSpace(sourceType)
	if sourceType == "" {
		return models.Source{}, ErrSourceTypeRequired
	}

	id, err := ids.New()
	if err != nil {
		return models.Source{}, fmt.Errorf("generate source ID: %w", err)
	}

	source, err := service.repository.UpsertSource(ctx, models.Source{
		ID:   id,
		Name: name,
		Type: sourceType,
	})
	if err != nil {
		return models.Source{}, fmt.Errorf("upsert source %q: %w", name, err)
	}
	return source, nil
}

// EnsureSourceConfiguration creates the configuration, or updates it when the
// same source/type/url combination already stores different configuration. The
// url identifies the access endpoint, so re-seeding stays idempotent while
// seed improvements still propagate. When the url itself moves (a provider
// reorganized its pages), the existing configuration of the same source and
// type is updated in place instead of leaving the old one behind.
func (service *SourceService) EnsureSourceConfiguration(
	ctx context.Context,
	sourceID uuid.UUID,
	configType string,
	config json.RawMessage,
) (models.SourceConfiguration, error) {
	configURL, err := validateConfiguration(configType, config)
	if err != nil {
		return models.SourceConfiguration{}, err
	}

	existing, err := service.repository.SourceConfigurationByURL(ctx, sourceID, configType, configURL)
	if errors.Is(err, models.ErrSourceConfigurationNotFound) {
		existing, err = service.movedConfiguration(ctx, sourceID, configType)
	}
	if err == nil {
		if string(existing.Config) == string(config) {
			return existing, nil
		}
		updated, updateErr := service.repository.UpdateSourceConfiguration(ctx, models.SourceConfiguration{
			ID:     existing.ID,
			Config: config,
		})
		if updateErr != nil {
			return models.SourceConfiguration{}, fmt.Errorf("update source configuration: %w", updateErr)
		}
		return updated, nil
	}
	if !errors.Is(err, models.ErrSourceConfigurationNotFound) {
		return models.SourceConfiguration{}, fmt.Errorf("find source configuration: %w", err)
	}

	id, idErr := ids.New()
	if idErr != nil {
		return models.SourceConfiguration{}, fmt.Errorf("generate source configuration ID: %w", idErr)
	}

	return service.createConfiguration(ctx, sourceID, configType, config, id)
}

// movedConfiguration finds the configuration a moved url should take over:
// the source's only configuration of that type. Zero or several mean there is
// nothing unambiguous to reuse, so the caller creates a new one.
func (service *SourceService) movedConfiguration(
	ctx context.Context,
	sourceID uuid.UUID,
	configType string,
) (models.SourceConfiguration, error) {
	configurations, err := service.repository.ConfigurationsByType(ctx, sourceID, configType)
	if err != nil {
		return models.SourceConfiguration{}, fmt.Errorf("list source configurations by type: %w", err)
	}
	if len(configurations) != 1 {
		return models.SourceConfiguration{}, models.ErrSourceConfigurationNotFound
	}
	return configurations[0], nil
}

func (service *SourceService) createConfiguration(
	ctx context.Context,
	sourceID uuid.UUID,
	configType string,
	config json.RawMessage,
	id uuid.UUID,
) (models.SourceConfiguration, error) {
	created, createErr := service.repository.CreateSourceConfiguration(ctx, models.SourceConfiguration{
		ID:       id,
		SourceID: sourceID,
		Type:     configType,
		Config:   config,
	})
	if createErr != nil {
		return models.SourceConfiguration{}, fmt.Errorf("create source configuration: %w", createErr)
	}
	return created, nil
}

func (service *SourceService) ListSourcesPage(
	ctx context.Context,
	cursor *paging.Cursor,
	limit int32,
) ([]models.Source, *paging.Cursor, error) {
	return service.repository.ListSourcesPage(ctx, cursor, limit)
}

func (service *SourceService) ListSourceConfigurationsPage(
	ctx context.Context,
	cursor *paging.Cursor,
	limit int32,
) ([]models.SourceConfiguration, *paging.Cursor, error) {
	return service.repository.ListSourceConfigurationsPage(ctx, cursor, limit)
}

func (service *SourceService) SourceByID(ctx context.Context, id uuid.UUID) (models.Source, error) {
	source, err := service.repository.SourceByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Source{}, fmt.Errorf("%w: %s", ErrSourceNotFound, id)
	}
	if err != nil {
		return models.Source{}, fmt.Errorf("find source by ID: %w", err)
	}
	return source, nil
}

// CreateSource creates a logical source; a duplicate name is rejected
// rather than reusing the existing source.
func (service *SourceService) CreateSource(ctx context.Context, name, sourceType string) (models.Source, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Source{}, ErrSourceNameRequired
	}
	sourceType = strings.TrimSpace(sourceType)
	if sourceType == "" {
		return models.Source{}, ErrSourceTypeRequired
	}

	if _, err := service.repository.SourceByName(ctx, name); err == nil {
		return models.Source{}, fmt.Errorf("%w: %s", ErrSourceNameExists, name)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return models.Source{}, fmt.Errorf("find source by name: %w", err)
	}

	id, err := ids.New()
	if err != nil {
		return models.Source{}, fmt.Errorf("generate source ID: %w", err)
	}

	created, err := service.repository.CreateSource(ctx, models.Source{
		ID:   id,
		Name: name,
		Type: sourceType,
	})
	if err != nil {
		return models.Source{}, fmt.Errorf("create source: %w", err)
	}
	return created, nil
}

// CreateSourceConfiguration stores a new configuration after validating
// it and refusing destinations the worker may not fetch. The same url
// may not appear twice under one source and type.
func (service *SourceService) CreateSourceConfiguration(
	ctx context.Context,
	sourceID uuid.UUID,
	configType string,
	config json.RawMessage,
) (models.SourceConfiguration, error) {
	configURL, err := validateConfiguration(configType, config)
	if err != nil {
		return models.SourceConfiguration{}, err
	}
	if err := checkConfigurationDestination(configURL); err != nil {
		return models.SourceConfiguration{}, err
	}
	if _, err := service.repository.SourceByID(ctx, sourceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.SourceConfiguration{}, fmt.Errorf("%w: %s", ErrSourceNotFound, sourceID)
		}
		return models.SourceConfiguration{}, fmt.Errorf("find configuration source: %w", err)
	}
	if _, err := service.repository.SourceConfigurationByURL(ctx, sourceID, configType, configURL); err == nil {
		return models.SourceConfiguration{}, fmt.Errorf("%w: %s", ErrConfigurationExists, configURL)
	} else if !errors.Is(err, models.ErrSourceConfigurationNotFound) {
		return models.SourceConfiguration{}, fmt.Errorf("find source configuration: %w", err)
	}

	id, err := ids.New()
	if err != nil {
		return models.SourceConfiguration{}, fmt.Errorf("generate source configuration ID: %w", err)
	}
	return service.createConfiguration(ctx, sourceID, configType, config, id)
}

// UpdateSourceConfiguration replaces the stored payload of an existing
// configuration; its type and source stay fixed.
func (service *SourceService) UpdateSourceConfiguration(
	ctx context.Context,
	id uuid.UUID,
	config json.RawMessage,
) (models.SourceConfiguration, error) {
	existing, err := service.repository.SourceConfigurationByID(ctx, id)
	if err != nil {
		return models.SourceConfiguration{}, err
	}
	configURL, err := validateConfiguration(existing.Type, config)
	if err != nil {
		return models.SourceConfiguration{}, err
	}
	if err := checkConfigurationDestination(configURL); err != nil {
		return models.SourceConfiguration{}, err
	}
	updated, err := service.repository.UpdateSourceConfiguration(ctx, models.SourceConfiguration{
		ID:     id,
		Config: config,
	})
	if err != nil {
		return models.SourceConfiguration{}, fmt.Errorf("update source configuration: %w", err)
	}
	return updated, nil
}

// checkConfigurationDestination applies the write-time address policy:
// the url must resolve to a host and the host and port must be ones the
// worker is allowed to fetch.
func checkConfigurationDestination(configURL string) error {
	target, err := url.Parse(configURL)
	if err != nil || target.Host == "" {
		return fmt.Errorf("%w: %s", ErrConfigurationURLInvalid, configURL)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return fmt.Errorf("%w: %s", ErrConfigurationURLInvalid, target.Scheme)
	}
	return safehttp.CheckDestination(target.Hostname(), target.Port())
}

func validateConfiguration(configType string, config json.RawMessage) (string, error) {
	switch strings.TrimSpace(configType) {
	case "api", "rss", "web":
	default:
		return "", ErrConfigurationTypeInvalid
	}

	var object map[string]any
	if err := json.Unmarshal(config, &object); err != nil || object == nil {
		return "", ErrConfigurationNotObject
	}

	rawURL, ok := object["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return "", ErrConfigurationURLRequired
	}

	for key := range object {
		normalized := strings.ToLower(key)
		if strings.HasSuffix(normalized, "_env") {
			continue
		}
		if hasForbiddenConfigurationKeyEnding(normalized) {
			return "", fmt.Errorf("%w: key %q", ErrConfigurationSecretInConfig, key)
		}
	}

	if rawInterval, exists := object["min_interval_s"]; exists {
		interval, ok := rawInterval.(float64)
		if !ok || interval <= 0 || interval != math.Trunc(interval) {
			return "", ErrConfigurationIntervalInvalid
		}
	}

	return strings.TrimSpace(rawURL), nil
}

func hasForbiddenConfigurationKeyEnding(normalizedKey string) bool {
	for _, ending := range forbiddenConfigurationKeyEndings {
		if normalizedKey == ending || strings.HasSuffix(normalizedKey, "_"+ending) {
			return true
		}
	}
	return false
}
