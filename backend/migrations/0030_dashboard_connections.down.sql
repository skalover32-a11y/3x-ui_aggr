ALTER TABLE node_metrics_latest
  DROP COLUMN IF EXISTS ping_ms,
  DROP COLUMN IF EXISTS tcp_connections,
  DROP COLUMN IF EXISTS udp_connections;
