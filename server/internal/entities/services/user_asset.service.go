package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrUserIDRequired       = errors.New("user ID is required")
	ErrEntityPairIDRequired = errors.New("entity pair ID is required")
)

type UserAssetRepository interface {
	EntityPairByID(context.Context, uuid.UUID) (models.EntityPair, error)
	EntityPairsByUser(context.Context, uuid.UUID) ([]models.EntityPair, error)
	EntityPairsByUserPage(context.Context, uuid.UUID, *paging.Cursor, int32) ([]models.EntityPair, *paging.Cursor, error)
	EntityIDsByUser(context.Context, uuid.UUID) ([]uuid.UUID, error)
	UserIDsByEntityPair(context.Context, uuid.UUID) ([]uuid.UUID, error)
	SubscribeUserAsset(context.Context, uuid.UUID, uuid.UUID) error
	UnsubscribeUserAsset(context.Context, uuid.UUID, uuid.UUID) error
}

type UserAssetService struct {
	repository UserAssetRepository
}

func NewUserAssetService(repository UserAssetRepository) *UserAssetService {
	return &UserAssetService{repository: repository}
}

// Subscribe is idempotent: subscribing twice keeps a single row.
func (service *UserAssetService) Subscribe(ctx context.Context, userID, entityPairID uuid.UUID) error {
	if err := service.validatePairReference(userID, entityPairID); err != nil {
		return err
	}
	if _, err := service.repository.EntityPairByID(ctx, entityPairID); errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrEntityPairNotFound, entityPairID)
	} else if err != nil {
		return fmt.Errorf("find entity pair: %w", err)
	}

	if err := service.repository.SubscribeUserAsset(ctx, userID, entityPairID); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	return nil
}

func (service *UserAssetService) Unsubscribe(ctx context.Context, userID, entityPairID uuid.UUID) error {
	if err := service.validatePairReference(userID, entityPairID); err != nil {
		return err
	}
	if err := service.repository.UnsubscribeUserAsset(ctx, userID, entityPairID); err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	return nil
}

func (service *UserAssetService) PairsForUser(ctx context.Context, userID uuid.UUID) ([]models.EntityPair, error) {
	if userID == uuid.Nil {
		return nil, ErrUserIDRequired
	}
	pairs, err := service.repository.EntityPairsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("pairs for user: %w", err)
	}
	return pairs, nil
}

func (service *UserAssetService) PairsForUserPage(
	ctx context.Context,
	userID uuid.UUID,
	cursor *paging.Cursor,
	limit int32,
) ([]models.EntityPair, *paging.Cursor, error) {
	if userID == uuid.Nil {
		return nil, nil, ErrUserIDRequired
	}
	pairs, next, err := service.repository.EntityPairsByUserPage(ctx, userID, cursor, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("pairs for user page: %w", err)
	}
	return pairs, next, nil
}

func (service *UserAssetService) EntityIDsForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	if userID == uuid.Nil {
		return nil, ErrUserIDRequired
	}
	ids, err := service.repository.EntityIDsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("entity IDs for user: %w", err)
	}
	return ids, nil
}

func (service *UserAssetService) SubscribersForPair(ctx context.Context, entityPairID uuid.UUID) ([]uuid.UUID, error) {
	if entityPairID == uuid.Nil {
		return nil, ErrEntityPairIDRequired
	}
	userIDs, err := service.repository.UserIDsByEntityPair(ctx, entityPairID)
	if err != nil {
		return nil, fmt.Errorf("subscribers for pair: %w", err)
	}
	return userIDs, nil
}

func (service *UserAssetService) validatePairReference(userID, entityPairID uuid.UUID) error {
	if userID == uuid.Nil {
		return ErrUserIDRequired
	}
	if entityPairID == uuid.Nil {
		return ErrEntityPairIDRequired
	}
	return nil
}
