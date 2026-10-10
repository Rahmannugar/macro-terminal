-- name: UpsertCalendarEvent :one
INSERT INTO calendar_events (id, source_id, indicator_id, scheduled_at, released_at, previous, consensus, actual, name, country_code, currency, importance)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (source_id, indicator_id, country_code, scheduled_at) DO UPDATE
SET released_at = COALESCE(calendar_events.released_at, EXCLUDED.released_at),
    previous = COALESCE(EXCLUDED.previous, calendar_events.previous),
    consensus = COALESCE(EXCLUDED.consensus, calendar_events.consensus),
    actual = COALESCE(EXCLUDED.actual, calendar_events.actual),
    name = COALESCE(EXCLUDED.name, calendar_events.name),
    country_code = COALESCE(EXCLUDED.country_code, calendar_events.country_code),
    currency = COALESCE(EXCLUDED.currency, calendar_events.currency),
    importance = COALESCE(EXCLUDED.importance, calendar_events.importance),
    revision = calendar_events.revision + CASE
        WHEN calendar_events.released_at IS NOT NULL
         AND (COALESCE(EXCLUDED.previous, calendar_events.previous) IS DISTINCT FROM calendar_events.previous
           OR COALESCE(EXCLUDED.consensus, calendar_events.consensus) IS DISTINCT FROM calendar_events.consensus
           OR COALESCE(EXCLUDED.actual, calendar_events.actual) IS DISTINCT FROM calendar_events.actual)
        THEN 1
        ELSE 0
    END,
    updated_at = now()
RETURNING *;

-- name: UpsertCalendarEventEntity :execrows
INSERT INTO calendar_event_entities (calendar_event_id, entity_id)
VALUES ($1, $2)
ON CONFLICT (calendar_event_id, entity_id) DO NOTHING;

-- name: GetCalendarEvent :one
SELECT ce.id, ce.source_id, ce.indicator_id, ce.scheduled_at, ce.released_at,
       ce.previous, ce.consensus, ce.actual, COALESCE(ce.name, ei.name) AS name,
       ei.name AS indicator_name, ei.type AS indicator_type
FROM calendar_events AS ce
JOIN economic_indicators AS ei ON ei.id = ce.indicator_id
WHERE ce.id = $1;

-- name: ListCalendarEventsPage :many
SELECT *
FROM calendar_events
WHERE (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) < (
        sqlc.narg('cursor_created_at')::timestamptz,
        sqlc.narg('cursor_id')::uuid
    )
)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: CreateCalendarEvent :one
INSERT INTO calendar_events (id, source_id, indicator_id, name, scheduled_at, released_at, previous, consensus, actual)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateCalendarEvent :one
UPDATE calendar_events
SET name = sqlc.arg('name'),
    scheduled_at = sqlc.arg('scheduled_at'),
    released_at = sqlc.arg('released_at'),
    previous = sqlc.arg('previous'),
    consensus = sqlc.arg('consensus'),
    actual = sqlc.arg('actual'),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ArchiveCalendarEvent :execrows
UPDATE calendar_events
SET archived_at = now()
WHERE id = $1 AND archived_at IS NULL;

-- name: RestoreCalendarEvent :execrows
UPDATE calendar_events
SET archived_at = NULL
WHERE id = $1 AND archived_at IS NOT NULL;

-- name: ListUpcomingCalendarEventsPage :many
SELECT ce.id, ce.source_id, ce.indicator_id, ce.scheduled_at, ce.released_at,
       ce.previous, ce.consensus, ce.actual, ce.country_code, ce.currency,
       ce.importance, ce.revision, ce.created_at, ce.updated_at,
       COALESCE(ce.name, ei.name) AS name,
       ei.name AS indicator_name, s.name AS source_name
