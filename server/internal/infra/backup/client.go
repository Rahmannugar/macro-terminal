package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/config"
)

type RepositoryState struct {
	FullCount  int64
	LastFull   time.Time
	LastLabel  string
	BackupSize int64
}

type Client struct {
	secrets []string
}

func NewClient(cfg config.Backup) *Client {
	return &Client{secrets: []string{cfg.AccessKey, cfg.SecretKey, cfg.CipherPass, cfg.Database.Password}}
}

func (client *Client) Initialize(ctx context.Context) error {
	_, err := client.command(ctx, "stanza-create")
	return err
}

func (client *Client) Full(ctx context.Context) error {
	_, err := client.command(ctx, "backup", "--type=full")
	return err
}

func (client *Client) Expire(ctx context.Context) error {
	_, err := client.command(ctx, "expire")
	return err
}

func (client *Client) State(ctx context.Context) (RepositoryState, error) {
	output, err := client.command(ctx, "info", "--output=json")
	if err != nil {
		return RepositoryState{}, err
	}
	return ParseState(output)
}

func (client *Client) command(ctx context.Context, operation string, options ...string) ([]byte, error) {
	arguments := []string{"--stanza=" + Stanza, "--log-level-console=error", "--log-level-file=off", operation}
	arguments = append(arguments, options...)
	command := exec.CommandContext(ctx, "pgbackrest", arguments...)
	command.Cancel = func() error { return command.Process.Signal(os.Interrupt) }
	command.WaitDelay = 10 * time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := stderr.String()
		for _, secret := range client.secrets {
			if secret != "" {
				message = strings.ReplaceAll(message, secret, "[redacted]")
			}
		}
		if len(message) > 2000 {
			message = message[:2000]
		}
		return nil, fmt.Errorf("pgBackRest %s: %w: %s", operation, err, strings.TrimSpace(message))
	}
	return stdout.Bytes(), nil
}

func ParseState(data []byte) (RepositoryState, error) {
	var stanzas []struct {
		Name   string `json:"name"`
		Status struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"status"`
		Backups []struct {
			Label     string `json:"label"`
			Type      string `json:"type"`
			Timestamp struct {
				Stop int64 `json:"stop"`
			} `json:"timestamp"`
			Info struct {
				Repository struct {
					Size int64 `json:"size"`
				} `json:"repository"`
			} `json:"info"`
		} `json:"backup"`
	}
	if err := json.Unmarshal(data, &stanzas); err != nil {
		return RepositoryState{}, fmt.Errorf("decode backup repository state: %w", err)
	}
	if len(stanzas) != 1 || stanzas[0].Name != Stanza {
		return RepositoryState{}, fmt.Errorf("backup repository does not describe the expected stanza")
	}
	stanza := stanzas[0]
	if stanza.Status.Code != 0 && stanza.Status.Message != "no valid backups" {
		return RepositoryState{}, fmt.Errorf("backup repository unavailable (status %d)", stanza.Status.Code)
	}
	var state RepositoryState
	for _, item := range stanza.Backups {
		if item.Type != "full" {
			continue
		}
		if item.Timestamp.Stop <= 0 || item.Label == "" {
			return RepositoryState{}, fmt.Errorf("backup repository contains an incomplete full backup")
		}
		state.FullCount++
		if timestamp := time.Unix(item.Timestamp.Stop, 0).UTC(); timestamp.After(state.LastFull) {
			state.LastFull = timestamp
			state.LastLabel = item.Label
			state.BackupSize = item.Info.Repository.Size
		}
	}
	return state, nil
}
