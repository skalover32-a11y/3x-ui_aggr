-- Usage:
-- psql "$DB_DSN" -v pattern='%example.com%' -f backend/scripts/cleanup_services.sql
-- NOTE: CTE scope is per statement; keep each delete self-contained.

WITH svc AS (
  SELECT id
  FROM services
  WHERE url ILIKE :'pattern'
)
DELETE FROM check_results
WHERE check_id IN (
  SELECT id FROM checks
  WHERE target_type = 'service'
    AND target_id IN (SELECT id FROM svc)
);

WITH svc AS (
  SELECT id
  FROM services
  WHERE url ILIKE :'pattern'
)
DELETE FROM checks
WHERE target_type = 'service'
  AND target_id IN (SELECT id FROM svc);

WITH svc AS (
  SELECT id
  FROM services
  WHERE url ILIKE :'pattern'
)
DELETE FROM alert_states
WHERE service_id IN (SELECT id FROM svc);

WITH svc AS (
  SELECT id
  FROM services
  WHERE url ILIKE :'pattern'
)
DELETE FROM services
WHERE id IN (SELECT id FROM svc);
