-- name: UpsertArticle :one
INSERT INTO articles (id, source_id, title, content, url, published_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (source_id, url) DO UPDATE
SET title = EXCLUDED.title,
    content = CASE
                  WHEN EXCLUDED.content IS NULL OR btrim(EXCLUDED.content) = '' THEN articles.content
                  ELSE EXCLUDED.content
        END,
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
SELECT a.id, a.source_id, s.name AS source_name, a.title, a.content, a.url, a.published_at
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
