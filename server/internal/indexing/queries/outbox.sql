-- name: EnqueueMissingIndexJobs :execrows
INSERT INTO outbox (id, type, payload)
SELECT gen_random_uuid(),
       'article_index',
       jsonb_build_object('article_id', article.id::text)
FROM articles AS article
WHERE EXISTS (
        SELECT 1
        FROM article_enrichments AS enrichment
        WHERE enrichment.article_id = article.id
    )
  AND NOT EXISTS (
        SELECT 1
        FROM outbox AS queued
        WHERE queued.type = 'article_index'
          AND queued.payload->>'article_id' = article.id::text
    )
ORDER BY article.created_at
LIMIT $1;

-- name: ReclaimStaleIndexJobs :execrows
UPDATE outbox
SET status = 'pending',
    available_at = now(),
    updated_at = now()
WHERE type = 'article_index'
  AND status = 'processing'
  AND updated_at < now() - interval '10 minutes';

-- name: ClaimIndexBatch :many
WITH due AS (
    SELECT id
    FROM outbox
    WHERE type = 'article_index'
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

-- name: CompleteOutboxJob :exec
UPDATE outbox
SET status     = 'done',
    updated_at = now()
WHERE id = $1
  AND status = 'processing';

-- name: FailOutboxJob :exec
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

-- name: FailOutboxJobPermanently :exec
UPDATE outbox
SET status     = 'failed',
    last_error = $2,
    updated_at = now()
WHERE id = $1
  AND status = 'processing';

-- name: GetArticleForIndexing :one
SELECT id, title, coalesce(content, '') AS content, published_at
FROM articles
WHERE id = $1;

-- name: ReopenIndexJobs :execrows
UPDATE outbox
SET status     = 'pending',
    attempts   = 0,
    available_at = now(),
    last_error  = NULL,
    updated_at  = now()
WHERE type = 'article_index'
  AND status IN ('done', 'failed');
