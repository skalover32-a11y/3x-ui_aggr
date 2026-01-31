CREATE TABLE IF NOT EXISTS fs_audit_logs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  ts timestamptz NOT NULL DEFAULT now(),
  user_id uuid NULL,
  actor text NOT NULL,
  node_id uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  op text NOT NULL,
  path text NOT NULL,
  extra jsonb NOT NULL DEFAULT '{}'::jsonb,
  ok boolean NOT NULL DEFAULT true
);

CREATE INDEX IF NOT EXISTS idx_fs_audit_logs_node_ts ON fs_audit_logs (node_id, ts DESC);
