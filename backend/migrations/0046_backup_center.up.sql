CREATE TABLE IF NOT EXISTS backup_storage_targets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name text NOT NULL,
    type text NOT NULL,
    config_encrypted text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true,
    last_tested_at timestamptz,
    last_test_status text,
    last_test_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_backup_storage_targets_org_id ON backup_storage_targets(org_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_backup_storage_targets_org_name ON backup_storage_targets(org_id, lower(name));

CREATE TABLE IF NOT EXISTS backup_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    node_id uuid REFERENCES nodes(id) ON DELETE SET NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true,
    timezone text NOT NULL DEFAULT 'UTC',
    cron_expression text NOT NULL,
    retention_days integer NOT NULL DEFAULT 14,
    storage_target_id uuid NOT NULL REFERENCES backup_storage_targets(id) ON DELETE RESTRICT,
    compression_enabled boolean NOT NULL DEFAULT true,
    compression_level integer,
    upload_concurrency integer NOT NULL DEFAULT 2,
    last_run_at timestamptz,
    last_success_at timestamptz,
    last_status text NOT NULL DEFAULT 'idle',
    last_error text,
    last_size_bytes bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_backup_jobs_org_id ON backup_jobs(org_id);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_node_id ON backup_jobs(node_id);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_storage_target_id ON backup_jobs(storage_target_id);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_enabled ON backup_jobs(enabled);
CREATE UNIQUE INDEX IF NOT EXISTS idx_backup_jobs_org_name ON backup_jobs(org_id, lower(name));

CREATE TABLE IF NOT EXISTS backup_sources (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id uuid NOT NULL REFERENCES backup_jobs(id) ON DELETE CASCADE,
    type text NOT NULL,
    name text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    order_index integer NOT NULL DEFAULT 0,
    config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_backup_sources_job_id ON backup_sources(job_id);
CREATE INDEX IF NOT EXISTS idx_backup_sources_job_order ON backup_sources(job_id, order_index);

CREATE TABLE IF NOT EXISTS backup_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    job_id uuid NOT NULL REFERENCES backup_jobs(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'queued',
    trigger_type text NOT NULL,
    initiated_by_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    local_workdir text,
    remote_workdir text,
    remote_path text,
    total_size_bytes bigint NOT NULL DEFAULT 0,
    uploaded_size_bytes bigint NOT NULL DEFAULT 0,
    file_count integer NOT NULL DEFAULT 0,
    checksum_status text NOT NULL DEFAULT 'pending',
    cleanup_status text NOT NULL DEFAULT 'pending',
    error_summary text,
    exit_code integer,
    log_excerpt text,
    log_path text,
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    duration_ms bigint NOT NULL DEFAULT 0,
    cancel_requested_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_backup_runs_org_id ON backup_runs(org_id);
CREATE INDEX IF NOT EXISTS idx_backup_runs_job_id ON backup_runs(job_id);
CREATE INDEX IF NOT EXISTS idx_backup_runs_status ON backup_runs(status);
CREATE INDEX IF NOT EXISTS idx_backup_runs_started_at ON backup_runs(started_at DESC);

CREATE TABLE IF NOT EXISTS backup_run_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id uuid NOT NULL REFERENCES backup_runs(id) ON DELETE CASCADE,
    source_id uuid REFERENCES backup_sources(id) ON DELETE SET NULL,
    item_type text NOT NULL,
    logical_name text NOT NULL,
    output_file_name text NOT NULL,
    remote_source_path text,
    size_bytes bigint NOT NULL DEFAULT 0,
    checksum text,
    status text NOT NULL DEFAULT 'queued',
    started_at timestamptz,
    finished_at timestamptz,
    error_text text,
    extra_json jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_backup_run_items_run_id ON backup_run_items(run_id);
CREATE INDEX IF NOT EXISTS idx_backup_run_items_source_id ON backup_run_items(source_id);
CREATE INDEX IF NOT EXISTS idx_backup_run_items_status ON backup_run_items(status);

CREATE TABLE IF NOT EXISTS backup_templates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug text NOT NULL UNIQUE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    definition_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
