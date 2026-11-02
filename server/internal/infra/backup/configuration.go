package backup

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Rahmannugar/macro-terminal/server/internal/config"
)

const (
	Stanza            = "macro-terminal"
	ConfigurationPath = "/etc/pgbackrest/pgbackrest.conf"
	DataPath          = "/var/lib/postgresql/data"
	SocketPath        = "/var/run/postgresql"
	LockPath          = "/var/run/pgbackrest"
	RetainedFull      = 7
)

func Configure(cfg config.Backup) error {
	if !cfg.Enabled {
		if err := os.Remove(ConfigurationPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove disabled backup configuration: %w", err)
		}
		return nil
	}
	contents, err := RenderConfiguration(cfg)
	if err != nil {
		return err
	}
	owner, err := user.Lookup("postgres")
	if err != nil {
		return fmt.Errorf("resolve PostgreSQL process owner: %w", err)
	}
	uid, err := strconv.Atoi(owner.Uid)
	if err != nil {
		return fmt.Errorf("resolve PostgreSQL owner ID: %w", err)
	}
	gid, err := strconv.Atoi(owner.Gid)
	if err != nil {
		return fmt.Errorf("resolve PostgreSQL group ID: %w", err)
	}
	for _, path := range []string{filepath.Dir(ConfigurationPath), LockPath} {
		if err := os.MkdirAll(path, 0750); err != nil {
			return fmt.Errorf("create backup runtime directory: %w", err)
		}
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("set backup directory owner: %w", err)
		}
	}
	if err := os.WriteFile(ConfigurationPath, []byte(contents), 0600); err != nil {
		return fmt.Errorf("write backup configuration: %w", err)
	}
	if err := os.Chown(ConfigurationPath, uid, gid); err != nil {
		return fmt.Errorf("set backup configuration owner: %w", err)
	}
	return nil
}

func RenderConfiguration(cfg config.Backup) (string, error) {
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	if !cfg.Enabled {
		return "", fmt.Errorf("backups are disabled")
	}
	var output strings.Builder
	_, err := fmt.Fprintf(&output, `[global]
repo1-type=s3
repo1-path=/macro-terminal
repo1-s3-bucket=%s
repo1-s3-region=%s
repo1-s3-endpoint=%s
repo1-storage-port=%d
repo1-s3-uri-style=%s
repo1-s3-key=%s
repo1-s3-key-secret=%s
repo1-cipher-type=aes-256-cbc
repo1-cipher-pass=%s
repo1-retention-full=%d
repo1-retention-full-type=count
repo1-retention-history=7
repo1-bundle=y
compress-type=zst
start-fast=y
lock-path=%s
log-level-console=error
log-level-file=off

[%s]
pg1-path=%s
pg1-socket-path=%s
pg1-user=%s
pg1-database=postgres
pg1-port=%d
`, cfg.Bucket, cfg.Region, cfg.Endpoint, cfg.StoragePort, cfg.URIStyle,
		cfg.AccessKey, cfg.SecretKey, cfg.CipherPass, RetainedFull, LockPath,
		Stanza, DataPath, SocketPath, cfg.Database.User, cfg.Database.Port)
	if err != nil {
		return "", fmt.Errorf("render backup configuration: %w", err)
	}
	if cfg.CAFile != "" {
		// Repository TLS settings belong to the global section.
		return strings.Replace(output.String(), "[global]\n", "[global]\nrepo1-storage-ca-file="+cfg.CAFile+"\n", 1), nil
	}
	return output.String(), nil
}
