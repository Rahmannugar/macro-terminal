package assets

import (
	"context"
	"fmt"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/google/uuid"
)

// AlertSyncer is implemented by the notification service: following a pair
// subscribes it, unfollowing mutes it.
type AlertSyncer interface {
	SubscribePairAlerts(ctx context.Context, userID, entityPairID uuid.UUID) error
	UnsubscribePairAlerts(ctx context.Context, userID, entityPairID uuid.UUID) error
}

type alertSyncWatchList struct {
	inner  WatchList
	syncer AlertSyncer
}

// WithAlertSync wraps the watch list so follow and unfollow keep the
// notification subscriptions in step.
func WithAlertSync(inner WatchList, syncer AlertSyncer) WatchList {
	return &alertSyncWatchList{inner: inner, syncer: syncer}
}

func (watchList *alertSyncWatchList) PairsForUserPage(
	ctx context.Context,
	userID uuid.UUID,
	cursor *paging.Cursor,
	limit int32,
) ([]entitymodels.EntityPair, *paging.Cursor, error) {
	return watchList.inner.PairsForUserPage(ctx, userID, cursor, limit)
}

func (watchList *alertSyncWatchList) Subscribe(ctx context.Context, userID, entityPairID uuid.UUID) error {
	if err := watchList.inner.Subscribe(ctx, userID, entityPairID); err != nil {
		return err
	}
	if err := watchList.syncer.SubscribePairAlerts(ctx, userID, entityPairID); err != nil {
		return fmt.Errorf("subscribe pair alerts: %w", err)
	}
	return nil
}

func (watchList *alertSyncWatchList) Unsubscribe(ctx context.Context, userID, entityPairID uuid.UUID) error {
	if err := watchList.inner.Unsubscribe(ctx, userID, entityPairID); err != nil {
		return err
	}
	if err := watchList.syncer.UnsubscribePairAlerts(ctx, userID, entityPairID); err != nil {
		return fmt.Errorf("unsubscribe pair alerts: %w", err)
	}
	return nil
}
