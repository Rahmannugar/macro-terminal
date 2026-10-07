-- name: ListSources :many
SELECT *
FROM sources
ORDER BY name;

-- name: GetSourceByID :one
SELECT *
FROM sources
WHERE id = $1;

-- name: GetSourceByName :one
SELECT *
FROM sources
WHERE name = $1;

-- name: UpsertSource :one
INSERT INTO sources (id, name, type)
VALUES ($1, $2, $3)
ON CONFLICT (name) DO UPDATE
SET type = EXCLUDED.type,
    updated_at = now()
RETURNING *;

-- name: ListSourceConfigurations :many
SELECT *
FROM source_configurations
WHERE source_id = $1
ORDER BY type, id;

-- name: GetSourceConfigurationByURL :one
SELECT *
FROM source_configurations
WHERE source_id = sqlc.arg(source_id)
  AND type = sqlc.arg(config_type)
  AND (config ->> 'url') = sqlc.arg(config_url)::text
LIMIT 1;

-- name: CreateSourceConfiguration :one
INSERT INTO source_configurations (id, source_id, type, config)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateSourceConfiguration :one
UPDATE source_configurations
SET config = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListSourceConfigurationsWithSource :many
SELECT sc.id,
       sc.source_id,
       sc.type,
       sc.config,
       sc.created_at,
       sc.updated_at,
       sc.last_run_at,
       s.name AS source_name,
       s.type AS source_type
FROM source_configurations sc
JOIN sources s ON s.id = sc.source_id
ORDER BY s.name, sc.type;

-- name: MarkSourceConfigurationsRun :exec
UPDATE source_configurations
SET last_run_at = sqlc.arg(run_at)
WHERE id = ANY(sqlc.slice(ids));

-- name: SourceConfigurationsByType :many
SELECT *
FROM source_configurations
WHERE source_id = sqlc.arg(source_id)
  AND type = sqlc.arg(config_type)
ORDER BY id;

-- name: ListSourcesPage :many
SELECT *
FROM sources
WHERE (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) < (
        sqlc.narg('cursor_created_at')::timestamptz,
        sqlc.narg('cursor_id')::uuid
    )
)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: CreateSource :one
INSERT INTO sources (id, name, type)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListSourceConfigurationsPage :many
SELECT *
FROM source_configurations
WHERE (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) < (
        sqlc.narg('cursor_created_at')::timestamptz,
        sqlc.narg('cursor_id')::uuid
    )
)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: GetSourceConfigurationByID :one
SELECT *
FROM source_configurations
WHERE id = $1;
