package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	listenRetryBase   = time.Second
	listenRetryMax    = time.Minute
	listenStableAfter = 30 * time.Second
)

// Listen holds a dedicated session (listening state is per connection) and
// never returns because of a connection failure — it reconnects with
// backoff and leaves recovery of missed notifications to polling. Only
// ctx cancellation ends it.
func Listen(
	ctx context.Context,
	logger *slog.Logger,
	connectionString string,
	channel string,
	onNotify func(),
) error {
	backoff := listenRetryBase
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		started := time.Now()
		err := listenOnce(ctx, connectionString, channel, onNotify)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Since(started) >= listenStableAfter {
			backoff = listenRetryBase
		}
		logger.WarnContext(ctx, "Database notification listener disconnected",
			"event", "database.listen.disconnected",
			"operation", "database.listen",
			"channel", channel,
			"retry_in", backoff.String(),
			"error", err,
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > listenRetryMax {
			backoff = listenRetryMax
		}
	}
}

func listenOnce(ctx context.Context, connectionString, channel string, onNotify func()) error {
	connection, err := pgx.Connect(ctx, connectionString)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() {
		_ = connection.Close(ctx)
	}()
	if _, err := connection.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	for {
		if _, err := connection.WaitForNotification(ctx); err != nil {
			return fmt.Errorf("wait for notification: %w", err)
		}
		onNotify()
	}
}
