ALTER TABLE nodes ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'PANEL';

UPDATE nodes
SET kind = CASE
  WHEN base_url IS NULL OR trim(base_url) = '' THEN 'HOST'
  ELSE 'PANEL'
END;
