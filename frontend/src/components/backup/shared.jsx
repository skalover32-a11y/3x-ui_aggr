import React from "react";
import { useI18n } from "../../i18n.js";

export const TABS = ["jobs", "targets", "runs", "templates"];

export const STORAGE_TYPE_OPTIONS = [
  { value: "ftp", label: "FTP" },
  { value: "ftps", label: "FTPS" },
  { value: "sftp", label: "SFTP" },
  { value: "webdav", label: "WebDAV" },
  { value: "s3", label: "S3-compatible" },
  { value: "local", label: "Local path" },
];

export const SOURCE_TYPE_OPTIONS = [
  { value: "directory_path", label: "Directory path" },
  { value: "file_path", label: "File path" },
  { value: "docker_volume", label: "Docker volume" },
  { value: "postgres_container_dump", label: "PostgreSQL container dump" },
  { value: "postgres_manual_dump", label: "PostgreSQL manual dump" },
  { value: "nginx_snapshot", label: "Nginx snapshot" },
  { value: "cron_snapshot", label: "Cron snapshot" },
  { value: "docker_inventory_snapshot", label: "Docker inventory" },
  { value: "system_snapshot", label: "System snapshot" },
  { value: "custom_command", label: "Custom command" },
];

export function formatTS(ts) {
  if (!ts) return "-";
  const date = new Date(ts);
  if (Number.isNaN(date.getTime())) return "-";
  return date.toLocaleString();
}

