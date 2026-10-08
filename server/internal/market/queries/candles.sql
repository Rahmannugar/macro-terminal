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
