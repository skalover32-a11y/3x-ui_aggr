ALTER TABLE nodes
  ADD COLUMN IF NOT EXISTS is_sandbox boolean NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_nodes_is_sandbox ON nodes (is_sandbox);
