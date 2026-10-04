-- name: UpsertCalendarEvent :one
INSERT INTO calendar_events (id, source_id, indicator_id, scheduled_at, released_at, previous, consensus, actual)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (source_id, indicator_id, scheduled_at) DO UPDATE
SET released_at = COALESCE(calendar_events.released_at, EXCLUDED.released_at),
    previous = COALESCE(EXCLUDED.previous, calendar_events.previous),
    consensus = COALESCE(EXCLUDED.consensus, calendar_events.consensus),
    actual = COALESCE(EXCLUDED.actual, calendar_events.actual),
    updated_at = now()
RETURNING *;

-- name: UpsertCalendarEventEntity :execrows
INSERT INTO calendar_event_entities (calendar_event_id, entity_id)
VALUES ($1, $2)
ON CONFLICT (calendar_event_id, entity_id) DO NOTHING;

-- name: GetCalendarEvent :one
SELECT ce.id, ce.source_id, ce.indicator_id, ce.scheduled_at, ce.released_at,
       ce.previous, ce.consensus, ce.actual,
       ei.name AS indicator_name, ei.type AS indicator_type
FROM calendar_events AS ce
JOIN economic_indicators AS ei ON ei.id = ce.indicator_id
WHERE ce.id = $1;
