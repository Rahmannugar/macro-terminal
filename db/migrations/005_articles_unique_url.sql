-- Idempotent article ingestion: one row per source and canonical URL.

DROP INDEX articles_source_id_idx;

CREATE UNIQUE INDEX articles_source_url_unique ON articles (source_id, url);

---- create above / drop below ----

DROP INDEX articles_source_url_unique;

CREATE INDEX articles_source_id_idx ON articles (source_id);
