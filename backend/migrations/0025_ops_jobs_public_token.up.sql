ALTER TABLE ops_jobs
  ADD COLUMN IF NOT EXISTS public_token_hash text;

CREATE INDEX IF NOT EXISTS idx_ops_jobs_public_token_hash ON ops_jobs (public_token_hash);
