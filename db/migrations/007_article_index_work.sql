-- Index discovery finds articles with no queued article_index row.

CREATE INDEX outbox_article_index_lookup_idx
    ON outbox ((payload->>'article_id'))
    WHERE type = 'article_index';

---- create above / drop below ----

DROP INDEX outbox_article_index_lookup_idx;
