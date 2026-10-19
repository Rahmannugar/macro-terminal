package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoadAppliesAIDefaultsAndOverrides(t *testing.T) {
	setRequiredEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with defaults: %v", err)
	}
	if cfg.AI.Model != "gemini-3.1-flash-lite" {
		t.Errorf("model = %q, want the shipped default", cfg.AI.Model)
	}
	if cfg.AI.ExplanationModel != "gemini-3.5-flash-lite" {
		t.Errorf("explanation model = %q, want the shipped default", cfg.AI.ExplanationModel)
	}
	if cfg.AI.EmbeddingModel != "gemini-embedding-001" {
		t.Errorf("embedding model = %q, want the shipped default", cfg.AI.EmbeddingModel)
	}

	t.Setenv("MACRO_TERMINAL_AI_API_KEY", " key ")
	t.Setenv("MACRO_TERMINAL_AI_BACKUP_API_KEY", " backup-key ")
	t.Setenv("MACRO_TERMINAL_AI_MODEL", "other-model")
	t.Setenv("MACRO_TERMINAL_AI_EXPLANATION_MODEL", "other-explain-model")
	t.Setenv("MACRO_TERMINAL_AI_EMBEDDING_MODEL", "other-embed-model")
	t.Setenv("MACRO_TERMINAL_AHNLICH_DB_ADDR", " ahnlich-db:1369 ")

	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load with overrides: %v", err)
	}
	if cfg.AI.APIKey != "key" {
		t.Errorf("api key = %q, want the trimmed override", cfg.AI.APIKey)
	}
	if cfg.AI.BackupAPIKey != "backup-key" {
		t.Errorf("backup key was not loaded and trimmed")
	}
	if cfg.AI.Model != "other-model" {
		t.Errorf("model = %q, want the override", cfg.AI.Model)
	}
	if cfg.AI.ExplanationModel != "other-explain-model" {
		t.Errorf("explanation model = %q, want the override", cfg.AI.ExplanationModel)
	}
	if cfg.AI.EmbeddingModel != "other-embed-model" {
		t.Errorf("embedding model = %q, want the override", cfg.AI.EmbeddingModel)
	}
	if cfg.Ahnlich.DBAddr != "ahnlich-db:1369" {
		t.Errorf("ahnlich db addr = %q, want the trimmed override", cfg.Ahnlich.DBAddr)
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
	t.Setenv("MACRO_TERMINAL_AUTH_OTP_HMAC_SECRET", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("MACRO_TERMINAL_RESEND_API_KEY", "re_test_key")
	t.Setenv("MACRO_TERMINAL_RESEND_NOREPLY_FROM", "Macro Terminal <noreply@example.com>")
}

func TestLoadAppliesRetentionDefaultsAndOverrides(t *testing.T) {
	setRequiredEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with defaults: %v", err)
	}
	if cfg.Retention.ArticleDays != 183 {
		t.Errorf("article retention = %d, want the shipped default", cfg.Retention.ArticleDays)
	}

	t.Setenv("MACRO_TERMINAL_RETENTION_ARTICLE_DAYS", "90")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load with retention override: %v", err)
	}
	if cfg.Retention.ArticleDays != 90 {
		t.Errorf("article retention = %d, want the override", cfg.Retention.ArticleDays)
	}
}

func TestValidateRequiresRetentionWindow(t *testing.T) {
	setRequiredEnvironment(t)
	base, err := Load()
	if err != nil {
		t.Fatalf("Load with required environment: %v", err)
	}

	tooShort := base
	tooShort.Retention.ArticleDays = 7
	if err := tooShort.Validate(); err == nil ||
		!strings.Contains(err.Error(), "MACRO_TERMINAL_RETENTION_ARTICLE_DAYS") {
		t.Errorf("short retention error = %v, want the setting named", err)
	}

	if err := base.Validate(); err != nil {
		t.Errorf("Validate with required configuration: %v", err)
	}
}

func TestValidateRequiresOTPAndResendConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	base, err := Load()
	if err != nil {
		t.Fatalf("Load with required environment: %v", err)
	}

	shortSecret := base
	shortSecret.Auth.OTPHMACSecret = make([]byte, 31)
	if err := shortSecret.Validate(); err == nil ||
		!strings.Contains(err.Error(), "MACRO_TERMINAL_AUTH_OTP_HMAC_SECRET") {
		t.Errorf("short OTP secret error = %v, want the secret named", err)
	}

	missingKey := base
	missingKey.Resend.APIKey = ""
	if err := missingKey.Validate(); err == nil ||
		!strings.Contains(err.Error(), "MACRO_TERMINAL_RESEND_API_KEY") {
		t.Errorf("missing Resend key error = %v, want the key named", err)
	}

	badSender := base
	badSender.Resend.NoReplyFrom = "not-an-address"
	if err := badSender.Validate(); err == nil ||
		!strings.Contains(err.Error(), "MACRO_TERMINAL_RESEND_NOREPLY_FROM") {
		t.Errorf("invalid sender error = %v, want the sender named", err)
	}

	if err := base.Validate(); err != nil {
		t.Errorf("Validate with required configuration: %v", err)
	}
}
