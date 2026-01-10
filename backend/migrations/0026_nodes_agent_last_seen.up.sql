ALTER TABLE nodes
  ADD COLUMN IF NOT EXISTS agent_last_seen_at timestamptz;
