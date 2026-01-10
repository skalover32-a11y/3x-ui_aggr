DROP INDEX IF EXISTS idx_ops_jobs_public_token_hash;

ALTER TABLE ops_jobs
  DROP COLUMN IF EXISTS public_token_hash;
