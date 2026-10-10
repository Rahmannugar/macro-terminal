ALTER TABLE calendar_events
    ADD COLUMN country_code text,
    ADD COLUMN currency text,
    ADD COLUMN importance text,
    ADD COLUMN revision integer NOT NULL DEFAULT 0;

-- Rows stored before these columns existed have no provider country. The
-- linked entity's currency is the best available stand-in so history can
-- still be filtered and flagged.
UPDATE calendar_events AS event
SET country_code = CASE linked.code
        WHEN 'USD' THEN 'US'
        WHEN 'EUR' THEN 'EU'
        WHEN 'JPY' THEN 'JP'
        WHEN 'GBP' THEN 'GB'
        WHEN 'CNY' THEN 'CN'
        WHEN 'CAD' THEN 'CA'
        WHEN 'AUD' THEN 'AU'
        WHEN 'CHF' THEN 'CH'
        WHEN 'NZD' THEN 'NZ'
        WHEN 'BRL' THEN 'BR'
        WHEN 'INR' THEN 'IN'
        WHEN 'MXN' THEN 'MX'
        WHEN 'ZAR' THEN 'ZA'
        WHEN 'KRW' THEN 'KR'
    END,
    currency = COALESCE(linked.code, event.currency)
FROM (
    SELECT DISTINCT ON (link.calendar_event_id)
           link.calendar_event_id, entity.code
    FROM calendar_event_entities AS link
    JOIN entities AS entity ON entity.id = link.entity_id
) AS linked
WHERE linked.calendar_event_id = event.id
  AND event.country_code IS NULL;

---- create above / drop below ----

ALTER TABLE calendar_events
    DROP COLUMN country_code,
    DROP COLUMN currency,
    DROP COLUMN importance,
    DROP COLUMN revision;
