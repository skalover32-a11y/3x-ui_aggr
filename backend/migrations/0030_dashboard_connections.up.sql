ALTER TABLE node_metrics_latest
  ADD COLUMN IF NOT EXISTS ping_ms bigint,
  ADD COLUMN IF NOT EXISTS tcp_connections bigint,
  ADD COLUMN IF NOT EXISTS udp_connections bigint;
