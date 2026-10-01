-- Durable schedule state and configuration-change wake-ups.

ALTER TABLE source_configurations
    ADD COLUMN last_run_at timestamptz;

COMMENT ON COLUMN source_configurations.last_run_at IS
    'When the scheduler last dispatched this configuration; survives restarts so a boot pass fetches only overdue work.';

CREATE OR REPLACE FUNCTION notify_source_configurations() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('macro_terminal_source_configurations', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER source_configurations_notify_after_change
AFTER INSERT OR UPDATE OF source_id, type, config ON source_configurations
FOR EACH ROW EXECUTE FUNCTION notify_source_configurations();

---- create above / drop below ----

DROP TRIGGER source_configurations_notify_after_change ON source_configurations;
DROP FUNCTION notify_source_configurations();
COMMENT ON COLUMN source_configurations.last_run_at IS NULL;
ALTER TABLE source_configurations
    DROP COLUMN last_run_at;
