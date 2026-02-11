CREATE TABLE IF NOT EXISTS node_metrics_latest (
  node_id uuid PRIMARY KEY,
  collected_at timestamptz NOT NULL,
  cpu_pct real NULL,
  ram_used_bytes bigint NULL,
  ram_total_bytes bigint NULL,
  disk_used_bytes bigint NULL,
  disk_total_bytes bigint NULL,
  net_rx_bps bigint NULL,
  net_tx_bps bigint NULL,
  net_rx_bytes bigint NULL,
  net_tx_bytes bigint NULL,
  uptime_sec bigint NULL,
  service_version text NULL,
  runtime_running boolean NULL,
  service_running boolean NULL
);

CREATE INDEX IF NOT EXISTS idx_node_metrics_latest_collected_at ON node_metrics_latest (collected_at);

CREATE TABLE IF NOT EXISTS active_users_latest (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id uuid NOT NULL,
  source_tag text NULL,
  client_email text NOT NULL,
  ip text NOT NULL DEFAULT '',
  rx_bps bigint NULL,
  tx_bps bigint NULL,
  total_up_bytes bigint NULL,
  total_down_bytes bigint NULL,
  last_seen timestamptz NOT NULL,
  collected_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_active_users_latest_unique ON active_users_latest (node_id, source_tag, client_email, ip);
CREATE INDEX IF NOT EXISTS idx_active_users_latest_node_id ON active_users_latest (node_id);
CREATE INDEX IF NOT EXISTS idx_active_users_latest_client_email ON active_users_latest (client_email);
CREATE INDEX IF NOT EXISTS idx_active_users_latest_last_seen ON active_users_latest (last_seen);

