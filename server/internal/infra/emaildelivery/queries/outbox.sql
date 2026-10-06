-- name: ClaimEmailBatch :many
WITH due AS (
    SELECT id
    FROM outbox
    WHERE type = 'email_delivery'
      AND status = 'pending'
      AND available_at <= now()
    ORDER BY available_at, id
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE outbox
SET status     = 'processing',
    attempts   = outbox.attempts + 1,
    updated_at = now()
FROM due
WHERE outbox.id = due.id
RETURNING outbox.id,
          (outbox.payload->>'delivery_id')::uuid AS delivery_id,
          outbox.attempts;

-- name: ReclaimStaleEmailJobs :execrows
UPDATE outbox
SET status = 'pending',
    available_at = now(),
    updated_at = now()
WHERE type = 'email_delivery'
  AND status = 'processing'
  AND updated_at < now() - interval '10 minutes';

-- name: CompleteEmailJob :exec
UPDATE outbox
SET status     = 'done',
    updated_at = now()
WHERE id = $1
  AND status = 'processing';

-- name: FailEmailJob :exec
UPDATE outbox
SET status = CASE
                 WHEN attempts >= $2 THEN 'failed'
                 ELSE 'pending'
    END,
    available_at = CASE
                       WHEN attempts >= $2 THEN available_at
                       ELSE now() + make_interval(secs => GREATEST(LEAST(10 * power(2, attempts - 1), 300), sqlc.arg(retry_after_secs)::double precision))
        END,
    last_error   = $3,
    updated_at   = now()
WHERE id = $1
  AND status = 'processing';

-- name: FailEmailJobPermanently :exec
UPDATE outbox
SET status     = 'failed',
    last_error = $2,
    updated_at = now()
WHERE id = $1
  AND status = 'processing';

-- name: GetEmailDelivery :one
SELECT id, template, encrypted_payload, payload_nonce, expires_at, status
FROM email_deliveries
WHERE id = $1;

-- name: MarkEmailDeliveryDelivered :execrows
UPDATE email_deliveries
SET status              = 'delivered',
    delivered_at        = now(),
    provider_message_id = $2,
    encrypted_payload   = '\x00'::bytea,
    payload_nonce       = decode('000000000000000000000000', 'hex'),
    last_error          = NULL,
    updated_at          = now()
WHERE id = $1
  AND status = 'pending';

-- name: MarkEmailDeliveryTerminal :execrows
UPDATE email_deliveries
SET status              = $2,
    last_error          = $3,
    encrypted_payload   = '\x00'::bytea,
    payload_nonce       = decode('000000000000000000000000', 'hex'),
    updated_at          = now()
WHERE id = $1
  AND status = 'pending';

-- name: NoteEmailDeliveryError :exec
UPDATE email_deliveries
SET last_error = $2,
    updated_at = now()
WHERE id = $1;
