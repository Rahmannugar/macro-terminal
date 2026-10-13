package notification

import (
	"context"
	"errors"
	"fmt"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/notification/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/notification/webhook"
	"github.com/google/uuid"
)

const (
	channelInApp   = "in-app"
	channelSlack   = webhook.ChannelSlack
	channelDiscord = webhook.ChannelDiscord
)

var (
	ErrWebhooksUnavailable  = errors.New("webhook channels are not configured")
	ErrInvalidChannel       = errors.New("channel must be slack or discord")
	ErrInvalidWebhookURL    = webhook.ErrInvalidWebhookURL
	ErrNotificationNotFound = errors.New("notification not found")
)

// Store is the notification repository surface the service needs.
type Store interface {
	ListUserNotificationsPage(ctx context.Context, userID uuid.UUID, cursor *paging.Cursor, limit int32) ([]models.UserNotification, *paging.Cursor, error)
	CountUserUnreadNotifications(ctx context.Context, userID uuid.UUID) (int64, error)
	MarkUserNotificationRead(ctx context.Context, id, userID uuid.UUID) (int64, error)
	MarkAllUserNotificationsRead(ctx context.Context, userID uuid.UUID) (int64, error)
	ListUserAlertPairIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	ListUserNotificationChannels(ctx context.Context, userID uuid.UUID) ([]models.UserChannel, error)
	EnsureInAppChannel(ctx context.Context, userID uuid.UUID) error
	UpsertWebhookChannel(ctx context.Context, userID uuid.UUID, channel string, nonce, ciphertext []byte, hint string) error
	DeleteUserNotificationChannel(ctx context.Context, userID uuid.UUID, channel string) (int64, error)
	UpsertPairAlertsForConfiguredChannels(ctx context.Context, userID, entityPairID uuid.UUID) (int64, error)
	DeletePairAlerts(ctx context.Context, userID, entityPairID uuid.UUID) (int64, error)
	DeleteChannelAlerts(ctx context.Context, userID uuid.UUID, channel string) (int64, error)
	SyncChannelAlertsFromInApp(ctx context.Context, userID uuid.UUID, channel string) (int64, error)
}

type ChannelSetting struct {
	Channel  string  `json:"channel" example:"slack"`
	Endpoint *string `json:"endpoint" example:"hooks.slack.com/…ab12"`
}

type Settings struct {
	InApp        bool             `json:"inApp"`
	Channels     []ChannelSetting `json:"channels"`
	AlertPairIDs []uuid.UUID      `json:"alertPairIds"`
}

type Service struct {
	store  Store
	cipher *webhook.Cipher
}

// NewService builds the notification service. A nil cipher means webhook
// channels are unavailable; in-app notifications keep working.
func NewService(store Store, cipher *webhook.Cipher) *Service {
	return &Service{store: store, cipher: cipher}
}

func (service *Service) ListPage(
	ctx context.Context,
	userID uuid.UUID,
	cursor *paging.Cursor,
	limit int32,
) ([]models.UserNotification, *paging.Cursor, error) {
	return service.store.ListUserNotificationsPage(ctx, userID, cursor, limit)
}

func (service *Service) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	return service.store.CountUserUnreadNotifications(ctx, userID)
}

func (service *Service) MarkRead(ctx context.Context, userID, notificationID uuid.UUID) error {
	updated, err := service.store.MarkUserNotificationRead(ctx, notificationID, userID)
	if err != nil {
		return err
	}
	if updated == 0 {
		return fmt.Errorf("%w: %s", ErrNotificationNotFound, notificationID)
	}
	return nil
}

func (service *Service) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	_, err := service.store.MarkAllUserNotificationsRead(ctx, userID)
	return err
}

func (service *Service) Settings(ctx context.Context, userID uuid.UUID) (Settings, error) {
	channels, err := service.store.ListUserNotificationChannels(ctx, userID)
	if err != nil {
		return Settings{}, err
	}
	settings := Settings{
		Channels:     make([]ChannelSetting, 0, len(channels)),
		AlertPairIDs: []uuid.UUID{},
	}
	for _, channel := range channels {
		if channel.Channel == channelInApp {
			settings.InApp = true
			continue
		}
		settings.Channels = append(settings.Channels, ChannelSetting{
			Channel:  channel.Channel,
			Endpoint: channel.WebhookHint,
		})
	}
	pairIDs, err := service.store.ListUserAlertPairIDs(ctx, userID)
	if err != nil {
		return Settings{}, err
	}
	if pairIDs == nil {
		pairIDs = []uuid.UUID{}
	}
	settings.AlertPairIDs = pairIDs
	return settings, nil
}

// SetInApp flips the in-app kill switch. Alert rows are kept so per-pair
// subscriptions survive an outage of the whole channel.
func (service *Service) SetInApp(ctx context.Context, userID uuid.UUID, enabled bool) error {
	if enabled {
		return service.store.EnsureInAppChannel(ctx, userID)
	}
	_, err := service.store.DeleteUserNotificationChannel(ctx, userID, channelInApp)
	return err
}

func (service *Service) ConnectWebhook(ctx context.Context, userID uuid.UUID, channel, rawURL string) error {
	if channel != channelSlack && channel != channelDiscord {
		return ErrInvalidChannel
	}
	if service.cipher == nil {
		return ErrWebhooksUnavailable
	}
	clean, hint, err := webhook.Validate(rawURL)
	if err != nil {
		return fmt.Errorf("%w", ErrInvalidWebhookURL)
	}
	nonce, ciphertext, err := service.cipher.Encrypt(clean)
	if err != nil {
		return err
	}
	if err := service.store.UpsertWebhookChannel(ctx, userID, channel, nonce, ciphertext, hint); err != nil {
		return err
	}
	_, err = service.store.SyncChannelAlertsFromInApp(ctx, userID, channel)
	return err
}

func (service *Service) DisconnectChannel(ctx context.Context, userID uuid.UUID, channel string) error {
	switch channel {
	case channelSlack, channelDiscord:
		if _, err := service.store.DeleteChannelAlerts(ctx, userID, channel); err != nil {
			return err
		}
	case channelInApp:
	default:
		return ErrInvalidChannel
	}
	_, err := service.store.DeleteUserNotificationChannel(ctx, userID, channel)
	return err
}

// SetPairAlerts is the per-pair toggle: enabling subscribes the pair on
// every configured channel (creating the in-app channel on first use),
// disabling mutes the pair everywhere.
func (service *Service) SetPairAlerts(ctx context.Context, userID, entityPairID uuid.UUID, enabled bool) error {
	if !enabled {
		_, err := service.store.DeletePairAlerts(ctx, userID, entityPairID)
		return err
	}
	if err := service.store.EnsureInAppChannel(ctx, userID); err != nil {
		return err
	}
	_, err := service.store.UpsertPairAlertsForConfiguredChannels(ctx, userID, entityPairID)
	return err
}

// SubscribePairAlerts is the follow hook: following a pair subscribes it.
func (service *Service) SubscribePairAlerts(ctx context.Context, userID, entityPairID uuid.UUID) error {
	return service.SetPairAlerts(ctx, userID, entityPairID, true)
}

// UnsubscribePairAlerts is the unfollow hook: unfollowing mutes the pair.
func (service *Service) UnsubscribePairAlerts(ctx context.Context, userID, entityPairID uuid.UUID) error {
	return service.SetPairAlerts(ctx, userID, entityPairID, false)
}
