-- Per-channel webhook endpoints for Slack/Discord delivery. The URL is a
-- secret: encrypted with a purpose-derived AEAD key, stored with a masked
-- hint for the UI. In-app rows carry no webhook columns.

ALTER TABLE user_notification_channels
    ADD COLUMN webhook_url_encrypted bytea,
    ADD COLUMN webhook_url_nonce bytea,
    ADD COLUMN webhook_url_key_version integer NOT NULL DEFAULT 1,
    ADD COLUMN webhook_hint text,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

CREATE INDEX outbox_webhook_delivery_lookup_idx
    ON outbox ((payload->>'notification_id'))
    WHERE type = 'webhook_delivery';

-- Follow = subscribe: existing watch-list follows become alert opt-ins and
-- their users get the in-app channel. Already-queued subjects are not
-- re-fanned-out, so nobody is notified about history.

INSERT INTO user_notification_channels (id, user_id, channel)
SELECT gen_random_uuid(), followed.user_id, 'in-app'
FROM (SELECT DISTINCT user_id FROM user_assets) AS followed
ON CONFLICT DO NOTHING;

INSERT INTO user_alerts (id, user_id, type, entity_pair_id, channel)
SELECT gen_random_uuid(), followed.user_id, alert_type.type, followed.entity_pair_id, 'in-app'
FROM user_assets AS followed
CROSS JOIN (VALUES ('new_article'), ('new_calendar_event')) AS alert_type (type)
ON CONFLICT DO NOTHING;

---- create above / drop below ----

DROP INDEX outbox_webhook_delivery_lookup_idx;

ALTER TABLE user_notification_channels
    DROP COLUMN webhook_hint,
    DROP COLUMN webhook_url_key_version,
    DROP COLUMN webhook_url_nonce,
    DROP COLUMN webhook_url_encrypted,
    DROP COLUMN updated_at;
