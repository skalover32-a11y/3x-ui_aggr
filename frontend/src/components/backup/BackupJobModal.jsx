import React from "react";
import {
  Modal,
  SummaryCard,
  StatusPill,
  createJobForm,
  formatCronPreview,
  formatDuration,
  formatBytes,
  summarizeSource,
  summarizeTarget,
  sourceTypeLabel,
} from "./shared.jsx";

export function openJobEditorState(defaults = {}) {
  return {
    open: true,
    mode: defaults.mode || "create",
    loading: false,
    busy: false,
    error: "",
    form: defaults.form || createJobForm(),
    sources: defaults.sources || [],
    initialSources: defaults.initialSources || [],
    plan: defaults.plan || null,
  };
}

function sourceKey(source, index) {
  return source.id || source._tmpId || `source-${index}`;
}

export default function BackupJobModal({
  t,
  state,
  nodes,
  targets,
  onClose,
  onChange,
  onSave,
  onAddSource,
  onEditSource,
  onRemoveSource,
  onMoveSource,
  onToggleSourceEnabled,
  onRefreshPlan,
}) {
  if (!state?.open) return null;

  const form = state.form || createJobForm();
  const sourceRows = Array.isArray(state.sources) ? state.sources : [];
  const selectedTarget = targets.find((item) => item.id === form.storage_target_id);
  const selectedNode = nodes.find((item) => item.id === form.node_id);
  const canRefreshPlan = state.mode === "edit" && form.id;

  function patchForm(patch) {
    onChange({ ...state, form: { ...form, ...patch }, error: "" });
  }

  return (
    <Modal
      title={state.mode === "create" ? t("Create backup job") : t("Edit backup job")}
      subtitle={form.name || t("Backup job editor")}
      wide
      onClose={onClose}
    >
      {state.error ? <div className="error">{state.error}</div> : null}
      {state.loading ? <div className="hint">{t("Loading...")}</div> : null}

      <div className="backup-summary-grid">
        <SummaryCard title={t("Schedule")} value={formatCronPreview(form.cron_expression, form.timezone)} note={t("Timezone-aware cron")} />
        <SummaryCard title={t("Storage")} value={selectedTarget?.name || "-"} note={selectedTarget ? summarizeTarget(selectedTarget) : t("Select a storage target")} />
        <SummaryCard title={t("Sources")} value={String(sourceRows.length)} note={t("Ordered execution list")} />
        <SummaryCard title={t("Retention")} value={`${Number(form.retention_days || 0)}d`} note={t("Remote cleanup horizon")} />
      </div>

      <div className="form-grid">
        <label>
          <span>{t("Name")}</span>
          <input value={form.name || ""} onChange={(event) => patchForm({ name: event.target.value })} />
        </label>
        <label>
          <span>{t("Node")}</span>
          <select value={form.node_id || ""} onChange={(event) => patchForm({ node_id: event.target.value })}>
            <option value="">{t("Local backend host")}</option>
            {nodes.map((node) => (
              <option key={node.id} value={node.id}>
                {node.name || node.ssh_host || node.id}
              </option>
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
        <label>
          <span>{t("Timezone")}</span>
          <input value={form.timezone || ""} onChange={(event) => patchForm({ timezone: event.target.value })} placeholder="Europe/Moscow" />
        </label>
        <label>
          <span>{t("Cron expression")}</span>
          <input value={form.cron_expression || ""} onChange={(event) => patchForm({ cron_expression: event.target.value })} placeholder="0 3 * * *" />
        </label>
        <label>
          <span>{t("Retention days")}</span>
          <input
            type="number"
            min="1"
            max="3650"
            value={form.retention_days || 14}
            onChange={(event) => patchForm({ retention_days: Number(event.target.value || 14) })}
          />
        </label>
        <label>
          <span>{t("Storage target")}</span>
          <select value={form.storage_target_id || ""} onChange={(event) => patchForm({ storage_target_id: event.target.value })}>
            <option value="">{t("Select storage target")}</option>
            {targets.map((target) => (
              <option key={target.id} value={target.id}>
                {target.name} · {target.type}
              </option>
            ))}
          </select>
        </label>
        <label className="checkbox">
          <input
            type="checkbox"
            checked={form.compression_enabled !== false}
            onChange={(event) => patchForm({ compression_enabled: event.target.checked })}
          />
          <span>{t("Compression enabled")}</span>
        </label>
        <label>
          <span>{t("Compression level")}</span>
          <input
            type="number"
            min="1"
            max="9"
            value={form.compression_level || 6}
            disabled={!form.compression_enabled}
            onChange={(event) => patchForm({ compression_level: Number(event.target.value || 6) })}
          />
        </label>
        <label>
          <span>{t("Upload concurrency")}</span>
          <input
            type="number"
            min="1"
            max="8"
            value={form.upload_concurrency || 2}
            onChange={(event) => patchForm({ upload_concurrency: Number(event.target.value || 2) })}
          />
        </label>
        <label className="backup-form-span">
          <span>{t("Description")}</span>
          <textarea rows="3" value={form.description || ""} onChange={(event) => patchForm({ description: event.target.value })} />
        </label>
      </div>

      <div className="table-card backup-inline-card">
        <div className="section-head">
          <div>
            <div className="section-title">{t("Sources")}</div>
            <div className="muted small">
              {selectedNode ? t("Node-bound job: {name}", { name: selectedNode.name || selectedNode.ssh_host || selectedNode.id }) : t("No node selected; catalogs will be limited")}
            </div>
          </div>
          <div className="section-actions">
            {canRefreshPlan ? (
              <button type="button" className="secondary" onClick={onRefreshPlan} disabled={Boolean(state.loading)}>
                {state.loading ? t("Loading...") : t("Refresh plan")}
              </button>
            ) : null}
            <button type="button" onClick={onAddSource}>{t("Add source")}</button>
          </div>
        </div>
        <div className="backup-source-list">
          {sourceRows.map((source, index) => (
            <div className="backup-source-row" key={sourceKey(source, index)}>
              <div className="backup-source-main">
                <div className="backup-source-title-row">
                  <div className="node-title">{source.name || source.logical_name || sourceTypeLabel(source.type)}</div>
                  <StatusPill value={source.enabled === false ? "disabled" : "enabled"} />
                </div>
                <div className="muted small">{sourceTypeLabel(source.type)} · {summarizeSource(source)}</div>
              </div>
              <div className="actions compact">
                <button type="button" className="secondary" onClick={() => onMoveSource(source.id || source._tmpId, -1)} disabled={index === 0}>{t("Up")}</button>
                <button type="button" className="secondary" onClick={() => onMoveSource(source.id || source._tmpId, 1)} disabled={index === sourceRows.length - 1}>{t("Down")}</button>
                <button type="button" className="secondary" onClick={() => onToggleSourceEnabled(source.id || source._tmpId)}>
                  {source.enabled === false ? t("Enable") : t("Disable")}
                </button>
                <button type="button" className="secondary" onClick={() => onEditSource(source)}>{t("Edit")}</button>
                <button type="button" className="danger" onClick={() => onRemoveSource(source.id || source._tmpId)}>{t("Delete")}</button>
              </div>
            </div>
          ))}
          {sourceRows.length === 0 && <div className="muted small">{t("No sources configured yet")}</div>}
        </div>
      </div>

      <div className="table-card backup-inline-card">
        <div className="section-head">
          <div>
            <div className="section-title">{t("Execution plan preview")}</div>
            <div className="muted small">
              {state.plan ? t("Backend-resolved plan for the current saved job") : t("Save the job once to generate server-side execution plan")}
            </div>
          </div>
        </div>
        {state.plan ? (
          <>
            <div className="backup-summary-grid compact">
              <SummaryCard title={t("Node")} value={selectedNode?.name || (state.plan.node_id ? state.plan.node_id : t("Local backend host"))} />
              <SummaryCard title={t("Storage type")} value={state.plan.storage_type || "-"} />
              <SummaryCard title={t("Sources")} value={String(Array.isArray(state.plan.sources) ? state.plan.sources.length : 0)} />
              <SummaryCard title={t("Concurrency")} value={String(state.plan.upload_concurrency || 0)} />
            </div>
            <div className="backup-plan-list">
              {(state.plan.sources || []).map((item) => (
                <div className="backup-plan-row" key={item.source_id || `${item.name}-${item.type}`}>
                  <div>
                    <div className="node-title">{item.logical_name || item.name}</div>
                    <div className="muted small">{sourceTypeLabel(item.type)}</div>
                  </div>
                </div>
              ))}
            </div>
          </>
        ) : (
          <div className="muted small">{t("Execution plan will appear after first save")}</div>
        )}
      </div>

      {state.mode === "edit" && form.last_status ? (
        <div className="backup-summary-grid compact">
          <SummaryCard title={t("Last status")} value={form.last_status} />
          <SummaryCard title={t("Last size")} value={formatBytes(form.last_size_bytes || 0)} />
          <SummaryCard title={t("Last success")} value={form.last_success_at ? new Date(form.last_success_at).toLocaleString() : "-"} />
          <SummaryCard title={t("Last duration")} value={formatDuration(form.duration_ms || 0)} />
        </div>
      ) : null}

      <div className="actions">
        <button type="button" className="secondary" onClick={onClose}>{t("Close")}</button>
        <button type="button" onClick={onSave} disabled={Boolean(state.busy)}>
          {state.busy ? t("Loading...") : t("Save")}
        </button>
      </div>
    </Modal>
  );
}
