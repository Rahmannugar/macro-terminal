package config

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

type Backup struct {
	Environment Environment
	Enabled     bool
	Database    Database
	Bucket      string
	Region      string
	Endpoint    string
	StoragePort int
	URIStyle    string
	AccessKey   string
	SecretKey   string
	CipherPass  string
	CAFile      string
}

func LoadBackup() (Backup, error) {
	k, err := loadEnvironment()
	if err != nil {
		return Backup{}, err
	}
	cfg := Backup{
		Environment: EnvironmentDevelopment,
		Database:    Database{Port: defaultPostgresPort, SSLMode: "disable"},
		Region:      "eu-central-1",
		StoragePort: 443,
		URIStyle:    "host",
	}
	if value := k.String("environment"); value != "" {
		cfg.Environment = Environment(value)
	}
	if value := k.String("backup.enabled"); value != "" {
		cfg.Enabled, err = strconv.ParseBool(value)
		if err != nil {
			return Backup{}, fmt.Errorf("MACRO_TERMINAL_BACKUP_ENABLED must be a boolean")
		}
	}
	cfg.Database.Host = strings.TrimSpace(k.String("database.host"))
	cfg.Database.Name = strings.TrimSpace(k.String("database.name"))
	cfg.Database.User = strings.TrimSpace(k.String("database.user"))
	cfg.Database.Password = k.String("database.password")
	if value := k.String("database.port"); value != "" {
		cfg.Database.Port, err = strconv.Atoi(value)
		if err != nil {
			return Backup{}, fmt.Errorf("MACRO_TERMINAL_DATABASE_PORT must be an integer")
		}
	}
	if value := k.String("database.ssl_mode"); value != "" {
		cfg.Database.SSLMode = value
	}
	cfg.Bucket = strings.TrimSpace(k.String("backup.s3_bucket"))
	if value := strings.TrimSpace(k.String("backup.s3_region")); value != "" {
		cfg.Region = value
	}
	cfg.Endpoint = strings.TrimSpace(k.String("backup.s3_endpoint"))
	if cfg.Endpoint == "" {
		cfg.Endpoint = "s3." + cfg.Region + ".amazonaws.com"
	}
	if value := k.String("backup.s3_port"); value != "" {
		cfg.StoragePort, err = strconv.Atoi(value)
		if err != nil {
			return Backup{}, fmt.Errorf("MACRO_TERMINAL_BACKUP_S3_PORT must be an integer")
		}
	}
	if value := strings.TrimSpace(k.String("backup.s3_uri_style")); value != "" {
		cfg.URIStyle = value
	}
	cfg.AccessKey = k.String("backup.s3_key")
	cfg.SecretKey = k.String("backup.s3_key_secret")
	cfg.CipherPass = k.String("backup.cipher_pass")
	cfg.CAFile = strings.TrimSpace(k.String("backup.s3_ca_file"))
	if err := cfg.Validate(); err != nil {
		return Backup{}, err
	}
	return cfg, nil
}

func (cfg Backup) Validate() error {
	if cfg.Environment != EnvironmentDevelopment && cfg.Environment != EnvironmentProduction {
		return fmt.Errorf("MACRO_TERMINAL_ENVIRONMENT must be development or production")
	}
	if !cfg.Enabled {
		return nil
	}
	values := []struct{ name, value string }{
		{"MACRO_TERMINAL_DATABASE_HOST", cfg.Database.Host},
		{"MACRO_TERMINAL_DATABASE_NAME", cfg.Database.Name},
		{"MACRO_TERMINAL_DATABASE_USER", cfg.Database.User},
		{"MACRO_TERMINAL_DATABASE_PASSWORD", cfg.Database.Password},
		{"MACRO_TERMINAL_BACKUP_S3_BUCKET", cfg.Bucket},
		{"MACRO_TERMINAL_BACKUP_S3_REGION", cfg.Region},
		{"MACRO_TERMINAL_BACKUP_S3_ENDPOINT", cfg.Endpoint},
		{"MACRO_TERMINAL_BACKUP_S3_KEY", cfg.AccessKey},
		{"MACRO_TERMINAL_BACKUP_S3_KEY_SECRET", cfg.SecretKey},
		{"MACRO_TERMINAL_BACKUP_CIPHER_PASS", cfg.CipherPass},
	}
	for _, value := range values {
		if value.value == "" {
			return fmt.Errorf("%s is required when backups are enabled", value.name)
		}
		if strings.ContainsAny(value.value, "\r\n\x00") {
			return fmt.Errorf("%s must be a single-line value", value.name)
		}
	}
	if len(cfg.CipherPass) < 32 {
		return fmt.Errorf("MACRO_TERMINAL_BACKUP_CIPHER_PASS must contain at least 32 bytes")
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`).MatchString(cfg.Bucket) || net.ParseIP(cfg.Bucket) != nil {
		return fmt.Errorf("MACRO_TERMINAL_BACKUP_S3_BUCKET must be a valid S3 bucket name")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`).MatchString(cfg.Endpoint) {
		return fmt.Errorf("MACRO_TERMINAL_BACKUP_S3_ENDPOINT must be a hostname without a scheme or port")
	}
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(cfg.Region) {
		return fmt.Errorf("MACRO_TERMINAL_BACKUP_S3_REGION must be a region identifier")
	}
	if cfg.StoragePort < 1 || cfg.StoragePort > 65535 || cfg.Database.Port < 1 || cfg.Database.Port > 65535 {
		return fmt.Errorf("backup storage and database ports must be between 1 and 65535")
	}
	if cfg.URIStyle != "host" && cfg.URIStyle != "path" {
		return fmt.Errorf("MACRO_TERMINAL_BACKUP_S3_URI_STYLE must be host or path")
	}
	if cfg.CAFile != "" && (!strings.HasPrefix(cfg.CAFile, "/") || strings.ContainsAny(cfg.CAFile, "\r\n\x00")) {
		return fmt.Errorf("MACRO_TERMINAL_BACKUP_S3_CA_FILE must be an absolute single-line path")
	}
	switch cfg.Database.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return fmt.Errorf("MACRO_TERMINAL_DATABASE_SSL_MODE must be a supported PostgreSQL SSL mode")
	}
	return nil
}
