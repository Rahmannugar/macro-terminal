-- +goose Up
CREATE TABLE article_embeddings (
    article_id uuid PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
    embedding real[] NOT NULL,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX article_embeddings_published_at_idx ON article_embeddings (published_at DESC NULLS LAST);

-- +goose Down
DROP TABLE IF EXISTS article_embeddings;
