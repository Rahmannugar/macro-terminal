-- Webhook delivery drives the outbox like every other async job: claim with
-- backoff, reclaim stale claims, permanent failure after max attempts.

-- name: EnqueueWebhookDeliveries :execrows
INSERT INTO outbox (id, type, payload)
SELECT gen_random_uuid(),
       'webhook_delivery',
       jsonb_build_object('notification_id', notification.id::text)
FROM notifications AS notification
WHERE notification.channel IN ('slack', 'discord')
  AND NOT EXISTS (
        SELECT 1
        FROM outbox AS queued
        WHERE queued.type = 'webhook_delivery'
          AND queued.payload->>'notification_id' = notification.id::text
    )
ORDER BY notification.created_at
LIMIT $1;

-- name: ReclaimStaleWebhookJobs :execrows
UPDATE outbox
SET status      = 'pending',
    available_at = now(),
    updated_at  = now()
WHERE type = 'webhook_delivery'
  AND status = 'processing'
  AND updated_at < now() - interval '10 minutes';

-- name: ClaimWebhookBatch :many
WITH due AS (
    SELECT id
    FROM outbox
    WHERE type = 'webhook_delivery'
      AND status = 'pending'
      AND available_at <= now()
    ORDER BY available_at, id
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE outbox
SET status     = 'processing',
    attempts   = outbox.attempts + 1,
    updated_at = now()
FROM due
WHERE outbox.id = due.id
RETURNING outbox.id,
          (outbox.payload->>'notification_id')::uuid AS notification_id,
          outbox.attempts;

-- name: CompleteWebhookJob :exec
UPDATE outbox
SET status     = 'done',
    updated_at = now()
WHERE id = $1
  AND status = 'processing';

-- name: FailWebhookJob :exec
UPDATE outbox
SET status = CASE
                 WHEN attempts >= $2 THEN 'failed'
                 ELSE 'pending'
    END,
    available_at = CASE
                       WHEN attempts >= $2 THEN available_at
                       ELSE now() + make_interval(secs => LEAST(30 * power(2, attempts - 1), 3600))
        END,
    last_error  = $3,
    updated_at  = now()
WHERE id = $1
  AND status = 'processing';

-- name: FailWebhookJobPermanently :exec
UPDATE outbox
SET status     = 'failed',
    last_error = $2,
    updated_at = now()
WHERE id = $1
  AND status = 'processing';

-- A missing channel row means the user disconnected between enqueue and
-- delivery; the caller completes the job without POSTing. A missing
-- notification means the row vanished, which never happens in normal
-- operation and fails permanently.

-- name: GetWebhookDelivery :one
SELECT n.title,
       n.type,
       p.symbol AS pair_symbol,
       n.subject_type,
       n.subject_id,
       a.image_url,
       c.channel,
       c.webhook_url_nonce,
       c.webhook_url_encrypted
FROM notifications AS n
JOIN entity_pairs AS p ON p.id = n.entity_pair_id
LEFT JOIN articles AS a
    ON n.subject_type = 'article'
   AND a.id = n.subject_id
LEFT JOIN user_notification_channels AS c
    ON c.user_id = n.user_id
   AND c.channel = n.channel
WHERE n.id = $1;
