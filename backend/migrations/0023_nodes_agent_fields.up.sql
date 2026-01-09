ALTER TABLE nodes
  ADD COLUMN IF NOT EXISTS agent_enabled boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS agent_url text,
  ADD COLUMN IF NOT EXISTS agent_token_enc text,
  ADD COLUMN IF NOT EXISTS agent_allow_insecure_tls boolean NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_nodes_agent_enabled ON nodes (agent_enabled);
