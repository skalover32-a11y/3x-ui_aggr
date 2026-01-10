DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'nodes' AND column_name = 'agent_insecure_tls'
  ) AND NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'nodes' AND column_name = 'agent_allow_insecure_tls'
  ) THEN
    ALTER TABLE nodes ADD COLUMN agent_allow_insecure_tls boolean NOT NULL DEFAULT false;
    UPDATE nodes SET agent_allow_insecure_tls = agent_insecure_tls;
  END IF;
END $$;

ALTER TABLE nodes
  ADD COLUMN IF NOT EXISTS agent_enabled boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS agent_url text,
  ADD COLUMN IF NOT EXISTS agent_token_enc text,
  ADD COLUMN IF NOT EXISTS agent_allow_insecure_tls boolean NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_nodes_agent_enabled ON nodes (agent_enabled);
