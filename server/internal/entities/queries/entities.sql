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
SELECT id, name, type, entity_id, created_at, updated_at
FROM knowledge_terms
WHERE entity_id IS NOT NULL
ORDER BY name, type;

-- name: UpsertKnowledgeTerm :one
INSERT INTO knowledge_terms (id, name, type, entity_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (name, type) DO UPDATE
SET entity_id = EXCLUDED.entity_id,
    updated_at = now()
RETURNING id, name, type, entity_id, created_at, updated_at;
