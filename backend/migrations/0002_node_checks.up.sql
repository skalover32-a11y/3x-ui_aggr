CREATE TABLE IF NOT EXISTS node_checks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    ts timestamptz NOT NULL DEFAULT now(),
    panel_ok boolean NOT NULL,
    ssh_ok boolean NOT NULL,
    latency_ms int,
    error text NULL
);

CREATE INDEX IF NOT EXISTS idx_node_checks_node_id_ts ON node_checks(node_id, ts DESC);
