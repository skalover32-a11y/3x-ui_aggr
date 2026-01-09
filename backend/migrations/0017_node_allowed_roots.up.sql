ALTER TABLE IF EXISTS nodes
  ADD COLUMN IF NOT EXISTS allowed_roots text[];
