ALTER TABLE nodes ADD COLUMN IF NOT EXISTS host text;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS region text;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS provider text;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS is_enabled boolean NOT NULL DEFAULT true;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS ssh_enabled boolean NOT NULL DEFAULT true;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS ssh_auth_method text NOT NULL DEFAULT 'key';
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS ssh_password_enc text;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS capabilities jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS services (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    kind text NOT NULL,
    url text,
    host text,
    port int,
    tls_mode text,
    health_path text,
    expected_status int[],
    headers jsonb NOT NULL DEFAULT '{}'::jsonb,
    auth_ref text,
    is_enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_services_node_id ON services(node_id);

CREATE TABLE IF NOT EXISTS checks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    target_type text NOT NULL,
    target_id uuid NOT NULL,
    type text NOT NULL,
    interval_sec int NOT NULL DEFAULT 60,
    timeout_ms int NOT NULL DEFAULT 3000,
    retries int NOT NULL DEFAULT 1,
    enabled boolean NOT NULL DEFAULT true,
    severity_rules jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_checks_target ON checks(target_type, target_id);

CREATE TABLE IF NOT EXISTS check_results (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    check_id uuid NOT NULL REFERENCES checks(id) ON DELETE CASCADE,
    ts timestamptz NOT NULL DEFAULT now(),
    status text NOT NULL,
    metrics jsonb NOT NULL DEFAULT '{}'::jsonb,
    error text,
    latency_ms int
);
CREATE INDEX IF NOT EXISTS idx_check_results_check_ts ON check_results(check_id, ts DESC);

CREATE TABLE IF NOT EXISTS alert_states (
    fingerprint text PRIMARY KEY,
    alert_type text NOT NULL,
    node_id uuid NULL REFERENCES nodes(id) ON DELETE SET NULL,
    service_id uuid NULL REFERENCES services(id) ON DELETE SET NULL,
    check_type text,
    last_status text,
    first_seen timestamptz NOT NULL DEFAULT now(),
    last_seen timestamptz NOT NULL DEFAULT now(),
    occurrences int NOT NULL DEFAULT 1,
    last_message_ids jsonb NOT NULL DEFAULT '{}'::jsonb,
    muted_until timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alert_states_node ON alert_states(node_id);
CREATE INDEX IF NOT EXISTS idx_alert_states_service ON alert_states(service_id);
