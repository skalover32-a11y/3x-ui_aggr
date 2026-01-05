DROP INDEX IF EXISTS idx_alert_states_bot;
ALTER TABLE alert_states DROP COLUMN IF EXISTS bot_id;

DROP INDEX IF EXISTS idx_bots_node_id;
DROP TABLE IF EXISTS bots;
