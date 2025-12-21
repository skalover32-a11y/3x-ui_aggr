-- Add versions to nodes
ALTER TABLE nodes
  ADD COLUMN IF NOT EXISTS xray_version TEXT,
  ADD COLUMN IF NOT EXISTS panel_version TEXT,
  ADD COLUMN IF NOT EXISTS versions_checked_at TIMESTAMPTZ;

-- Metrics time-series
CREATE TABLE IF NOT EXISTS node_metrics (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  ts TIMESTAMPTZ NOT NULL DEFAULT now(),
  load1 DOUBLE PRECISION,
  load5 DOUBLE PRECISION,
  load15 DOUBLE PRECISION,
  mem_total_bytes BIGINT,
  mem_available_bytes BIGINT,
  disk_total_bytes BIGINT,
  disk_used_bytes BIGINT,
  error TEXT
);

CREATE INDEX IF NOT EXISTS idx_node_metrics_node_ts ON node_metrics (node_id, ts DESC);
