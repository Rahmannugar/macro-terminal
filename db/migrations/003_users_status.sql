-- Admin suspend/reactivate users.

ALTER TABLE users
    ADD COLUMN status text NOT NULL DEFAULT 'active',
    ADD CONSTRAINT users_status_valid CHECK (status IN ('active', 'suspended'));

---- create above / drop below ----

ALTER TABLE users
    DROP CONSTRAINT users_status_valid,
    DROP COLUMN status;
