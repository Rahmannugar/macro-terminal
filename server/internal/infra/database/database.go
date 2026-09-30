package database

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maximumConnections int32 = 20
	minimumConnections int32 = 2
)

func Open(ctx context.Context, connectionString string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		return nil, fmt.Errorf("parse connection string: %w", err)
	}
	config.MaxConns = maximumConnections
	config.MinConns = minimumConnections
	config.ConnConfig.Tracer = otelpgx.NewTracer(otelpgx.WithDisableSQLStatementInAttributes())

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := otelpgx.RecordStats(pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("record database pool metrics: %w", err)
	}

	return pool, nil
}
