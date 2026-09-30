package main

import (
	"fmt"

	"github.com/Rahmannugar/macro-terminal/server/internal/config"
	"github.com/Rahmannugar/macro-terminal/server/internal/health"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cors"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/telemetry"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newRouter(
	cfg config.Config,
	runtime *telemetry.Runtime,
	database *pgxpool.Pool,
) (*gin.Engine, error) {
	if cfg.Environment != config.EnvironmentDevelopment {
		gin.SetMode(gin.ReleaseMode)
	}

	requestTelemetry, err := runtime.HTTPMiddleware()
	if err != nil {
		return nil, fmt.Errorf("configure HTTP telemetry: %w", err)
	}
	router := gin.New()
	allowedOrigins := append([]string{cfg.Auth.BaseURL}, cfg.Auth.TrustedOrigins...)
	router.Use(cors.Middleware(allowedOrigins), requestTelemetry, gin.Recovery())
	if err := router.SetTrustedProxies(cfg.Auth.TrustedProxies); err != nil {
		return nil, fmt.Errorf("configure trusted HTTP proxies: %w", err)
	}

	health.RegisterRoutes(router, database)
	if err := openapi.RegisterRoutes(router); err != nil {
		return nil, fmt.Errorf("register OpenAPI routes: %w", err)
	}
	return router, nil
}
