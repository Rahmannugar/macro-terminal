package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/google/uuid"
)

func TestValidateConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		configType string
		config     string
		wantErr    error
		wantURL    string
	}{
		{
			name:       "valid api configuration",
			configType: "api",
			config:     `{"url":"https://api.bls.gov/publicAPI/v2/timeseries/data/"}`,
			wantURL:    "https://api.bls.gov/publicAPI/v2/timeseries/data/",
		},
		{
			name:       "environment reference keys are allowed",
			configType: "api",
			config:     `{"url":"https://apps.bea.gov/api/data","api_key_param":"UserID","api_key_env":"MACRO_TERMINAL_BEA_API_KEY"}`,
			wantURL:    "https://apps.bea.gov/api/data",
		},
		{
			name:       "unknown configuration type",
			configType: "ftp",
			config:     `{"url":"https://example.com"}`,
			wantErr:    ErrConfigurationTypeInvalid,
		},
		{
			name:       "configuration is not an object",
			configType: "rss",
			config:     `["https://example.com/feed"]`,
			wantErr:    ErrConfigurationNotObject,
		},
		{
			name:       "missing url",
			configType: "web",
			config:     `{"selector":"article h1"}`,
			wantErr:    ErrConfigurationURLRequired,
		},
		{
			name:       "blank url",
			configType: "api",
			config:     `{"url":"   "}`,
			wantErr:    ErrConfigurationURLRequired,
		},
		{
			name:       "request spacing interval accepted",
			configType: "api",
			config:     `{"url":"https://api.gdeltproject.org/api/v2/doc/doc","min_interval_s":5}`,
			wantURL:    "https://api.gdeltproject.org/api/v2/doc/doc",
		},
		{
			name:       "non-integer request spacing rejected",
			configType: "api",
			config:     `{"url":"https://example.com","min_interval_s":"5"}`,
			wantErr:    ErrConfigurationIntervalInvalid,
		},
		{
			name:       "non-positive request spacing rejected",
			configType: "api",
			config:     `{"url":"https://example.com","min_interval_s":0}`,
			wantErr:    ErrConfigurationIntervalInvalid,
		},
		{
			name:       "literal secret value rejected",
			configType: "api",
			config:     `{"url":"https://example.com","token":"abc123"}`,
			wantErr:    ErrConfigurationSecretInConfig,
		},
		{
			name:       "literal api key rejected",
			configType: "api",
			config:     `{"url":"https://example.com","api_key":"abc123"}`,
			wantErr:    ErrConfigurationSecretInConfig,
		},
		{
			name:       "nested-style secret key rejected",
			configType: "api",
			config:     `{"url":"https://example.com","client_secret":"abc123"}`,
			wantErr:    ErrConfigurationSecretInConfig,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			url, err := validateConfiguration(test.configType, json.RawMessage(test.config))
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("validateConfiguration() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateConfiguration() unexpected error: %v", err)
			}
			if url != test.wantURL {
				t.Fatalf("validateConfiguration() url = %q, want %q", url, test.wantURL)
			}
		})
	}
}

type fakeSourceRepository struct {
	configurations map[uuid.UUID]models.SourceConfiguration
	created        int
	updated        int
}

func newFakeSourceRepository() *fakeSourceRepository {
	return &fakeSourceRepository{configurations: map[uuid.UUID]models.SourceConfiguration{}}
}

func (repository *fakeSourceRepository) ListSources(context.Context) ([]models.Source, error) {
	return nil, nil
}

func (repository *fakeSourceRepository) SourceByID(context.Context, uuid.UUID) (models.Source, error) {
	return models.Source{}, nil
}

func (repository *fakeSourceRepository) SourceByName(context.Context, string) (models.Source, error) {
	return models.Source{}, nil
}

func (repository *fakeSourceRepository) UpsertSource(_ context.Context, source models.Source) (models.Source, error) {
	return source, nil
}

func (repository *fakeSourceRepository) ConfigurationsByType(
	_ context.Context,
	sourceID uuid.UUID,
	configType string,
) ([]models.SourceConfiguration, error) {
	var configurations []models.SourceConfiguration
	for _, configuration := range repository.configurations {
		if configuration.SourceID == sourceID && configuration.Type == configType {
			configurations = append(configurations, configuration)
		}
	}
	return configurations, nil
}

