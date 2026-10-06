CREATE TABLE email_deliveries (
    id uuid PRIMARY KEY,
    template text NOT NULL,
    encrypted_payload bytea NOT NULL,
    payload_nonce bytea NOT NULL,
    payload_key_version integer NOT NULL DEFAULT 1,
    expires_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    delivered_at timestamptz,
    provider_message_id text,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT email_deliveries_template_not_blank CHECK (length(btrim(template)) > 0),
    CONSTRAINT email_deliveries_status_known CHECK (status IN ('pending', 'delivered', 'failed', 'expired'))
);

CREATE INDEX email_deliveries_created_idx
    ON email_deliveries (created_at)
    WHERE status = 'pending';

---- create above / drop below ----

DROP TABLE email_deliveries;
