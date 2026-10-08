-- Backfilled windows the provider answered with no candles, so gap
-- detection stops re-fetching market-closed periods every cycle.

CREATE TABLE market_backfill_checks (
    id uuid PRIMARY KEY,
    entity_pair_id uuid NOT NULL REFERENCES entity_pairs (id),
    timeframe text NOT NULL,
    checked_from timestamptz NOT NULL,
    checked_to timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT market_backfill_checks_timeframe_not_blank CHECK (length(btrim(timeframe)) > 0),
    CONSTRAINT market_backfill_checks_ordered CHECK (checked_to > checked_from)
);

CREATE INDEX market_backfill_checks_pair_timeframe_idx
    ON market_backfill_checks (entity_pair_id, timeframe, checked_from);

---- create above / drop below ----

DROP INDEX market_backfill_checks_pair_timeframe_idx;
DROP TABLE market_backfill_checks;
