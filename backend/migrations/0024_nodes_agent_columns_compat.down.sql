ALTER TABLE nodes
  DROP COLUMN IF EXISTS agent_allow_insecure_tls,
  DROP COLUMN IF EXISTS agent_token_enc,
  DROP COLUMN IF EXISTS agent_url,
  DROP COLUMN IF EXISTS agent_enabled;

DROP INDEX IF EXISTS idx_nodes_agent_enabled;
