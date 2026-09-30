-- Baseline schema for the §8 data model.

CREATE TABLE users (
    id uuid PRIMARY KEY,
    username text NOT NULL,
    authlier_subject_id text NOT NULL UNIQUE,
    role text NOT NULL DEFAULT 'user',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_username_not_blank CHECK (length(btrim(username)) > 0),
    CONSTRAINT users_authlier_subject_id_not_blank CHECK (length(btrim(authlier_subject_id)) > 0),
    CONSTRAINT users_role_valid CHECK (role IN ('admin', 'user'))
);

CREATE TABLE entities (
    id uuid PRIMARY KEY,
    code text NOT NULL UNIQUE,
    name text NOT NULL UNIQUE,
    type text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT entities_code_not_blank CHECK (length(btrim(code)) > 0),
    CONSTRAINT entities_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT entities_type_not_blank CHECK (length(btrim(type)) > 0)
);

CREATE TABLE entity_pairs (
    id uuid PRIMARY KEY,
    base_entity_id uuid NOT NULL REFERENCES entities (id),
    quote_entity_id uuid NOT NULL REFERENCES entities (id),
    symbol text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT entity_pairs_symbol_not_blank CHECK (length(btrim(symbol)) > 0),
    CONSTRAINT entity_pairs_distinct_entities CHECK (base_entity_id <> quote_entity_id)
);

CREATE INDEX entity_pairs_base_entity_id_idx ON entity_pairs (base_entity_id);
CREATE INDEX entity_pairs_quote_entity_id_idx ON entity_pairs (quote_entity_id);

CREATE TABLE user_assets (
    user_id uuid NOT NULL REFERENCES users (id),
    entity_pair_id uuid NOT NULL REFERENCES entity_pairs (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, entity_pair_id)
);

CREATE INDEX user_assets_entity_pair_id_idx ON user_assets (entity_pair_id);

CREATE TABLE sources (
    id uuid PRIMARY KEY,
    name text NOT NULL UNIQUE,
    type text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sources_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT sources_type_not_blank CHECK (length(btrim(type)) > 0)
);

CREATE TABLE source_configurations (
    id uuid PRIMARY KEY,
    source_id uuid NOT NULL REFERENCES sources (id),
    type text NOT NULL,
    config jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT source_configurations_type_valid CHECK (type IN ('api', 'rss', 'web')),
    CONSTRAINT source_configurations_config_object CHECK (jsonb_typeof(config) = 'object')
);

CREATE INDEX source_configurations_source_id_idx ON source_configurations (source_id);

CREATE TABLE articles (
    id uuid PRIMARY KEY,
    source_id uuid NOT NULL REFERENCES sources (id),
    title text NOT NULL,
    content text,
    url text NOT NULL,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT articles_title_not_blank CHECK (length(btrim(title)) > 0),
    CONSTRAINT articles_url_not_blank CHECK (length(btrim(url)) > 0)
);

CREATE INDEX articles_source_id_idx ON articles (source_id);
CREATE INDEX articles_url_idx ON articles (url);

CREATE TABLE unmapped_articles (
    id uuid PRIMARY KEY,
    article_id uuid NOT NULL UNIQUE REFERENCES articles (id),
    status text NOT NULL DEFAULT 'pending',
    attempts integer NOT NULL DEFAULT 0,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT unmapped_articles_attempts_nonnegative CHECK (attempts >= 0)
);

CREATE INDEX unmapped_articles_pending_idx
    ON unmapped_articles (created_at, id)
    WHERE status = 'pending';

CREATE TABLE economic_indicators (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    entity_id uuid NOT NULL REFERENCES entities (id),
    type text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT economic_indicators_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT economic_indicators_type_not_blank CHECK (length(btrim(type)) > 0),
    UNIQUE (name, entity_id)
);

CREATE INDEX economic_indicators_entity_id_idx ON economic_indicators (entity_id);

CREATE TABLE calendar_events (
    id uuid PRIMARY KEY,
    source_id uuid NOT NULL REFERENCES sources (id),
    indicator_id uuid NOT NULL REFERENCES economic_indicators (id),
    scheduled_at timestamptz NOT NULL,
    released_at timestamptz,
    previous numeric,
    consensus numeric,
    actual numeric,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_id, indicator_id, scheduled_at)
);

CREATE INDEX calendar_events_scheduled_at_idx ON calendar_events (scheduled_at);
CREATE INDEX calendar_events_indicator_id_idx ON calendar_events (indicator_id);

CREATE TABLE market_candles (
    id uuid PRIMARY KEY,
    source_id uuid NOT NULL REFERENCES sources (id),
    entity_pair_id uuid NOT NULL REFERENCES entity_pairs (id),
    timeframe text NOT NULL,
    timestamp timestamptz NOT NULL,
    open numeric NOT NULL,
    high numeric NOT NULL,
    low numeric NOT NULL,
    close numeric NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT market_candles_timeframe_not_blank CHECK (length(btrim(timeframe)) > 0),
    CONSTRAINT market_candles_high_gte_low CHECK (high >= low),
    UNIQUE (source_id, entity_pair_id, timeframe, timestamp)
);