export function formatBytes(bytes) {
  const numeric = Number(bytes || 0);
  if (!Number.isFinite(numeric) || numeric <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = numeric;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

export function formatDuration(ms) {
  const numeric = Number(ms || 0);
  if (!Number.isFinite(numeric) || numeric <= 0) return "-";
  const totalSec = Math.round(numeric / 1000);
  const mins = Math.floor(totalSec / 60);
  const hours = Math.floor(mins / 60);
  const seconds = totalSec % 60;
  if (hours > 0) return `${hours}h ${mins % 60}m`;
  if (mins > 0) return `${mins}m ${seconds}s`;
  return `${seconds}s`;
}

export function formatCronPreview(expression, timezone, t = (value) => value) {
  const cron = String(expression || "").trim();
  const tz = String(timezone || "UTC").trim() || "UTC";
  const parts = cron.split(/\s+/);
  if (parts.length !== 5) return `${cron} (${tz})`;
  const [minute, hour, dom, month, dow] = parts;
  if (dom === "*" && month === "*" && dow === "*" && /^\d+$/.test(hour) && /^\d+$/.test(minute)) {
    return t("Daily at {time} ({timezone})", {
      time: `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`,
      timezone: tz,
    });
  }
  if (hour === "*" && dom === "*" && month === "*" && dow === "*" && /^\d+$/.test(minute)) {
    return t("Hourly at :{minute} ({timezone})", {
      minute: String(minute).padStart(2, "0"),
      timezone: tz,
    });
  }
  return `${cron} (${tz})`;
}

export function defaultPortForStorage(type) {
  switch (type) {
    case "ftp":
    case "ftps":
      return 21;
    case "sftp":
      return 22;
    default:
      return 0;
  }
}

export function createTargetForm(type = "sftp") {
  return {
    id: "",
    name: "",
    type,
    enabled: true,
    host: "",
    port: defaultPortForStorage(type),
    username: "",
    password: "",
    base_path: "",
    timeout_sec: 30,
    passive_mode: type === "ftp" || type === "ftps",
    insecure_skip_verify: false,
    auth_method: type === "sftp" ? "password" : "",
    private_key_pem: "",
    url: "",
    bucket: "",
    region: "",
    access_key: "",
    secret_key: "",
    use_ssl: type === "s3",
    path_style: false,
    local_path: "",
  };
}

export function createJobForm(defaults = {}) {
  return {
    id: "",
    name: "",
    description: "",
    node_id: defaults.node_id || "",
    enabled: true,
    timezone: defaults.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
    cron_expression: defaults.cron_expression || "0 3 * * *",
    retention_days: defaults.retention_days || 14,
    storage_target_id: defaults.storage_target_id || "",
    compression_enabled: true,
    compression_level: 6,
    upload_concurrency: 2,
  };
}

export function defaultSourceConfig(type) {
  switch (type) {
    case "directory_path":
      return { path: "", archive: true };
    case "file_path":
      return { path: "", archive: true };
    case "docker_volume":
      return { volume_name: "", archive: true };
    case "postgres_container_dump":
      return { container_name: "", db_name: "", db_user: "", db_password: "", auto_detect_from_env: true, dump_args: "" };
    case "postgres_manual_dump":
      return { host: "", port: 5432, db_name: "", db_user: "", db_password: "", dump_args: "" };
    case "nginx_snapshot":
      return { command: "nginx -T", output_name: "nginx-full-config" };
    case "cron_snapshot":
      return { include_system: true, output_name: "cron-snapshot" };
    case "docker_inventory_snapshot":
      return { output_name: "docker-inventory" };
    case "system_snapshot":
      return { output_name: "system-snapshot" };
    case "custom_command":
      return { command: "", output_name: "custom-command", allow_untrusted_command: false };
    default:
      return {};
  }
}

export function createSourceDraft(type = "directory_path") {
  return {
    id: "",
    name: "",
    type,
    enabled: true,
    order_index: 0,
    config: defaultSourceConfig(type),
  };
}

export function parseTemplateDefinition(definition) {
  if (!definition) return { sources: [] };
  if (typeof definition === "object") return definition;
  try {
    return JSON.parse(definition);
  } catch {
    return { sources: [] };
  }
}

export function sourceTypeLabel(type, t = (value) => value) {
  const label = SOURCE_TYPE_OPTIONS.find((item) => item.value === type)?.label || type || "-";
  return t(label);
}

export function storageTypeLabel(type, t = (value) => value) {
  const label = STORAGE_TYPE_OPTIONS.find((item) => item.value === type)?.label || type || "-";
  return t(label);
}

export function summarizeTarget(target) {
  const cfg = target?.config || {};
  switch (target?.type) {
    case "ftp":
    case "ftps":
    case "sftp":
      return `${cfg.host || "-"}${cfg.port ? `:${cfg.port}` : ""}${cfg.base_path ? ` · ${cfg.base_path}` : ""}`;
    case "webdav":
      return `${cfg.url || "-"}${cfg.base_path ? ` · ${cfg.base_path}` : ""}`;
    case "s3":
      return `${cfg.bucket || "-"}${cfg.region ? ` · ${cfg.region}` : ""}${cfg.base_path ? ` · ${cfg.base_path}` : ""}`;
    case "local":
      return cfg.local_path || "-";
    default:
      return "-";
  }
}

export function summarizeSource(source, t = (value) => value) {
  const cfg = source?.config || {};
  switch (source?.type) {
    case "directory_path":
    case "file_path":
      return cfg.path || "-";
    case "docker_volume":
      return cfg.volume_name || "-";
    case "postgres_container_dump":
      return cfg.container_name || "-";
    case "postgres_manual_dump":
      return `${cfg.host || "-"}:${cfg.port || 5432}/${cfg.db_name || ""}`;
    case "nginx_snapshot":
      return cfg.command || "nginx -T";
    case "cron_snapshot":
      return cfg.include_system ? t("user + system cron") : t("user crontab");
    case "docker_inventory_snapshot":
      return t("docker ps / volume / network / image / compose");
    case "system_snapshot":
      return t("hostnamectl, ip addr, ss, df, free");
    case "custom_command":
      return cfg.command || "-";
    default:
      return "-";
  }
}

export function normalizeSourceForForm(source) {
  return {
    id: source?.id || "",
    name: source?.name || "",
    type: source?.type || "directory_path",
    enabled: source?.enabled !== false,
    order_index: Number(source?.order_index || 0),
    config: source?.config ? { ...source.config } : defaultSourceConfig(source?.type || "directory_path"),
  };
}

export function statusTone(status) {
  switch (String(status || "").toLowerCase()) {
    case "success":
    case "enabled":
    case "online":
    case "idle":
      return "ok";
    case "running":
    case "scheduled":
    case "queued":
    case "partial_success":
      return "warn";
    case "failed":
    case "cancelled":
    case "disabled":
      return "danger";
    default:
      return "muted";
  }
}

export function humanStatus(status, t = (value) => value) {
  const value = String(status || "").trim();
  if (!value) return "-";
  const normalized = value.toLowerCase();
  switch (normalized) {
    case "queued":
      return t("Queued");
    case "running":
      return t("Running");
    case "success":
      return t("Success");
    case "failed":
      return t("Failed");
    case "partial_success":
      return t("Partial success");
    case "cancelled":
      return t("Cancelled");
    case "idle":
      return t("Idle");
    case "enabled":
      return t("Enabled");
    case "disabled":
      return t("Disabled");
    case "scheduled":
      return t("Scheduled");
    case "online":
      return t("Online");
    default:
      return value.replaceAll("_", " ");
  }
}

export function StatusPill({ value }) {
  const { t } = useI18n();
  const tone = statusTone(value);
  return <span className={`backup-status-pill ${tone}`}>{humanStatus(value, t)}</span>;
}

export function SummaryCard({ title, value, note }) {
  return (
    <div className="backup-summary-card">
      <div className="backup-summary-title">{title}</div>
      <div className="backup-summary-value">{value}</div>
      {note ? <div className="muted small">{note}</div> : null}
    </div>
  );
}

export function Modal({ title, subtitle, wide = false, children, onClose }) {
  return (
    <div className="modal overlay-modal" onClick={onClose}>
      <div className={`modal-content ${wide ? "wide" : ""}`} onClick={(event) => event.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h3>{title}</h3>
            {subtitle ? <div className="muted small">{subtitle}</div> : null}
          </div>
        </div>
        {children}
      </div>
    </div>
  );
}
