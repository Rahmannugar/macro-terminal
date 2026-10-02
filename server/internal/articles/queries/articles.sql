-- name: UpsertArticle :one
INSERT INTO articles (id, source_id, title, content, url, published_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (source_id, url) DO UPDATE
SET title = EXCLUDED.title,
    content = EXCLUDED.content,
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
