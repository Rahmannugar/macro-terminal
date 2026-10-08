-- name: UpsertCandle :exec
INSERT INTO market_candles (id, source_id, entity_pair_id, timeframe, timestamp, open, high, low, close)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (source_id, entity_pair_id, timeframe, timestamp) DO UPDATE
SET open = EXCLUDED.open,
    high = EXCLUDED.high,
    low = EXCLUDED.low,
    close = EXCLUDED.close;

-- name: ListCandlesPage :many
SELECT id, entity_pair_id, timeframe, timestamp, open, high, low, close
FROM market_candles
WHERE entity_pair_id = sqlc.arg('entity_pair_id')
  AND timeframe = sqlc.arg('timeframe')
  AND (
    sqlc.narg('cursor_timestamp')::timestamptz IS NULL
    OR (timestamp, id) < (
        sqlc.narg('cursor_timestamp')::timestamptz,
        sqlc.narg('cursor_id')::uuid
    )
  )
ORDER BY timestamp DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: CandleGaps :many
WITH series AS (
  SELECT timestamp
  FROM market_candles
  WHERE entity_pair_id = sqlc.arg('entity_pair_id')
    AND timeframe = sqlc.arg('timeframe')
    AND timestamp >= sqlc.arg('range_start')
    AND timestamp < sqlc.arg('range_end')
),
gaps AS (
  SELECT
    sqlc.arg('range_start')::timestamptz AS gap_start,
    min(timestamp) AS gap_end
  FROM series
  HAVING min(timestamp) > sqlc.arg('range_start')
      + sqlc.arg('min_gap_seconds')::double precision * interval '1 second'
  UNION ALL
  SELECT
    max(timestamp) AS gap_start,
    sqlc.arg('range_end')::timestamptz AS gap_end
  FROM series
  HAVING sqlc.arg('range_end')
      > max(timestamp) + sqlc.arg('min_gap_seconds')::double precision * interval '1 second'
  UNION ALL
  SELECT lag_ts AS gap_start, ts AS gap_end
  FROM (
    SELECT
      timestamp AS ts,
      lag(timestamp) OVER (ORDER BY timestamp) AS lag_ts
    FROM series
  ) windows
  WHERE lag_ts IS NOT NULL
    AND ts > lag_ts + sqlc.arg('min_gap_seconds')::double precision * interval '1 second'
  UNION ALL
  SELECT
    sqlc.arg('range_start')::timestamptz AS gap_start,
    sqlc.arg('range_end')::timestamptz AS gap_end
  WHERE NOT EXISTS (SELECT 1 FROM series)
)
SELECT gap_start::timestamptz AS gap_start, gap_end::timestamptz AS gap_end
FROM gaps
ORDER BY (gap_end - gap_start) DESC;

-- name: BackfillChecks :many
SELECT checked_from, checked_to
FROM market_backfill_checks
WHERE entity_pair_id = sqlc.arg('entity_pair_id')
  AND timeframe = sqlc.arg('timeframe')
  AND checked_to > sqlc.arg('range_start')
  AND checked_from < sqlc.arg('range_end')
ORDER BY checked_from;

-- name: RecordBackfillCheck :exec
INSERT INTO market_backfill_checks (id, entity_pair_id, timeframe, checked_from, checked_to)
VALUES ($1, $2, $3, $4, $5);
