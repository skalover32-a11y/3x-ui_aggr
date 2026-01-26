ALTER TABLE node_metrics
  DROP COLUMN IF EXISTS net_rx_bytes,
  DROP COLUMN IF EXISTS net_tx_bytes,
  DROP COLUMN IF EXISTS ping_ms;
