CREATE TABLE IF NOT EXISTS bots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    name text NOT NULL,
    kind text NOT NULL,
    docker_container text,
    systemd_unit text,
    health_url text,
    health_path text NOT NULL DEFAULT '/',
    expected_status int[] NOT NULL DEFAULT '{200}',
    is_enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_bots_node_id ON bots(node_id);

ALTER TABLE alert_states ADD COLUMN IF NOT EXISTS bot_id uuid NULL REFERENCES bots(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_alert_states_bot ON alert_states(bot_id);
