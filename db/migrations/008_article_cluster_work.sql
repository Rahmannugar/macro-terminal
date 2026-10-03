-- Cluster discovery finds articles with no queued article_cluster row.

CREATE INDEX outbox_article_cluster_lookup_idx
    ON outbox ((payload->>'article_id'))
    WHERE type = 'article_cluster';

---- create above / drop below ----

DROP INDEX outbox_article_cluster_lookup_idx;
