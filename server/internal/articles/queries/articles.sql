-- name: UpsertArticle :one
INSERT INTO articles (id, source_id, title, content, url, published_at, image_url)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (source_id, url) DO UPDATE
SET title = EXCLUDED.title,
    content = CASE
                  WHEN EXCLUDED.content IS NULL OR btrim(EXCLUDED.content) = '' THEN articles.content
                  ELSE EXCLUDED.content
        END,
    image_url = COALESCE(EXCLUDED.image_url, articles.image_url),
    published_at = COALESCE(EXCLUDED.published_at, articles.published_at),
    updated_at = now()
RETURNING *;

-- name: UpsertArticleEntity :exec
INSERT INTO article_entities (article_id, entity_id)
VALUES ($1, $2)
ON CONFLICT (article_id, entity_id) DO NOTHING;

-- name: UpsertUnmappedArticle :execrows
INSERT INTO unmapped_articles (id, article_id, status)
VALUES ($1, $2, 'pending')
ON CONFLICT (article_id) DO NOTHING;

-- name: ResolveUnmappedArticle :execrows
DELETE FROM unmapped_articles
WHERE article_id = $1
  AND status = 'pending';

-- name: GetArticlesByIDs :many
SELECT a.id, a.source_id, s.name AS source_name, a.title, a.content, a.url, a.image_url, a.published_at
FROM articles AS a
JOIN sources AS s ON s.id = a.source_id
WHERE a.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: GetRecentArticleIDsByEntities :many
SELECT DISTINCT a.id, a.published_at
FROM articles AS a
JOIN article_entities AS link ON link.article_id = a.id
WHERE link.entity_id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY a.published_at DESC NULLS LAST, a.id
LIMIT $1;

-- name: GetRecentArticleIDsPage :many
SELECT id, COALESCE(published_at, created_at) AS sort_at
FROM articles
WHERE (
    sqlc.narg('cursor_at')::timestamptz IS NULL
    OR (COALESCE(published_at, created_at), id) < (
        sqlc.narg('cursor_at')::timestamptz,
        sqlc.narg('cursor_id')::uuid
    )
)
ORDER BY COALESCE(published_at, created_at) DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: GetRecentArticleIDsByEntitiesPage :many
SELECT DISTINCT a.id, COALESCE(a.published_at, a.created_at) AS sort_at
FROM articles AS a
JOIN article_entities AS link ON link.article_id = a.id
WHERE link.entity_id = ANY(sqlc.arg(ids)::uuid[])
  AND (
    sqlc.narg('cursor_at')::timestamptz IS NULL
    OR (COALESCE(a.published_at, a.created_at), a.id) < (
        sqlc.narg('cursor_at')::timestamptz,
        sqlc.narg('cursor_id')::uuid
    )
  )
ORDER BY COALESCE(a.published_at, a.created_at) DESC, a.id DESC
LIMIT sqlc.arg('page_size');
