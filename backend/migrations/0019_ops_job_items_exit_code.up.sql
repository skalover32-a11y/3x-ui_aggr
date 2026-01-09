ALTER TABLE IF EXISTS ops_job_items
  ADD COLUMN IF NOT EXISTS exit_code int;
