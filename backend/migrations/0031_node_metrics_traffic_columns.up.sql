ALTER TABLE node_metrics
  ADD COLUMN IF NOT EXISTS net_rx_bytes bigint,
  ADD COLUMN IF NOT EXISTS net_tx_bytes bigint,
  ADD COLUMN IF NOT EXISTS ping_ms bigint;