FROM calendar_events AS ce
JOIN economic_indicators AS ei ON ei.id = ce.indicator_id
JOIN sources AS s ON s.id = ce.source_id
WHERE ce.scheduled_at >= sqlc.arg('not_before')
  AND (sqlc.narg('not_after')::timestamptz IS NULL
       OR ce.scheduled_at < sqlc.narg('not_after')::timestamptz)
  AND ce.archived_at IS NULL
  AND ce.id = (
      SELECT peer.id
      FROM calendar_events AS peer
      WHERE peer.indicator_id = ce.indicator_id
        AND peer.scheduled_at = ce.scheduled_at
      ORDER BY peer.id ASC
      LIMIT 1
  )
  AND (sqlc.narg('countries')::text IS NULL
       OR ce.country_code = ANY(string_to_array(sqlc.narg('countries')::text, ',')))
  AND (sqlc.narg('importances')::text IS NULL
       OR ce.importance = ANY(string_to_array(sqlc.narg('importances')::text, ',')))
  AND (sqlc.narg('watcher')::uuid IS NULL OR EXISTS (
      SELECT 1
      FROM calendar_event_entities AS link
      JOIN user_assets AS asset ON asset.user_id = sqlc.narg('watcher')::uuid
      JOIN entity_pairs AS pair ON pair.id = asset.entity_pair_id
      WHERE link.calendar_event_id = ce.id
        AND (link.entity_id = pair.base_entity_id OR link.entity_id = pair.quote_entity_id)
  ))
  AND (sqlc.narg('cursor_scheduled_at')::timestamptz IS NULL
       OR (ce.scheduled_at, ce.id) > (
           sqlc.narg('cursor_scheduled_at')::timestamptz,
           sqlc.narg('cursor_id')::uuid
       ))
ORDER BY ce.scheduled_at ASC, ce.id ASC
LIMIT sqlc.arg('page_size');

-- name: ListReleasedCalendarEventsPage :many
SELECT ce.id, ce.source_id, ce.indicator_id, ce.scheduled_at, ce.released_at,
       ce.previous, ce.consensus, ce.actual, ce.country_code, ce.currency,
       ce.importance, ce.revision, ce.created_at, ce.updated_at,
       COALESCE(ce.name, ei.name) AS name,
       ei.name AS indicator_name, s.name AS source_name
FROM calendar_events AS ce
JOIN economic_indicators AS ei ON ei.id = ce.indicator_id
JOIN sources AS s ON s.id = ce.source_id
WHERE ce.scheduled_at < sqlc.arg('not_after')
  AND (sqlc.narg('not_before')::timestamptz IS NULL
       OR ce.scheduled_at >= sqlc.narg('not_before')::timestamptz)
  AND ce.archived_at IS NULL
  AND ce.id = (
      SELECT peer.id
      FROM calendar_events AS peer
      WHERE peer.indicator_id = ce.indicator_id
        AND peer.scheduled_at = ce.scheduled_at
      ORDER BY peer.id ASC
      LIMIT 1
  )
  AND (sqlc.narg('countries')::text IS NULL
       OR ce.country_code = ANY(string_to_array(sqlc.narg('countries')::text, ',')))
  AND (sqlc.narg('importances')::text IS NULL
       OR ce.importance = ANY(string_to_array(sqlc.narg('importances')::text, ',')))
  AND (sqlc.narg('watcher')::uuid IS NULL OR EXISTS (
      SELECT 1
      FROM calendar_event_entities AS link
      JOIN user_assets AS asset ON asset.user_id = sqlc.narg('watcher')::uuid
      JOIN entity_pairs AS pair ON pair.id = asset.entity_pair_id
      WHERE link.calendar_event_id = ce.id
        AND (link.entity_id = pair.base_entity_id OR link.entity_id = pair.quote_entity_id)
  ))
  AND (sqlc.narg('cursor_scheduled_at')::timestamptz IS NULL
       OR (ce.scheduled_at, ce.id) < (
           sqlc.narg('cursor_scheduled_at')::timestamptz,
           sqlc.narg('cursor_id')::uuid
       ))
ORDER BY ce.scheduled_at DESC, ce.id DESC
LIMIT sqlc.arg('page_size');
