CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS nodes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    tags text[] NOT NULL DEFAULT '{}',
    base_url text NOT NULL,
    panel_username text NOT NULL,
    panel_password_enc text NOT NULL,
    ssh_host text NOT NULL,
    ssh_port int NOT NULL DEFAULT 22,
    ssh_user text NOT NULL,
    ssh_key_enc text NOT NULL,
    verify_tls bool NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor text NOT NULL,
    node_id uuid NULL REFERENCES nodes(id) ON DELETE SET NULL,
    action text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL,
    error text NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_node_id ON audit_logs(node_id);
