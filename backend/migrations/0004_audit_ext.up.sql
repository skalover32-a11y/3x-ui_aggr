ALTER TABLE audit_logs
    ADD COLUMN IF NOT EXISTS ts timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS actor_user text,
    ADD COLUMN IF NOT EXISTS ip text,
    ADD COLUMN IF NOT EXISTS message text,
    ADD COLUMN IF NOT EXISTS payload_json jsonb NOT NULL DEFAULT '{}'::jsonb;

UPDATE audit_logs SET ts = created_at WHERE ts IS NULL;
UPDATE audit_logs SET payload_json = payload WHERE payload_json = '{}'::jsonb AND payload IS NOT NULL;
