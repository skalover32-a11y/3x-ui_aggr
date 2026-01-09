DROP INDEX IF EXISTS idx_nodes_is_sandbox;

ALTER TABLE nodes
  DROP COLUMN IF EXISTS is_sandbox;
