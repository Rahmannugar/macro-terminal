-- name: ListEntities :many
SELECT *
FROM entities
ORDER BY code;

-- name: GetEntityByID :one
SELECT *
FROM entities
WHERE id = $1;

-- name: GetEntityByCode :one
SELECT *
FROM entities
WHERE code = $1;

-- name: CreateEntity :one
INSERT INTO entities (id, code, name, type)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpsertEntity :one
INSERT INTO entities (id, code, name, type)
VALUES ($1, $2, $3, $4)
ON CONFLICT (code) DO UPDATE
SET name = EXCLUDED.name,
    type = EXCLUDED.type,
    updated_at = now()
RETURNING *;

-- name: ListEntityPairs :many
SELECT *
FROM entity_pairs
ORDER BY symbol;

-- name: GetEntityPairByID :one
SELECT *
FROM entity_pairs
WHERE id = $1;

-- name: GetEntityPairBySymbol :one
SELECT *
FROM entity_pairs
WHERE symbol = $1;

-- name: ListEntityPairsContainingEntity :many
SELECT *
FROM entity_pairs
WHERE base_entity_id = $1
   OR quote_entity_id = $1
ORDER BY symbol;

-- name: CreateEntityPair :one
INSERT INTO entity_pairs (id, base_entity_id, quote_entity_id, symbol)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpsertEntityPair :one
INSERT INTO entity_pairs (id, base_entity_id, quote_entity_id, symbol)
VALUES ($1, $2, $3, $4)
ON CONFLICT (symbol) DO UPDATE
SET base_entity_id = EXCLUDED.base_entity_id,
    quote_entity_id = EXCLUDED.quote_entity_id,
    updated_at = now()
RETURNING *;

-- name: ListEntityPairsByUser :many
SELECT ep.*
FROM user_assets ua
JOIN entity_pairs ep ON ep.id = ua.entity_pair_id
WHERE ua.user_id = $1
ORDER BY ep.symbol;

-- name: ListUserIDsByEntityPair :many
SELECT ua.user_id
FROM user_assets ua
WHERE ua.entity_pair_id = $1
ORDER BY ua.user_id;

-- name: SubscribeUserAsset :exec
INSERT INTO user_assets (user_id, entity_pair_id)
VALUES ($1, $2)
ON CONFLICT (user_id, entity_pair_id) DO NOTHING;

-- name: UnsubscribeUserAsset :exec
DELETE FROM user_assets
WHERE user_id = $1
  AND entity_pair_id = $2;

-- name: ListEntityKnowledgeTerms :many
SELECT id, name, type, entity_id, indicator_id, created_at, updated_at
FROM knowledge_terms
WHERE entity_id IS NOT NULL
ORDER BY name, type;

-- name: UpsertKnowledgeTerm :one
INSERT INTO knowledge_terms (id, name, type, entity_id, indicator_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (name, type) DO UPDATE
SET entity_id = EXCLUDED.entity_id,
    indicator_id = COALESCE(EXCLUDED.indicator_id, knowledge_terms.indicator_id),
    updated_at = now()
RETURNING id, name, type, entity_id, indicator_id, created_at, updated_at;

-- name: GetEntitiesForArticle :many
SELECT e.id, e.code, e.name, e.type, e.created_at, e.updated_at
FROM article_entities AS link
JOIN entities AS e ON e.id = link.entity_id
WHERE link.article_id = $1
ORDER BY e.code;

-- name: GetEntitiesForCalendarEvent :many
SELECT e.id, e.code, e.name, e.type, e.created_at, e.updated_at
FROM calendar_event_entities AS link
JOIN entities AS e ON e.id = link.entity_id
WHERE link.calendar_event_id = $1
ORDER BY e.code;

-- name: ListKnowledgeTermsForEntities :many
SELECT id, name, type, entity_id, indicator_id, created_at, updated_at
FROM knowledge_terms
WHERE entity_id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY name, type;

-- name: ListIndicators :many
SELECT id, name, entity_id, type, created_at, updated_at
FROM economic_indicators
ORDER BY name;

-- name: ListIndicatorKnowledgeTerms :many
SELECT name, indicator_id
FROM knowledge_terms
WHERE indicator_id IS NOT NULL
ORDER BY name, type;

-- name: UpsertIndicator :one
INSERT INTO economic_indicators (id, name, entity_id, type)
VALUES ($1, $2, $3, $4)
ON CONFLICT (name, entity_id) DO UPDATE
SET type = EXCLUDED.type,
    updated_at = now()
RETURNING id, name, entity_id, type, created_at, updated_at;

-- name: ListEntitiesPage :many
SELECT *
FROM entities
WHERE (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) < (
        sqlc.narg('cursor_created_at')::timestamptz,
        sqlc.narg('cursor_id')::uuid
    )
)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: ListEntityPairsPage :many
SELECT *
FROM entity_pairs
WHERE (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) < (
        sqlc.narg('cursor_created_at')::timestamptz,
        sqlc.narg('cursor_id')::uuid
    )
)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: ListIndicatorsPage :many
SELECT *
FROM economic_indicators
WHERE (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) < (
        sqlc.narg('cursor_created_at')::timestamptz,
        sqlc.narg('cursor_id')::uuid
    )
)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: GetIndicatorByID :one
SELECT *
FROM economic_indicators
WHERE id = $1;

-- name: CreateIndicator :one
INSERT INTO economic_indicators (id, name, entity_id, type)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListKnowledgeTermsPage :many
SELECT *
FROM knowledge_terms
WHERE (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) < (
        sqlc.narg('cursor_created_at')::timestamptz,
        sqlc.narg('cursor_id')::uuid
    )
)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: CreateKnowledgeTerm :one
INSERT INTO knowledge_terms (id, name, type, entity_id, indicator_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;
