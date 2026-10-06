-- Content discovery fills articles a listing page produced without a body,
-- limited to sources configured with a content selector.

-- name: EnqueueMissingContentJobs :execrows
INSERT INTO outbox (id, type, payload)
SELECT gen_random_uuid(),
       'article_content',
       jsonb_build_object('article_id', article.id::text)
FROM articles AS article
WHERE (article.content IS NULL OR btrim(article.content) = '')
  AND EXISTS (
        SELECT 1
        FROM source_configurations AS configuration
        WHERE configuration.source_id = article.source_id
          AND configuration.type = 'web'
          AND configuration.config->'selectors'->>'content' IS NOT NULL
    )
  AND NOT EXISTS (
        SELECT 1
        FROM outbox AS queued
        WHERE queued.type = 'article_content'
          AND queued.payload->>'article_id' = article.id::text
    )
ORDER BY article.created_at
LIMIT $1;

-- name: ReclaimStaleContentJobs :execrows
UPDATE outbox
SET status       = 'pending',
    available_at = now(),
    updated_at   = now()
WHERE type = 'article_content'
  AND status = 'processing'
  AND updated_at < now() - interval '10 minutes';

-- name: ClaimContentBatch :many
WITH due AS (
    SELECT id
    FROM outbox
    WHERE type = 'article_content'
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
          (outbox.payload->>'article_id')::uuid AS article_id,
          outbox.attempts;

-- name: CompleteContentJob :exec
UPDATE outbox
SET status     = 'done',
    updated_at = now()
WHERE id = $1
  AND status = 'processing';

-- name: FailContentJob :exec
UPDATE outbox
SET status = CASE
                 WHEN attempts >= $2 THEN 'failed'
                 ELSE 'pending'
    END,
    available_at = CASE
                       WHEN attempts >= $2 THEN available_at
                       ELSE now() + make_interval(secs => LEAST(30 * power(2, attempts - 1), 3600))
        END,
    last_error = $3,
    updated_at = now()
WHERE id = $1
  AND status = 'processing';

-- name: FailContentJobPermanently :exec
UPDATE outbox
SET status     = 'failed',
    last_error = $2,
    updated_at = now()
WHERE id = $1
  AND status = 'processing';

-- name: GetContentTarget :one
SELECT article.id                    AS article_id,
       article.url                   AS article_url,
       article.source_id             AS source_id,
       configuration.id              AS configuration_id,
       configuration.type            AS configuration_type,
       configuration.config,
       source.name                   AS source_name,
       source.type                   AS source_type
FROM articles AS article
JOIN sources AS source ON source.id = article.source_id
JOIN source_configurations AS configuration
     ON configuration.source_id = article.source_id
    AND configuration.type = 'web'
WHERE article.id = $1
ORDER BY configuration.id
LIMIT 1;

-- Stored content never overwrites text a feed or an earlier fetch already
-- delivered.

-- name: StoreArticleContent :execrows
UPDATE articles
SET content    = $2,
    updated_at = now()
WHERE id = $1
  AND (content IS NULL OR btrim(content) = '');
