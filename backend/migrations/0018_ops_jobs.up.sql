CREATE TABLE IF NOT EXISTS ops_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  type text NOT NULL,
  status text NOT NULL,
  created_by_actor text NOT NULL,
  created_by_user_id uuid NULL,
  parallelism int NOT NULL DEFAULT 5,
  targets jsonb NOT NULL DEFAULT '[]'::jsonb,
  params jsonb NOT NULL DEFAULT '{}'::jsonb,
  error text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz NULL,
  finished_at timestamptz NULL
);

CREATE TABLE IF NOT EXISTS ops_job_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  job_id uuid NOT NULL,
  node_id uuid NOT NULL,
  status text NOT NULL,
  log text NOT NULL DEFAULT '',
  error text NULL,
  started_at timestamptz NULL,
  finished_at timestamptz NULL
);

CREATE INDEX IF NOT EXISTS idx_ops_jobs_status_created_at ON ops_jobs (status, created_at);
CREATE INDEX IF NOT EXISTS idx_ops_job_items_job_id ON ops_job_items (job_id);
CREATE INDEX IF NOT EXISTS idx_ops_job_items_node_id_status ON ops_job_items (node_id, status);
