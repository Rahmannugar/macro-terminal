package main

import (
	"fmt"

	articlerepositories "github.com/Rahmannugar/macro-terminal/server/internal/articles/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/config"
	"github.com/Rahmannugar/macro-terminal/server/internal/health"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cors"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/telemetry"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/Rahmannugar/macro-terminal/server/internal/search"
	"github.com/Rahmannugar/macro-terminal/server/internal/vector"
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

	var searcher vector.Searcher
	if cfg.Ahnlich.AIAddr != "" {
		searcher, err = vector.NewSearcher(cfg.Ahnlich.AIAddr)
		if err != nil {
			return nil, fmt.Errorf("connect vector search: %w", err)
		}
	}
	search.RegisterRoutes(router, search.NewService(searcher, articlerepositories.NewArticleRepository(database)))
	return router, nil
}
