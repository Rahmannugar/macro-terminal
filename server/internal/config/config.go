package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/knadh/koanf/parsers/dotenv"
	koanfenv "github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	environmentPrefix   = "MACRO_TERMINAL_"
	defaultHTTPPort     = 8080
	defaultPostgresPort = 5432
)

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentProduction  Environment = "production"
)

type Config struct {
	Environment Environment
	HTTP        HTTP
	Database    Database
	Redis       Redis
	Auth        Auth
}

type HTTP struct {
	Port int
}

type Database struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	SSLMode  string

	APIPoolMax    int32
	WorkerPoolMax int32
}

type Redis struct {
	URL string
}

type Auth struct {
	BaseURL        string
	ClientBaseURL  string
	TrustedOrigins []string
	TrustedProxies []string
}

func Load() (Config, error) {
	k := koanf.New(".")
	transform := func(key, value string) (string, any) {
		key = strings.ToLower(strings.TrimPrefix(key, environmentPrefix))
		group, setting, grouped := strings.Cut(key, "_")
		if grouped {
			key = group + "." + setting
		}
		return key, value
	}

	if err := k.Load(
		file.Provider(".env"),
		dotenv.ParserEnvWithValue(environmentPrefix, ".", transform),
	); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}

	provider := koanfenv.Provider(".", koanfenv.Opt{
		Prefix:        environmentPrefix,
		TransformFunc: transform,
	})
	if err := k.Load(provider, nil); err != nil {
		return Config{}, fmt.Errorf("load environment: %w", err)
	}

	cfg := Config{
		Environment: EnvironmentDevelopment,
		HTTP: HTTP{
			Port: defaultHTTPPort,
		},
		Database: Database{
			Port:    defaultPostgresPort,
			SSLMode: "disable",
		},
	}

	if k.Exists("environment") {
		cfg.Environment = Environment(k.String("environment"))
	}
	if k.Exists("http.port") {
		port, err := strconv.Atoi(k.String("http.port"))
		if err != nil {
			return Config{}, fmt.Errorf("MACRO_TERMINAL_HTTP_PORT must be an integer: %w", err)
		}
		cfg.HTTP.Port = port
	}
	cfg.Database.Host = strings.TrimSpace(k.String("database.host"))
	if k.Exists("database.port") {
		port, err := strconv.Atoi(k.String("database.port"))
		if err != nil {
			return Config{}, fmt.Errorf("MACRO_TERMINAL_DATABASE_PORT must be an integer: %w", err)
		}
		cfg.Database.Port = port
	}
	cfg.Database.Name = strings.TrimSpace(k.String("database.name"))
	cfg.Database.User = strings.TrimSpace(k.String("database.user"))
	cfg.Database.Password = k.String("database.password")
	if k.Exists("database.ssl_mode") {
		cfg.Database.SSLMode = strings.TrimSpace(k.String("database.ssl_mode"))
	}
	if pool := strings.TrimSpace(k.String("database.api_pool_max")); pool != "" {
		value, err := strconv.Atoi(pool)
		if err != nil {
			return Config{}, fmt.Errorf("MACRO_TERMINAL_DATABASE_API_POOL_MAX must be an integer: %w", err)
		}
		cfg.Database.APIPoolMax = int32(value)
	}
	if pool := strings.TrimSpace(k.String("database.worker_pool_max")); pool != "" {
		value, err := strconv.Atoi(pool)
		if err != nil {
			return Config{}, fmt.Errorf("MACRO_TERMINAL_DATABASE_WORKER_POOL_MAX must be an integer: %w", err)
		}
		cfg.Database.WorkerPoolMax = int32(value)
	}
	cfg.Redis.URL = k.String("redis.url")
	cfg.Auth.BaseURL = k.String("auth.base_url")
	cfg.Auth.ClientBaseURL = k.String("auth.client_base_url")
	cfg.Auth.TrustedOrigins = commaSeparated(k.String("auth.trusted_origins"))
	cfg.Auth.TrustedProxies = commaSeparated(k.String("auth.trusted_proxies"))

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) Validate() error {
	switch cfg.Environment {
	case EnvironmentDevelopment, EnvironmentProduction:
	default:
		return fmt.Errorf("MACRO_TERMINAL_ENVIRONMENT must be development or production")
	}

	if cfg.HTTP.Port < 1 || cfg.HTTP.Port > 65535 {
		return fmt.Errorf("MACRO_TERMINAL_HTTP_PORT must be between 1 and 65535")
	}
	if cfg.Database.Host == "" {
		return fmt.Errorf("MACRO_TERMINAL_DATABASE_HOST is required")
	}
	if cfg.Database.Port < 1 || cfg.Database.Port > 65535 {
		return fmt.Errorf("MACRO_TERMINAL_DATABASE_PORT must be between 1 and 65535")
	}
	if cfg.Database.Name == "" {
		return fmt.Errorf("MACRO_TERMINAL_DATABASE_NAME is required")
	}
	if cfg.Database.User == "" {
		return fmt.Errorf("MACRO_TERMINAL_DATABASE_USER is required")
	}
	if cfg.Database.Password == "" {
		return fmt.Errorf("MACRO_TERMINAL_DATABASE_PASSWORD is required")
	}
	switch cfg.Database.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return fmt.Errorf("MACRO_TERMINAL_DATABASE_SSL_MODE must be a supported PostgreSQL SSL mode")
	}
	if cfg.Database.APIPoolMax != 0 && (cfg.Database.APIPoolMax < 1 || cfg.Database.APIPoolMax > 100) {
		return fmt.Errorf("MACRO_TERMINAL_DATABASE_API_POOL_MAX must be between 1 and 100")
	}
	if cfg.Database.WorkerPoolMax != 0 && (cfg.Database.WorkerPoolMax < 1 || cfg.Database.WorkerPoolMax > 100) {
		return fmt.Errorf("MACRO_TERMINAL_DATABASE_WORKER_POOL_MAX must be between 1 and 100")
	}
	if strings.TrimSpace(cfg.Redis.URL) == "" {
		return fmt.Errorf("MACRO_TERMINAL_REDIS_URL is required")
	}
	if _, err := url.ParseRequestURI(cfg.Redis.URL); err != nil {
		return fmt.Errorf("MACRO_TERMINAL_REDIS_URL must be a valid URL: %w", err)
	}
	if err := validateOrigin("MACRO_TERMINAL_AUTH_BASE_URL", cfg.Auth.BaseURL, cfg.Environment); err != nil {
		return err
	}
	if err := validateOrigin("MACRO_TERMINAL_AUTH_CLIENT_BASE_URL", cfg.Auth.ClientBaseURL, cfg.Environment); err != nil {
		return err
	}
	return nil
}

func validateOrigin(name, value string, environment Environment) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an HTTP or HTTPS origin without a path", name)
	}
	if environment == EnvironmentProduction && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use HTTPS in production", name)
	}
	return nil
}

func (cfg HTTP) Address() string {
	return ":" + strconv.Itoa(cfg.Port)
}

func (cfg Database) ConnectionString() string {
	connection := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   cfg.Name,
	}
	query := connection.Query()
	query.Set("sslmode", cfg.SSLMode)
	connection.RawQuery = query.Encode()
	return connection.String()
}

// APIPoolOr returns MACRO_TERMINAL_DATABASE_API_POOL_MAX when set, else fallback.
func (cfg Database) APIPoolOr(fallback int32) int32 {
	if cfg.APIPoolMax > 0 {
		return cfg.APIPoolMax
	}
	return fallback
}

// WorkerPoolOr returns MACRO_TERMINAL_DATABASE_WORKER_POOL_MAX when set, else fallback.
func (cfg Database) WorkerPoolOr(fallback int32) int32 {
	if cfg.WorkerPoolMax > 0 {
		return cfg.WorkerPoolMax
	}
	return fallback
}

func commaSeparated(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}
