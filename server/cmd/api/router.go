package main

import (
	"fmt"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
	"github.com/Rahmannugar/macro-terminal/server/internal/articles"
	articlerepositories "github.com/Rahmannugar/macro-terminal/server/internal/articles/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/authentication"
	calendarrepositories "github.com/Rahmannugar/macro-terminal/server/internal/calendar/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/clustering"
	clusteringrepositories "github.com/Rahmannugar/macro-terminal/server/internal/clustering/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/config"
	enrichmentrepositories "github.com/Rahmannugar/macro-terminal/server/internal/enrichment/repositories"
	entityrepositories "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories"
	"github.com/Rahmannugar/macro-terminal/server/internal/explanation"
	"github.com/Rahmannugar/macro-terminal/server/internal/health"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cache"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/cors"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/telemetry"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/Rahmannugar/macro-terminal/server/internal/search"
	"github.com/Rahmannugar/macro-terminal/server/internal/vector"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const aiHTTPTimeout = 30 * time.Second

func newRouter(
	cfg config.Config,
	runtime *telemetry.Runtime,
	database *pgxpool.Pool,
	redisClient *redis.Client,
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

	authComponents, err := newAuthenticationComponents(cfg, database, redisClient)
	if err != nil {
		return nil, err
	}
	authentication.RegisterRoutes(router, authComponents.service, authComponents.handler, authComponents.sessionCookie)

	var searcher vector.Searcher
	if cfg.Ahnlich.AIAddr != "" {
		searcher, err = vector.NewSearcher(cfg.Ahnlich.AIAddr)
		if err != nil {
			return nil, fmt.Errorf("connect vector search: %w", err)
		}
	}
	resourceStore := cache.NewJSONStore(redisClient)
	articleRepository := articlerepositories.NewArticleRepository(database, resourceStore)
	clusterRepository := clusteringrepositories.NewRepository(database, resourceStore)
	relatedService := clustering.NewService(clusterRepository, searcher, articleRepository)
	search.RegisterRoutes(router, search.NewService(searcher, articleRepository))
	clustering.RegisterRoutes(router, relatedService)
	articles.RegisterRoutes(router, articleRepository)
	explanation.RegisterRoutes(router, explanation.NewService(
		articleRepository,
		enrichmentrepositories.NewOutboxRepository(database, resourceStore),
		entityrepositories.NewEntityRepository(database),
		calendarrepositories.NewEventRepository(database, resourceStore),
		clusterRepository,
		relatedService,
		ai.NewExplainer(telemetry.NewHTTPClient(aiHTTPTimeout), cfg.AI.APIKey, cfg.AI.ExplanationModel),
		cache.NewJSONStoreWithTTL(redisClient, explanation.CacheTTL),
	))
	return router, nil
}
