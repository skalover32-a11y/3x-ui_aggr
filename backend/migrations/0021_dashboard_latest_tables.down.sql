DROP INDEX IF EXISTS idx_active_users_latest_last_seen;
DROP INDEX IF EXISTS idx_active_users_latest_client_email;
DROP INDEX IF EXISTS idx_active_users_latest_node_id;
DROP INDEX IF EXISTS idx_active_users_latest_unique;
DROP TABLE IF EXISTS active_users_latest;

DROP INDEX IF EXISTS idx_node_metrics_latest_collected_at;
DROP TABLE IF EXISTS node_metrics_latest;
