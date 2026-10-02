package config

import "testing"

func TestLoadAppliesAIDefaultsAndOverrides(t *testing.T) {
	setRequiredEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with defaults: %v", err)
	}
	if cfg.AI.Model != "gemini-3.1-flash-lite" {
		t.Errorf("model = %q, want the shipped default", cfg.AI.Model)
	}
	if cfg.AI.APIKey != "" {
		t.Errorf("api key = %q, want empty until the owner sets it", cfg.AI.APIKey)
	}

	t.Setenv("MACRO_TERMINAL_AI_API_KEY", " key ")
	t.Setenv("MACRO_TERMINAL_AI_MODEL", "other-model")

	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load with overrides: %v", err)
	}
	if cfg.AI.APIKey != "key" {
		t.Errorf("api key = %q, want the trimmed override", cfg.AI.APIKey)
	}
	if cfg.AI.Model != "other-model" {
		t.Errorf("model = %q, want the override", cfg.AI.Model)
	}
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("MACRO_TERMINAL_ENVIRONMENT", "development")
	t.Setenv("MACRO_TERMINAL_HTTP_PORT", "8081")
	t.Setenv("MACRO_TERMINAL_DATABASE_HOST", "localhost")
	t.Setenv("MACRO_TERMINAL_DATABASE_PORT", "5433")
	t.Setenv("MACRO_TERMINAL_DATABASE_NAME", "macro_terminal")
	t.Setenv("MACRO_TERMINAL_DATABASE_USER", "macro_terminal")
	t.Setenv("MACRO_TERMINAL_DATABASE_PASSWORD", "test-password")
	t.Setenv("MACRO_TERMINAL_DATABASE_SSL_MODE", "disable")
	t.Setenv("MACRO_TERMINAL_REDIS_URL", "redis://localhost:6380/0")
	t.Setenv("MACRO_TERMINAL_AUTH_BASE_URL", "http://localhost:8081")
	t.Setenv("MACRO_TERMINAL_AUTH_CLIENT_BASE_URL", "http://localhost:5173")
}
