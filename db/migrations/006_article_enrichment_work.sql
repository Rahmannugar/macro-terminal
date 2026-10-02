-- One enrichment per article; discovery finds articles with no queued or stored row.

DROP INDEX article_enrichments_article_id_idx;

CREATE UNIQUE INDEX article_enrichments_article_id_idx ON article_enrichments (article_id);

CREATE INDEX outbox_article_enrichment_lookup_idx
    ON outbox ((payload->>'article_id'))
    WHERE type = 'article_enrichment';

---- create above / drop below ----

DROP INDEX outbox_article_enrichment_lookup_idx;

DROP INDEX article_enrichments_article_id_idx;

CREATE INDEX article_enrichments_article_id_idx ON article_enrichments (article_id);
