package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/safehttp"
	"github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	sources        map[string]models.Source
	created        int
	updated        int
}

func newFakeSourceRepository() *fakeSourceRepository {
	return &fakeSourceRepository{
		configurations: map[uuid.UUID]models.SourceConfiguration{},
		sources:        map[string]models.Source{},
	}
}

func (repository *fakeSourceRepository) ListSources(context.Context) ([]models.Source, error) {
	return nil, nil
}

func (repository *fakeSourceRepository) SourceByID(context.Context, uuid.UUID) (models.Source, error) {
	return models.Source{}, nil
}

func (repository *fakeSourceRepository) SourceByName(_ context.Context, name string) (models.Source, error) {
	source, ok := repository.sources[name]
	if !ok {
		return models.Source{}, pgx.ErrNoRows
	}
	return source, nil
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

func (repository *fakeSourceRepository) SourceConfigurationByID(
	_ context.Context,
	id uuid.UUID,
) (models.SourceConfiguration, error) {
	configuration, ok := repository.configurations[id]
	if !ok {
		return models.SourceConfiguration{}, models.ErrSourceConfigurationNotFound
	}
	return configuration, nil
}

func (repository *fakeSourceRepository) CreateSource(
	_ context.Context,
	source models.Source,
) (models.Source, error) {
	repository.sources[source.Name] = source
	return source, nil
}

func (repository *fakeSourceRepository) ListSourcesPage(
	context.Context, *paging.Cursor, int32,
) ([]models.Source, *paging.Cursor, error) {
	return nil, nil, nil
}

func (repository *fakeSourceRepository) ListSourceConfigurationsPage(
	context.Context, *paging.Cursor, int32,
) ([]models.SourceConfiguration, *paging.Cursor, error) {
	return nil, nil, nil
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

func TestCreateSource(t *testing.T) {
	service := NewSourceService(newFakeSourceRepository())

	if _, err := service.CreateSource(context.Background(), "", "api"); !errors.Is(err, ErrSourceNameRequired) {
		t.Fatalf("blank name error = %v, want %v", err, ErrSourceNameRequired)
	}
	if _, err := service.CreateSource(context.Background(), "BLS", ""); !errors.Is(err, ErrSourceTypeRequired) {
		t.Fatalf("blank type error = %v, want %v", err, ErrSourceTypeRequired)
	}

	created, err := service.CreateSource(context.Background(), "BLS", "api")
	if err != nil {
		t.Fatalf("CreateSource(): %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatalf("CreateSource() id = %v, want a generated id", created.ID)
	}
	if _, err := service.CreateSource(context.Background(), "BLS", "api"); !errors.Is(err, ErrSourceNameExists) {
		t.Fatalf("duplicate name error = %v, want %v", err, ErrSourceNameExists)
	}
}

func TestCreateSourceConfigurationAdmin(t *testing.T) {
	service := NewSourceService(newFakeSourceRepository())
	sourceID := uuid.New()

	blockedURLs := []string{
		`{"url":"http://10.0.0.5/feed"}`,
		`{"url":"https://example.com:8080/feed"}`,
		`{"url":"ftp://example.com/feed"}`,
	}
	for _, config := range blockedURLs {
		if _, err := service.CreateSourceConfiguration(context.Background(), sourceID, "rss", json.RawMessage(config)); !errors.Is(err, safehttp.ErrBlockedAddress) && !errors.Is(err, ErrConfigurationURLInvalid) {
			t.Errorf("config %s error = %v, want a destination refusal", config, err)
		}
	}

	created, err := service.CreateSourceConfiguration(
		context.Background(), sourceID, "rss", json.RawMessage(`{"url":"https://example.com/feed"}`),
	)
	if err != nil {
		t.Fatalf("CreateSourceConfiguration(): %v", err)
	}
	fake := service.repository.(*fakeSourceRepository)
	if fake.created != 1 {
		t.Fatalf("created = %d, want 1", fake.created)
	}

	if _, err := service.CreateSourceConfiguration(
		context.Background(), sourceID, "rss", json.RawMessage(`{"url":"https://example.com/feed"}`),
	); !errors.Is(err, ErrConfigurationExists) {
		t.Fatalf("duplicate url error = %v, want %v", err, ErrConfigurationExists)
	}
	if fake.created != 1 {
		t.Fatalf("duplicate created = %d, want 1", fake.created)
	}
	if created.ID == uuid.Nil {
		t.Fatalf("created id = %v, want a generated id", created.ID)
	}
}

func TestUpdateSourceConfigurationAdmin(t *testing.T) {
	service := NewSourceService(newFakeSourceRepository())
	sourceID := uuid.New()

	if _, err := service.UpdateSourceConfiguration(
		context.Background(), uuid.New(), json.RawMessage(`{"url":"https://example.com/feed"}`),
	); !errors.Is(err, models.ErrSourceConfigurationNotFound) {
		t.Fatalf("unknown id error = %v, want %v", err, models.ErrSourceConfigurationNotFound)
	}

	created, err := service.CreateSourceConfiguration(
		context.Background(), sourceID, "rss", json.RawMessage(`{"url":"https://example.com/feed"}`),
	)
	if err != nil {
		t.Fatalf("seed configuration: %v", err)
	}

	updated, err := service.UpdateSourceConfiguration(
		context.Background(), created.ID, json.RawMessage(`{"url":"https://example.com/moved","language":"english"}`),
	)
	if err != nil {
		t.Fatalf("UpdateSourceConfiguration(): %v", err)
	}
	if string(updated.Config) != `{"url":"https://example.com/moved","language":"english"}` {
		t.Fatalf("updated config = %s, want the new payload", updated.Config)
	}

	if _, err := service.UpdateSourceConfiguration(
		context.Background(), created.ID, json.RawMessage(`{"url":"http://192.168.1.10/feed"}`),
	); !errors.Is(err, safehttp.ErrBlockedAddress) {
		t.Fatalf("blocked update error = %v, want %v", err, safehttp.ErrBlockedAddress)
	}
}