CREATE INDEX market_candles_pair_timeframe_timestamp_idx
    ON market_candles (entity_pair_id, timeframe, timestamp DESC);

CREATE TABLE article_entities (
    article_id uuid NOT NULL REFERENCES articles (id),
    entity_id uuid NOT NULL REFERENCES entities (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (article_id, entity_id)
);

CREATE INDEX article_entities_entity_id_idx ON article_entities (entity_id);

CREATE TABLE calendar_event_entities (
    calendar_event_id uuid NOT NULL REFERENCES calendar_events (id),
    entity_id uuid NOT NULL REFERENCES entities (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (calendar_event_id, entity_id)
);

CREATE INDEX calendar_event_entities_entity_id_idx ON calendar_event_entities (entity_id);

CREATE TABLE article_enrichments (
    id uuid PRIMARY KEY,
    article_id uuid NOT NULL REFERENCES articles (id),
    model text NOT NULL,
    result jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT article_enrichments_model_not_blank CHECK (length(btrim(model)) > 0),
    CONSTRAINT article_enrichments_result_object CHECK (jsonb_typeof(result) = 'object')
);

CREATE INDEX article_enrichments_article_id_idx ON article_enrichments (article_id);

CREATE TABLE story_clusters (
    id uuid PRIMARY KEY,
    title text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT story_clusters_title_not_blank CHECK (length(btrim(title)) > 0)
);

CREATE TABLE article_story_clusters (
    article_id uuid NOT NULL REFERENCES articles (id),
    story_cluster_id uuid NOT NULL REFERENCES story_clusters (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (article_id, story_cluster_id)
);

CREATE INDEX article_story_clusters_story_cluster_id_idx ON article_story_clusters (story_cluster_id);

CREATE TABLE user_alerts (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id),
    type text NOT NULL,
    entity_pair_id uuid NOT NULL REFERENCES entity_pairs (id),
    channel text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_alerts_type_not_blank CHECK (length(btrim(type)) > 0),
    CONSTRAINT user_alerts_channel_valid CHECK (channel IN ('in-app', 'discord', 'slack')),
    UNIQUE (user_id, type, entity_pair_id, channel)
);

CREATE INDEX user_alerts_entity_pair_id_idx ON user_alerts (entity_pair_id);

CREATE TABLE user_notification_channels (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id),
    channel text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_notification_channels_channel_valid CHECK (channel IN ('in-app', 'discord', 'slack')),
    UNIQUE (user_id, channel)
);

CREATE TABLE knowledge_terms (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    type text NOT NULL,
    entity_id uuid REFERENCES entities (id),
    indicator_id uuid REFERENCES economic_indicators (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT knowledge_terms_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT knowledge_terms_type_not_blank CHECK (length(btrim(type)) > 0),
    UNIQUE (name, type)
);

CREATE INDEX knowledge_terms_entity_id_idx ON knowledge_terms (entity_id);
CREATE INDEX knowledge_terms_indicator_id_idx ON knowledge_terms (indicator_id);

CREATE TABLE outbox (
    id uuid PRIMARY KEY,
    type text NOT NULL,
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    available_at timestamptz NOT NULL DEFAULT now(),
    attempts integer NOT NULL DEFAULT 0,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT outbox_type_not_blank CHECK (length(btrim(type)) > 0),
    CONSTRAINT outbox_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT outbox_attempts_nonnegative CHECK (attempts >= 0)
);

CREATE INDEX outbox_pending_idx
    ON outbox (available_at, id)
    WHERE status = 'pending';

CREATE OR REPLACE FUNCTION notify_outbox() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('macro_terminal_outbox', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER outbox_notify_after_insert
AFTER INSERT ON outbox
FOR EACH ROW EXECUTE FUNCTION notify_outbox();

---- create above / drop below ----

DROP TRIGGER outbox_notify_after_insert ON outbox;
DROP FUNCTION notify_outbox();
DROP TABLE outbox;
DROP TABLE knowledge_terms;
DROP TABLE user_notification_channels;
DROP TABLE user_alerts;
DROP TABLE article_story_clusters;
DROP TABLE story_clusters;
DROP TABLE article_enrichments;
DROP TABLE calendar_event_entities;
DROP TABLE article_entities;
DROP TABLE market_candles;
DROP TABLE calendar_events;
DROP TABLE economic_indicators;
DROP TABLE unmapped_articles;
DROP TABLE articles;
DROP TABLE source_configurations;
DROP TABLE sources;
DROP TABLE user_assets;
DROP TABLE entity_pairs;
DROP TABLE entities;
DROP TABLE users;
