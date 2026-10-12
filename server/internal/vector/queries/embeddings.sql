-- name: UpsertEmbedding :exec
INSERT INTO article_embeddings (article_id, embedding, published_at)
VALUES ($1, $2, $3)
ON CONFLICT (article_id) DO UPDATE
SET embedding = EXCLUDED.embedding,
    published_at = EXCLUDED.published_at,
    created_at = now();

-- name: AllEmbeddings :many
SELECT article_id, embedding, published_at
FROM article_embeddings;

-- name: DeleteEmbedding :exec
DELETE FROM article_embeddings
WHERE article_id = $1;
