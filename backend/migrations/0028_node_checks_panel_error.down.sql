ALTER TABLE node_checks
  DROP COLUMN IF EXISTS panel_error_detail,
  DROP COLUMN IF EXISTS panel_error_code;
