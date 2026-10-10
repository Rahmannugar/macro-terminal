ALTER TABLE calendar_events
    ADD COLUMN name text,
    ADD COLUMN archived_at timestamptz;

UPDATE calendar_events
SET name = indicator.name
FROM economic_indicators AS indicator
WHERE indicator.id = calendar_events.indicator_id
  AND calendar_events.name IS NULL;

UPDATE calendar_events
SET country_code = 'US',
    currency = 'USD'
WHERE country_code IS NULL;

DELETE FROM calendar_event_entities;
DELETE FROM calendar_events;

ALTER TABLE calendar_events
    DROP CONSTRAINT calendar_events_source_id_indicator_id_scheduled_at_key;

ALTER TABLE calendar_events
    ADD CONSTRAINT calendar_events_source_id_indicator_id_country_code_scheduled_at_key
    UNIQUE (source_id, indicator_id, country_code, scheduled_at);

---- create above / drop below ----

ALTER TABLE calendar_events
    DROP CONSTRAINT calendar_events_source_id_indicator_id_country_code_scheduled_at_key;

ALTER TABLE calendar_events
    ADD CONSTRAINT calendar_events_source_id_indicator_id_scheduled_at_key
    UNIQUE (source_id, indicator_id, scheduled_at);

ALTER TABLE calendar_events
    DROP COLUMN name,
    DROP COLUMN archived_at;
