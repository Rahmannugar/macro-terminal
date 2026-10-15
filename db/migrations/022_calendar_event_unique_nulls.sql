-- The event upsert matches on source, indicator, country, and schedule.
-- A plain unique index treats NULL country codes as distinct, so rows
-- without a country duplicated themselves on every repeat delivery.

ALTER TABLE calendar_events
    DROP CONSTRAINT calendar_events_source_id_indicator_id_country_code_scheduled_at_key;

ALTER TABLE calendar_events
    ADD CONSTRAINT calendar_events_source_id_indicator_id_country_code_scheduled_at_key
    UNIQUE NULLS NOT DISTINCT (source_id, indicator_id, country_code, scheduled_at);

---- create above / drop below ----

ALTER TABLE calendar_events
    DROP CONSTRAINT calendar_events_source_id_indicator_id_country_code_scheduled_at_key;

ALTER TABLE calendar_events
    ADD CONSTRAINT calendar_events_source_id_indicator_id_country_code_scheduled_at_key
    UNIQUE (source_id, indicator_id, country_code, scheduled_at);
