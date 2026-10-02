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
