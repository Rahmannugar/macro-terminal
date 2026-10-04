-- Enqueue runs only for subjects that are mapped to at least one entity.

-- name: EnqueueMissingArticleNotifications :execrows
INSERT INTO outbox (id, type, payload)
SELECT gen_random_uuid(),
       'asset_notification',
       jsonb_build_object('subject_type', 'article', 'subject_id', article.id::text)
FROM articles AS article
WHERE EXISTS (
        SELECT 1
        FROM article_entities AS link
        WHERE link.article_id = article.id
    )
  AND NOT EXISTS (
        SELECT 1
        FROM outbox AS queued
        WHERE queued.type = 'asset_notification'
          AND queued.payload->>'subject_type' = 'article'
          AND queued.payload->>'subject_id' = article.id::text
    )
ORDER BY article.created_at
LIMIT $1;

-- name: EnqueueMissingCalendarEventNotifications :execrows
INSERT INTO outbox (id, type, payload)
SELECT gen_random_uuid(),
       'asset_notification',
       jsonb_build_object('subject_type', 'calendar_event', 'subject_id', event.id::text)
FROM calendar_events AS event
WHERE EXISTS (
        SELECT 1
        FROM calendar_event_entities AS link
        WHERE link.calendar_event_id = event.id
    )
  AND NOT EXISTS (
        SELECT 1
        FROM outbox AS queued
        WHERE queued.type = 'asset_notification'
          AND queued.payload->>'subject_type' = 'calendar_event'
          AND queued.payload->>'subject_id' = event.id::text
    )
ORDER BY event.created_at
LIMIT $1;

-- name: ReclaimStaleNotificationJobs :execrows
UPDATE outbox
SET status     = 'pending',
    available_at = now(),
    updated_at = now()
WHERE type = 'asset_notification'
  AND status = 'processing'
  AND updated_at < now() - interval '10 minutes';

-- name: ClaimNotificationBatch :many
WITH due AS (
    SELECT id
    FROM outbox
    WHERE type = 'asset_notification'
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
          (outbox.payload->>'subject_type')::text AS subject_type,
          (outbox.payload->>'subject_id')::uuid AS subject_id,
          outbox.attempts;

-- name: CompleteNotificationJob :exec
UPDATE outbox
SET status     = 'done',
    updated_at = now()
WHERE id = $1
  AND status = 'processing';

-- name: FailNotificationJob :exec
UPDATE outbox
SET status = CASE
                 WHEN attempts >= $2 THEN 'failed'
                 ELSE 'pending'
    END,
    available_at = CASE
                       WHEN attempts >= $2 THEN available_at
                       ELSE now() + make_interval(secs => LEAST(30 * power(2, attempts - 1), 3600))
        END,
    last_error   = $3,
    updated_at   = now()
WHERE id = $1
  AND status = 'processing';

-- name: FailNotificationJobPermanently :exec
UPDATE outbox
SET status     = 'failed',
    last_error = $2,
    updated_at = now()
WHERE id = $1
  AND status = 'processing';

-- name: GetNotificationSubjectForArticle :one
SELECT id, title
FROM articles
WHERE id = $1;

-- name: GetNotificationSubjectForCalendarEvent :one
SELECT event.id, indicator.name AS title
FROM calendar_events AS event
JOIN economic_indicators AS indicator ON indicator.id = event.indicator_id
WHERE event.id = $1;

-- Fan-out targets only in-app alert opt-ins whose channel is enabled.

-- name: FanOutArticleNotifications :execrows
WITH affected AS (
    SELECT id
    FROM entity_pairs
    WHERE base_entity_id IN (SELECT entity_id FROM article_entities WHERE article_id = $1)
       OR quote_entity_id IN (SELECT entity_id FROM article_entities WHERE article_id = $1)
)
INSERT INTO notifications (id, user_id, type, entity_pair_id, subject_type, subject_id, channel, title)
SELECT gen_random_uuid(),
       alert.user_id,
       alert.type,
       alert.entity_pair_id,
       'article',
       $1,
       alert.channel,
       $2
FROM user_alerts AS alert
JOIN user_notification_channels AS configured
  ON configured.user_id = alert.user_id
 AND configured.channel = alert.channel
WHERE alert.type = 'new_article'
  AND alert.channel = 'in-app'
  AND alert.entity_pair_id IN (SELECT id FROM affected)
ON CONFLICT DO NOTHING;

-- name: FanOutCalendarEventNotifications :execrows
WITH affected AS (
    SELECT id
    FROM entity_pairs
    WHERE base_entity_id IN (SELECT entity_id FROM calendar_event_entities WHERE calendar_event_id = $1)
       OR quote_entity_id IN (SELECT entity_id FROM calendar_event_entities WHERE calendar_event_id = $1)
)
INSERT INTO notifications (id, user_id, type, entity_pair_id, subject_type, subject_id, channel, title)
SELECT gen_random_uuid(),
       alert.user_id,
       alert.type,
       alert.entity_pair_id,
       'calendar_event',
       $1,
       alert.channel,
       $2
FROM user_alerts AS alert
JOIN user_notification_channels AS configured
  ON configured.user_id = alert.user_id
 AND configured.channel = alert.channel
WHERE alert.type = 'new_calendar_event'
  AND alert.channel = 'in-app'
  AND alert.entity_pair_id IN (SELECT id FROM affected)
ON CONFLICT DO NOTHING;