func (repository *fakeSourceRepository) ConfigurationsBySource(context.Context, uuid.UUID) ([]models.SourceConfiguration, error) {
	return nil, nil
}

func (repository *fakeSourceRepository) SourceConfigurationByURL(
	_ context.Context,
	sourceID uuid.UUID,
	configType string,
	configURL string,
) (models.SourceConfiguration, error) {
	for _, configuration := range repository.configurations {
		if configuration.SourceID != sourceID || configuration.Type != configType {
			continue
		}
		var object map[string]any
		if err := json.Unmarshal(configuration.Config, &object); err != nil {
			continue
		}
		if object["url"] == configURL {
			return configuration, nil
		}
	}
	return models.SourceConfiguration{}, models.ErrSourceConfigurationNotFound
}

func (repository *fakeSourceRepository) CreateSourceConfiguration(
	_ context.Context,
	configuration models.SourceConfiguration,
) (models.SourceConfiguration, error) {
	repository.created++
	repository.configurations[configuration.ID] = configuration
	return configuration, nil
}

func (repository *fakeSourceRepository) UpdateSourceConfiguration(
	_ context.Context,
	configuration models.SourceConfiguration,
) (models.SourceConfiguration, error) {
	repository.updated++
	existing := repository.configurations[configuration.ID]
	existing.Config = configuration.Config
	repository.configurations[configuration.ID] = existing
	return existing, nil
}

func TestEnsureSourceConfiguration(t *testing.T) {
	sourceID := uuid.New()
	service := NewSourceService(newFakeSourceRepository())
	fake := service.repository.(*fakeSourceRepository)

	created, err := service.EnsureSourceConfiguration(
		context.Background(),
		sourceID,
		"rss",
		json.RawMessage(`{"url":"https://example.com/feed"}`),
	)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if fake.created != 1 || fake.updated != 0 {
		t.Fatalf("first ensure created=%d updated=%d, want created=1 updated=0", fake.created, fake.updated)
	}

	same, err := service.EnsureSourceConfiguration(
		context.Background(),
		sourceID,
		"rss",
		json.RawMessage(`{"url":"https://example.com/feed"}`),
	)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if same.ID != created.ID {
		t.Fatalf("second ensure returned a different configuration")
	}
	if fake.created != 1 || fake.updated != 0 {
		t.Fatalf("idempotent ensure created=%d updated=%d, want created=1 updated=0", fake.created, fake.updated)
	}

	changed, err := service.EnsureSourceConfiguration(
		context.Background(),
		sourceID,
		"rss",
		json.RawMessage(`{"url":"https://example.com/feed","language":"english"}`),
	)
	if err != nil {
		t.Fatalf("third ensure: %v", err)
	}
	if changed.ID != created.ID {
		t.Fatalf("config change should update in place, got a new configuration")
	}
	if fake.created != 1 || fake.updated != 1 {
		t.Fatalf("config change created=%d updated=%d, want created=1 updated=1", fake.created, fake.updated)
	}

	if _, err := service.EnsureSourceConfiguration(
		context.Background(),
		sourceID,
		"rss",
		json.RawMessage(`{"url":"https://example.com/feed","password":"hunter2"}`),
	); !errors.Is(err, ErrConfigurationSecretInConfig) {
		t.Fatalf("secret in configuration error = %v, want %v", err, ErrConfigurationSecretInConfig)
	}
}

func TestEnsureSourceConfigurationFollowsMovedURL(t *testing.T) {
	sourceID := uuid.New()
	service := NewSourceService(newFakeSourceRepository())
	fake := service.repository.(*fakeSourceRepository)

	original, err := service.EnsureSourceConfiguration(
		context.Background(),
		sourceID,
		"web",
		json.RawMessage(`{"url":"https://example.com/news/index.html"}`),
	)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}

	moved, err := service.EnsureSourceConfiguration(
		context.Background(),
		sourceID,
		"web",
		json.RawMessage(`{"url":"https://example.com/news/2026/index.html","selectors":{"item":".item","title":".title"}}`),
	)
	if err != nil {
		t.Fatalf("moved url ensure: %v", err)
	}
	if moved.ID != original.ID {
		t.Fatalf("moved url should update the existing configuration, got a second row")
	}
	if fake.created != 1 || fake.updated != 1 {
		t.Fatalf("moved url created=%d updated=%d, want created=1 updated=1", fake.created, fake.updated)
	}
}
