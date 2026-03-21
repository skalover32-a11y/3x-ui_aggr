import React from "react";
import {
  Modal,
  SOURCE_TYPE_OPTIONS,
  createSourceDraft,
  defaultSourceConfig,
  summarizeSource,
  sourceTypeLabel,
} from "./shared.jsx";

const ALLOWED_SOURCE_OPTIONS = SOURCE_TYPE_OPTIONS.filter((item) => item.value !== "custom_command");

function emptyState() {
  return { open: false, mode: "create", form: createSourceDraft(), busy: false, error: "" };
}

export function openSourceEditorState(source = null) {
  if (!source) {
    return { open: true, mode: "create", form: createSourceDraft(), busy: false, error: "" };
  }
  return {
    open: true,
    mode: "edit",
    form: {
      id: source.id || "",
      name: source.name || "",
      type: source.type || "directory_path",
      enabled: source.enabled !== false,
      order_index: Number(source.order_index || 0),
      config: source.config ? { ...source.config } : defaultSourceConfig(source.type || "directory_path"),
    },
    busy: false,
    error: "",
  };
}

function CatalogButtons({ items, onPick, title }) {
  if (!Array.isArray(items) || items.length === 0) return null;
  return (
    <div className="backup-catalog-block">
      <div className="muted small">{title}</div>
      <div className="backup-chip-row">
        {items.map((item) => (
          <button
            key={item.name}
            type="button"
            className="secondary backup-chip"
            onClick={() => onPick(item)}
          >
            {item.label || item.name}
          </button>
        ))}
      </div>
    </div>
  );
}

function renderConfigFields({ t, form, patchConfig, catalogs, nodeId }) {
  const cfg = form.config || {};
  const nodeBoundHint = !nodeId ? t("Pick a node in the job to use detected server catalog") : "";
  switch (form.type) {
    case "directory_path":
    case "file_path":
      return (
        <>
          <label>
            <span>{t("Path")}</span>
            <input
              value={cfg.path || ""}
              onChange={(event) => patchConfig({ path: event.target.value })}
              placeholder={form.type === "directory_path" ? "/etc/nginx" : "/etc/nginx/nginx.conf"}
            />
          </label>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={Boolean(cfg.archive)}
              onChange={(event) => patchConfig({ archive: event.target.checked })}
            />
            <span>{t("Archive before upload")}</span>
          </label>
          <div className="backup-form-span">
            <CatalogButtons items={catalogs.commonPaths} onPick={(item) => patchConfig({ path: item.name })} title={t("Common paths")} />
            <CatalogButtons items={catalogs.systemDetected} onPick={(item) => patchConfig({ path: item.name })} title={nodeBoundHint || t("Detected on selected node")} />
            {catalogs.error ? <div className="muted small">{catalogs.error}</div> : null}
          </div>
        </>
      );
    case "docker_volume":
      return (
        <>
          <label>
            <span>{t("Volume name")}</span>
            <input
              value={cfg.volume_name || ""}
              onChange={(event) => patchConfig({ volume_name: event.target.value })}
              placeholder="3x-ui_aggr_prometheus_data"
            />
          </label>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={Boolean(cfg.archive)}
              onChange={(event) => patchConfig({ archive: event.target.checked })}
            />
            <span>{t("Archive before upload")}</span>
          </label>
          <div className="backup-form-span">
            <CatalogButtons items={catalogs.dockerVolumes} onPick={(item) => patchConfig({ volume_name: item.name })} title={nodeBoundHint || t("Detected docker volumes")} />
            {catalogs.error ? <div className="muted small">{catalogs.error}</div> : null}
          </div>
        </>
      );
    case "postgres_container_dump":
      return (
        <>
          <label>
            <span>{t("Container name")}</span>
            <input
              value={cfg.container_name || ""}
              onChange={(event) => patchConfig({ container_name: event.target.value })}
              placeholder="3x-ui_aggr-postgres-1"
            />
          </label>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={Boolean(cfg.auto_detect_from_env)}
              onChange={(event) => patchConfig({ auto_detect_from_env: event.target.checked })}
            />
            <span>{t("Auto-detect DB credentials from container env")}</span>
          </label>
          {!cfg.auto_detect_from_env && (
            <>
              <label>
                <span>{t("DB name")}</span>
                <input value={cfg.db_name || ""} onChange={(event) => patchConfig({ db_name: event.target.value })} />
              </label>
              <label>
                <span>{t("DB user")}</span>
                <input value={cfg.db_user || ""} onChange={(event) => patchConfig({ db_user: event.target.value })} />
              </label>
              <label>
                <span>{t("DB password")}</span>
                <input type="password" value={cfg.db_password || ""} onChange={(event) => patchConfig({ db_password: event.target.value })} />
              </label>
            </>
          )}
          <label className="backup-form-span">
            <span>{t("Extra dump args")}</span>
            <input value={cfg.dump_args || ""} onChange={(event) => patchConfig({ dump_args: event.target.value })} placeholder="--clean --if-exists" />
          </label>
          <div className="backup-form-span">
            <CatalogButtons items={catalogs.postgresContainers} onPick={(item) => patchConfig({ container_name: item.name })} title={nodeBoundHint || t("Detected postgres containers")} />
            {catalogs.error ? <div className="muted small">{catalogs.error}</div> : null}
          </div>
        </>
      );
    case "postgres_manual_dump":
      return (
        <>
          <label>
            <span>{t("Host")}</span>
            <input value={cfg.host || ""} onChange={(event) => patchConfig({ host: event.target.value })} placeholder="127.0.0.1" />
          </label>
          <label>
            <span>{t("Port")}</span>
            <input type="number" min="1" value={cfg.port || 5432} onChange={(event) => patchConfig({ port: Number(event.target.value || 5432) })} />
          </label>
          <label>
            <span>{t("DB name")}</span>
            <input value={cfg.db_name || ""} onChange={(event) => patchConfig({ db_name: event.target.value })} />
          </label>
          <label>
            <span>{t("DB user")}</span>
            <input value={cfg.db_user || ""} onChange={(event) => patchConfig({ db_user: event.target.value })} />
          </label>
          <label>
            <span>{t("DB password")}</span>
            <input type="password" value={cfg.db_password || ""} onChange={(event) => patchConfig({ db_password: event.target.value })} />
          </label>
          <label>
            <span>{t("Extra dump args")}</span>
            <input value={cfg.dump_args || ""} onChange={(event) => patchConfig({ dump_args: event.target.value })} placeholder="--clean --if-exists" />
          </label>
        </>
      );
    case "nginx_snapshot":
      return (
        <>
          <label>
            <span>{t("Command")}</span>
            <input value={cfg.command || "nginx -T"} onChange={(event) => patchConfig({ command: event.target.value })} />
          </label>
          <label>
            <span>{t("Output name")}</span>
            <input value={cfg.output_name || "nginx-full-config"} onChange={(event) => patchConfig({ output_name: event.target.value })} />
          </label>
        </>
      );
    case "cron_snapshot":
      return (
        <>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={Boolean(cfg.include_system)}
              onChange={(event) => patchConfig({ include_system: event.target.checked })}
            />
            <span>{t("Include system cron")}</span>
          </label>
          <label>
            <span>{t("Output name")}</span>
            <input value={cfg.output_name || "cron-snapshot"} onChange={(event) => patchConfig({ output_name: event.target.value })} />
          </label>
        </>
      );
    case "docker_inventory_snapshot":
      return (
        <label>
          <span>{t("Output name")}</span>
          <input value={cfg.output_name || "docker-inventory"} onChange={(event) => patchConfig({ output_name: event.target.value })} />
        </label>
      );
    case "system_snapshot":
      return (
        <label>
          <span>{t("Output name")}</span>
          <input value={cfg.output_name || "system-snapshot"} onChange={(event) => patchConfig({ output_name: event.target.value })} />
        </label>
      );
    default:
      return (
        <div className="muted small">
          {t("This source type is not available in the current UI yet")}
        </div>
      );
  }
}

