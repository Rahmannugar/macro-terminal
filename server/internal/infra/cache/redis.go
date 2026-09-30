package cache

import (
	"fmt"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

func Open(connectionString string) (*redis.Client, error) {
	options, err := redis.ParseURL(connectionString)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URL: %w", err)
	}

	client := redis.NewClient(options)
	if err := redisotel.InstrumentTracing(client); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("instrument Redis tracing: %w", err)
	}
	if err := redisotel.InstrumentMetrics(client); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("instrument Redis metrics: %w", err)
	}

	return client, nil
}
