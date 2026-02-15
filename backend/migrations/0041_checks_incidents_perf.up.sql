ALTER TABLE checks
  ADD COLUMN IF NOT EXISTS fail_after_sec int NOT NULL DEFAULT 300,
  ADD COLUMN IF NOT EXISTS recover_after_ok int NOT NULL DEFAULT 2,
  ADD COLUMN IF NOT EXISTS mute_until timestamptz NULL;

ALTER TABLE alert_states
  ADD COLUMN IF NOT EXISTS ok_streak int NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS incident_id uuid NULL;

CREATE TABLE IF NOT EXISTS incidents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id uuid NULL REFERENCES organizations(id) ON DELETE CASCADE,
  fingerprint text NOT NULL UNIQUE,
  alert_type text NOT NULL,
  severity text NOT NULL DEFAULT 'critical',
  status text NOT NULL DEFAULT 'open',
  node_id uuid NULL REFERENCES nodes(id) ON DELETE SET NULL,
  service_id uuid NULL REFERENCES services(id) ON DELETE SET NULL,
  bot_id uuid NULL REFERENCES bots(id) ON DELETE SET NULL,
  check_id uuid NULL REFERENCES checks(id) ON DELETE SET NULL,
  title text NOT NULL DEFAULT '',
  description text NULL,
  first_seen timestamptz NOT NULL DEFAULT now(),
  last_seen timestamptz NOT NULL DEFAULT now(),
  acknowledged_at timestamptz NULL,
  acknowledged_by text NULL,
  recovered_at timestamptz NULL,
  occurrences int NOT NULL DEFAULT 1,
  last_error text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'alert_states_incident_id_fkey'
  ) THEN
    ALTER TABLE alert_states
      ADD CONSTRAINT alert_states_incident_id_fkey
      FOREIGN KEY (incident_id) REFERENCES incidents(id) ON DELETE SET NULL;
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_incidents_status_last_seen ON incidents(status, last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_incidents_org_status ON incidents(org_id, status);
CREATE INDEX IF NOT EXISTS idx_incidents_node_id ON incidents(node_id);
CREATE INDEX IF NOT EXISTS idx_alert_states_status_muted ON alert_states(last_status, muted_until);

CREATE INDEX IF NOT EXISTS idx_node_metrics_ts_node ON node_metrics(ts DESC, node_id);
CREATE INDEX IF NOT EXISTS idx_node_metrics_traffic_cov
  ON node_metrics(node_id, ts DESC)
  INCLUDE (net_rx_bytes, net_tx_bytes);
