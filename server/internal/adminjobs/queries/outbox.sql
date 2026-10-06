-- name: ListFailedOutboxJobs :many
SELECT id, type, status, attempts, last_error, created_at, updated_at
FROM outbox
WHERE status = 'failed'
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type')::text)
  AND (
      sqlc.narg('cursor_updated_at')::timestamptz IS NULL
      OR (updated_at, id) < (
          sqlc.narg('cursor_updated_at')::timestamptz,
          sqlc.narg('cursor_id')::uuid
      )
  )
ORDER BY updated_at DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: GetOutboxJob :one
SELECT id, type, status, attempts, last_error, created_at, updated_at
FROM outbox
WHERE id = $1;

-- name: ReplayOutboxJob :one
UPDATE outbox
SET status = 'pending', attempts = 0, available_at = now(), last_error = NULL, updated_at = now()
WHERE id = $1 AND status = 'failed'
RETURNING id, type, status, attempts, last_error, created_at, updated_at;