export default function BackupSourceModal({
  t,
  state,
  nodeId,
  catalogs,
  onClose,
  onChange,
  onSave,
  onRefreshCatalogs,
}) {
  if (!state?.open) return null;

  const form = state.form || createSourceDraft();

  function patchForm(patch) {
    onChange({ ...state, form: { ...form, ...patch }, error: "" });
  }

  function patchConfig(patch) {
    patchForm({ config: { ...(form.config || {}), ...patch } });
  }

  function changeType(nextType) {
    patchForm({
      type: nextType,
      config: defaultSourceConfig(nextType),
    });
  }

  return (
    <Modal
      title={state.mode === "create" ? t("Add source") : t("Edit source")}
      subtitle={`${sourceTypeLabel(form.type, t)} · ${summarizeSource(form, t)}`}
      wide
      onClose={onClose}
    >
      {state.error ? <div className="error">{state.error}</div> : null}
      <div className="section-head">
        <div className="muted small">{t("Source builder")}</div>
        <div className="section-actions">
          <button type="button" className="secondary" onClick={onRefreshCatalogs} disabled={Boolean(catalogs.busy)}>
            {catalogs.busy ? t("Loading...") : t("Refresh catalog")}
          </button>
        </div>
      </div>
      <div className="form-grid">
        <label>
          <span>{t("Name")}</span>
          <input value={form.name || ""} onChange={(event) => patchForm({ name: event.target.value })} placeholder={t("Human-readable source name")} />
        </label>
        <label>
          <span>{t("Source type")}</span>
          <select value={form.type} onChange={(event) => changeType(event.target.value)}>
            {ALLOWED_SOURCE_OPTIONS.map((item) => (
              <option key={item.value} value={item.value}>{t(item.label)}</option>
            ))}
          </select>
        </label>
        <label className="checkbox">
          <input
            type="checkbox"
            checked={form.enabled !== false}
            onChange={(event) => patchForm({ enabled: event.target.checked })}
          />
          <span>{t("Enabled")}</span>
        </label>
        {renderConfigFields({ t, form, patchConfig, catalogs, nodeId })}
      </div>
      <div className="actions">
        <button type="button" className="secondary" onClick={onClose}>{t("Close")}</button>
        <button type="button" onClick={onSave} disabled={Boolean(state.busy)}>
          {state.busy ? t("Loading...") : t("Save")}
        </button>
      </div>
    </Modal>
  );
}

export { emptyState as emptySourceEditorState };
