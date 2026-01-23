ALTER TABLE node_checks
  ADD COLUMN IF NOT EXISTS panel_error_code text,
  ADD COLUMN IF NOT EXISTS panel_error_detail text;
