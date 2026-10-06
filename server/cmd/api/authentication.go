package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Rahmannugar/authlier"
	"github.com/Rahmannugar/authlier/emailverification"
	"github.com/Rahmannugar/authlier/sessiontoken"
	authlierpostgres "github.com/Rahmannugar/authlier/storage/postgres"
	authlierredis "github.com/Rahmannugar/authlier/storage/redis"
	"github.com/Rahmannugar/macro-terminal/server/internal/authentication"
	"github.com/Rahmannugar/macro-terminal/server/internal/config"
	infraauthentication "github.com/Rahmannugar/macro-terminal/server/internal/infra/authentication"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/emaildelivery"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/ratelimit"
	userrepositories "github.com/Rahmannugar/macro-terminal/server/internal/users/repositories"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	authMigrationTimeout = 30 * time.Second
	sessionLifetime      = 7 * 24 * time.Hour
	sessionCacheTTL      = time.Hour
	sessionExtension     = 24 * time.Hour
	absoluteSession      = 30 * 24 * time.Hour
	sessionCachePrefix   = "macro_terminal:auth"
	rateLimitPrefix      = "macro_terminal:rate-limit"
	localSessionCookie   = "macro_terminal_session"
	secureSessionCookie  = "__Host-macro_terminal_session"
)

type authenticationComponents struct {
	handler       http.Handler
	service       *authentication.Service
	sessionCookie *http.Cookie
}

func newAuthenticationComponents(
	cfg config.Config,
	databasePool *pgxpool.Pool,
	redisClient *redis.Client,
) (authenticationComponents, error) {
	authlierPostgres, err := authlierpostgres.New(databasePool, authlierpostgres.Config{})
	if err != nil {
		return authenticationComponents{}, fmt.Errorf("configure Authlier PostgreSQL storage: %w", err)
	}
	migrationContext, cancelMigration := context.WithTimeout(context.Background(), authMigrationTimeout)
	err = authlierPostgres.Migrate(migrationContext)
	cancelMigration()
	if err != nil {
		return authenticationComponents{}, fmt.Errorf("migrate Authlier PostgreSQL storage: %w", err)
	}

	sessionCache, err := authlierredis.NewSessionCache(redisClient, sessionCachePrefix)
	if err != nil {
		return authenticationComponents{}, fmt.Errorf("configure Authlier session cache: %w", err)
	}
	limiter, err := ratelimit.NewRedisLimiter(redisClient, cfg.Auth.OTPHMACSecret, rateLimitPrefix)
	if err != nil {
		return authenticationComponents{}, fmt.Errorf("configure authentication rate limiter: %w", err)
	}
	emailQueue, err := emaildelivery.NewQueue(databasePool, cfg.Auth.OTPHMACSecret)
	if err != nil {
		return authenticationComponents{}, fmt.Errorf("configure email delivery queue: %w", err)
	}

	auth, err := authlier.New(authlier.Config{
		AppName:         "Macro Terminal",
		BaseURL:         cfg.Auth.BaseURL,
		BasePath:        "/auth",
		AccountBasePath: "/account",
		Database:        authlierPostgres,
		TrustedOrigins:  cfg.Auth.TrustedOrigins,
		TrustedProxies:  cfg.Auth.TrustedProxies,
		EmailAndPassword: authlier.EmailAndPasswordConfig{
			Enabled:                  true,
			RequireEmailVerification: true,
			AttemptGuard:             infraauthentication.NewPasswordAttemptGuard(limiter),
		},
		EmailVerification: authlier.EmailVerificationConfig{
			Enabled:                     true,
			Delivery:                    emailverification.DeliveryMethodOTP,
			OTPSecret:                   cfg.Auth.OTPHMACSecret,
			Sender:                      emailQueue,
			SendOnSignUp:                true,
			AutoSignInAfterVerification: true,
			AttemptGuard:                infraauthentication.NewOTPAttemptGuard(limiter),
		},
		Session: authlier.SessionConfig{
			Mode:     authlier.SessionModeCookie,
			Lifetime: sessionLifetime,
			Extension: &sessiontoken.ExtensionConfig{
				ExtendAfter:      sessionExtension,
				AbsoluteLifetime: absoluteSession,
			},
			Cache:    sessionCache,
			CacheTTL: sessionCacheTTL,
			Cookie: authlier.CookieConfig{
				Name:     sessionCookieName(cfg.Environment),
				Path:     "/",
				SameSite: http.SameSiteLaxMode,
			},
		},
	})
	if err != nil {
		return authenticationComponents{}, fmt.Errorf("configure Authlier: %w", err)
	}

	return authenticationComponents{
		handler: auth.Handler(),
		service: authentication.NewService(
			auth,
			userrepositories.NewUserRepository(databasePool),
			authlierPostgres.EmailPassword(),
		),
		sessionCookie: &http.Cookie{
			Name:     sessionCookieName(cfg.Environment),
			Path:     "/",
			SameSite: http.SameSiteLaxMode,
			HttpOnly: true,
			Secure:   strings.HasPrefix(cfg.Auth.BaseURL, "https://"),
		},
	}, nil
}

func sessionCookieName(environment config.Environment) string {
	if environment == config.EnvironmentProduction {
		return secureSessionCookie
	}
	return localSessionCookie
}
