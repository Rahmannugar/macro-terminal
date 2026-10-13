-- User-facing notification reads and the subscription settings that drive
-- fan-out. The in-app alert rows are the canonical per-pair subscription;
-- other channels mirror them when connected.

-- name: ListUserNotificationsPage :many
SELECT n.id,
       n.type,
       n.subject_type,
       n.subject_id,
       n.title,
       n.read_at,
       n.created_at,
       p.symbol AS pair_symbol
FROM notifications AS n
JOIN entity_pairs AS p ON p.id = n.entity_pair_id
WHERE n.user_id = $1
  AND n.channel = 'in-app'
  AND (
    sqlc.narg('cursor_at')::timestamptz IS NULL
    OR (n.created_at, n.id) < (
        sqlc.narg('cursor_at')::timestamptz,
        sqlc.narg('cursor_id')::uuid
    )
  )
ORDER BY n.created_at DESC, n.id DESC
LIMIT sqlc.arg('page_size');

-- name: CountUserUnreadNotifications :one
SELECT count(*)
FROM notifications
WHERE user_id = $1
  AND channel = 'in-app'
  AND read_at IS NULL;

-- name: MarkUserNotificationRead :execrows
UPDATE notifications
SET read_at = now()
WHERE id = $1
  AND user_id = $2
  AND channel = 'in-app'
  AND read_at IS NULL;

-- name: MarkAllUserNotificationsRead :execrows
UPDATE notifications
SET read_at = now()
WHERE user_id = $1
  AND channel = 'in-app'
  AND read_at IS NULL;

-- name: ListUserAlertPairIDs :many
SELECT DISTINCT entity_pair_id
FROM user_alerts
WHERE user_id = $1
  AND channel = 'in-app'
  AND type = 'new_article';

-- name: ListUserNotificationChannels :many
SELECT channel, webhook_hint
FROM user_notification_channels
WHERE user_id = $1
ORDER BY channel;

-- name: EnsureInAppChannel :exec
INSERT INTO user_notification_channels (id, user_id, channel)
VALUES (gen_random_uuid(), $1, 'in-app')
ON CONFLICT (user_id, channel) DO NOTHING;

-- name: UpsertWebhookChannel :exec
INSERT INTO user_notification_channels (
    id, user_id, channel,
    webhook_url_encrypted, webhook_url_nonce, webhook_url_key_version, webhook_hint,
    updated_at
)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, now())
ON CONFLICT (user_id, channel) DO UPDATE
SET webhook_url_encrypted = EXCLUDED.webhook_url_encrypted,
    webhook_url_nonce     = EXCLUDED.webhook_url_nonce,
    webhook_url_key_version = EXCLUDED.webhook_url_key_version,
    webhook_hint          = EXCLUDED.webhook_hint,
    updated_at            = now();

-- name: DeleteUserNotificationChannel :execrows
DELETE FROM user_notification_channels
WHERE user_id = $1
  AND channel = sqlc.arg(channel);

-- Follow, pair toggle on, and reconnect all reuse this: both alert types for
-- every channel the user currently has configured.

-- name: UpsertPairAlertsForConfiguredChannels :execrows
INSERT INTO user_alerts (id, user_id, type, entity_pair_id, channel)
SELECT gen_random_uuid(), $1, alert_type.type, $2, configured.channel
FROM (VALUES ('new_article'), ('new_calendar_event')) AS alert_type (type)
CROSS JOIN user_notification_channels AS configured
WHERE configured.user_id = $1
ON CONFLICT DO NOTHING;

-- name: DeletePairAlerts :execrows
DELETE FROM user_alerts
WHERE user_id = $1
  AND entity_pair_id = $2;

-- name: DeleteChannelAlerts :execrows
DELETE FROM user_alerts
WHERE user_id = $1
  AND channel = sqlc.arg(channel);

-- Connecting a webhook channel copies the canonical in-app set.

-- name: SyncChannelAlertsFromInApp :execrows
INSERT INTO user_alerts (id, user_id, type, entity_pair_id, channel)
SELECT gen_random_uuid(), canonical.user_id, canonical.type, canonical.entity_pair_id, $2
FROM user_alerts AS canonical
WHERE canonical.user_id = $1
  AND canonical.channel = 'in-app'
ON CONFLICT DO NOTHING;
