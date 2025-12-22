CREATE TABLE IF NOT EXISTS telegram_settings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_token_enc text NOT NULL,
    admin_chat_id text NOT NULL,
    alert_connection boolean NOT NULL DEFAULT true,
    alert_cpu boolean NOT NULL DEFAULT true,
    alert_memory boolean NOT NULL DEFAULT true,
    alert_disk boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_telegram_settings_created_at ON telegram_settings(created_at DESC);
