package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/Rahmannugar/authlier/emailpassword"
	authlierpostgres "github.com/Rahmannugar/authlier/storage/postgres"
	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	userrepositories "github.com/Rahmannugar/macro-terminal/server/internal/users/repositories"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ensureAdmin(
	ctx context.Context,
	pool *pgxpool.Pool,
	username string,
	email string,
	password string,
) (string, error) {
	identityStorage, err := authlierpostgres.New(pool, authlierpostgres.Config{})
	if err != nil {
		return "", fmt.Errorf("configure identity storage: %w", err)
	}
	if err := identityStorage.Migrate(ctx); err != nil {
		return "", fmt.Errorf("migrate identity storage: %w", err)
	}

	normalizedEmail, err := emailpassword.NormalizeEmail(email)
	if err != nil {
		return "", fmt.Errorf("normalize admin email: %w", err)
	}
	credentials := identityStorage.EmailPassword()

	var subjectID string
	identity, _, err := credentials.FindByEmail(ctx, normalizedEmail)
	switch {
	case err == nil:
		subjectID = identity.ID
	case errors.Is(err, emailpassword.ErrNotFound):
		manager, err := emailpassword.NewManager(credentials, emailpassword.Config{})
		if err != nil {
			return "", fmt.Errorf("configure password registration: %w", err)
		}
		registered, err := manager.Register(ctx, emailpassword.RegisterInput{
			Email:    normalizedEmail,
			Password: password,
		})
		if err != nil {
			return "", fmt.Errorf("register admin identity: %w", err)
		}
		subjectID = registered.ID
	default:
		return "", fmt.Errorf("look up admin identity: %w", err)
	}

	users := userrepositories.NewUserRepository(pool)
	account, err := users.ResolveUser(ctx, subjectID, models.RoleAdmin)
	if err != nil {
		return "", fmt.Errorf("bootstrap admin user: %w", err)
	}
	if account.Role != models.RoleAdmin {
		account, err = users.UpdateRole(ctx, account.ID, models.RoleAdmin)
		if err != nil {
			return "", fmt.Errorf("promote admin user: %w", err)
		}
	}
	if username != "" && account.Username == nil {
		name := username
		if _, err := users.UpdateUsername(ctx, account.ID, &name); err != nil {
			return "", fmt.Errorf("set admin username: %w", err)
		}
	}
	return normalizedEmail, nil
}
