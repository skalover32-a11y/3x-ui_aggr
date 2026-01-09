ALTER TABLE node_metrics_latest
  ADD COLUMN IF NOT EXISTS net_iface text;
