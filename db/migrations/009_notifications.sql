-- In-app notification records deduplicated per user, alert type, pair, subject, and channel.

CREATE TABLE notifications (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id),
    type text NOT NULL,
    entity_pair_id uuid NOT NULL REFERENCES entity_pairs (id),
    subject_type text NOT NULL,
    subject_id uuid NOT NULL,
    channel text NOT NULL,
    title text NOT NULL,
    read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notifications_type_not_blank CHECK (length(btrim(type)) > 0),
    CONSTRAINT notifications_subject_type_valid CHECK (subject_type IN ('article', 'calendar_event')),
    CONSTRAINT notifications_channel_valid CHECK (channel IN ('in-app', 'discord', 'slack')),
    CONSTRAINT notifications_title_not_blank CHECK (length(btrim(title)) > 0),
    UNIQUE (user_id, type, entity_pair_id, subject_type, subject_id, channel)
);

CREATE INDEX notifications_user_created_idx ON notifications (user_id, created_at DESC);

-- Fan-out discovery finds subjects with no queued asset_notification row.

CREATE INDEX outbox_asset_notification_lookup_idx
    ON outbox ((payload->>'subject_type'), (payload->>'subject_id'))
    WHERE type = 'asset_notification';

---- create above / drop below ----

DROP INDEX outbox_asset_notification_lookup_idx;
DROP INDEX notifications_user_created_idx;
DROP TABLE notifications;
