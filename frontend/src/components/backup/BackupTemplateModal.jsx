import React from "react";
import { Modal, SummaryCard, createJobForm, formatCronPreview, parseTemplateDefinition, sourceTypeLabel, summarizeTarget } from "./shared.jsx";

export function openTemplateApplyState(template, defaults = {}) {
  return {
    open: true,
    template,
    busy: false,
    error: "",
    form: {
      ...createJobForm(defaults),
      name: defaults.name || template?.name || "",
      description: defaults.description || template?.description || "",
      storage_target_id: defaults.storage_target_id || "",
      node_id: defaults.node_id || "",
    },
  };
}

export default function BackupTemplateModal({ t, state, nodes, targets, onClose, onChange, onApply }) {
  if (!state?.open || !state.template) return null;

  const definition = parseTemplateDefinition(state.template.definition);
  const sources = Array.isArray(definition.sources) ? definition.sources : [];
  const form = state.form || createJobForm();
  const selectedTarget = targets.find((item) => item.id === form.storage_target_id);

  function patchForm(patch) {
    onChange({ ...state, form: { ...form, ...patch }, error: "" });
  }

  return (
    <Modal
      title={t("Create job from template")}
      subtitle={state.template.name || state.template.slug || ""}
      wide
      onClose={onClose}
    >
      {state.error ? <div className="error">{state.error}</div> : null}

      <div className="backup-summary-grid">
        <SummaryCard title={t("Template")} value={state.template.name || "-"} note={state.template.slug || ""} />
        <SummaryCard title={t("Sources")} value={String(sources.length)} note={t("Editable after creation")} />
        <SummaryCard title={t("Schedule")} value={formatCronPreview(form.cron_expression, form.timezone)} />
        <SummaryCard title={t("Storage")} value={selectedTarget?.name || "-"} note={selectedTarget ? summarizeTarget(selectedTarget) : t("Select a storage target")} />
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
              <option key={node.id} value={node.id}>{node.name || node.ssh_host || node.id}</option>
            ))}
          </select>
        </label>
        <label>
          <span>{t("Storage target")}</span>
          <select value={form.storage_target_id || ""} onChange={(event) => patchForm({ storage_target_id: event.target.value })}>
            <option value="">{t("Select storage target")}</option>
            {targets.map((target) => (
              <option key={target.id} value={target.id}>{target.name} · {target.type}</option>
            ))}
          </select>
        </label>
        <label className="checkbox">
          <input type="checkbox" checked={form.enabled !== false} onChange={(event) => patchForm({ enabled: event.target.checked })} />
          <span>{t("Enabled")}</span>
        </label>
        <label>
          <span>{t("Timezone")}</span>
          <input value={form.timezone || ""} onChange={(event) => patchForm({ timezone: event.target.value })} />
        </label>
        <label>
          <span>{t("Cron expression")}</span>
          <input value={form.cron_expression || ""} onChange={(event) => patchForm({ cron_expression: event.target.value })} />
        </label>
        <label>
          <span>{t("Retention days")}</span>
          <input type="number" min="1" max="3650" value={form.retention_days || 14} onChange={(event) => patchForm({ retention_days: Number(event.target.value || 14) })} />
        </label>
        <label className="checkbox">
          <input type="checkbox" checked={form.compression_enabled !== false} onChange={(event) => patchForm({ compression_enabled: event.target.checked })} />
          <span>{t("Compression enabled")}</span>
        </label>
        <label>
          <span>{t("Compression level")}</span>
          <input type="number" min="1" max="9" value={form.compression_level || 6} disabled={!form.compression_enabled} onChange={(event) => patchForm({ compression_level: Number(event.target.value || 6) })} />
        </label>
        <label>
          <span>{t("Upload concurrency")}</span>
          <input type="number" min="1" max="8" value={form.upload_concurrency || 2} onChange={(event) => patchForm({ upload_concurrency: Number(event.target.value || 2) })} />
        </label>
        <label className="backup-form-span">
          <span>{t("Description")}</span>
          <textarea rows="3" value={form.description || ""} onChange={(event) => patchForm({ description: event.target.value })} />
        </label>
      </div>

      <div className="table-card backup-inline-card">
        <div className="section-head">
          <div>
            <div className="section-title">{t("Template sources")}</div>
            <div className="muted small">{t("These sources will be copied into the new job and remain editable")}</div>
          </div>
        </div>
        <div className="backup-plan-list">
          {sources.map((source, index) => (
            <div className="backup-plan-row" key={`${source.name || source.type}-${index}`}>
              <div>
                <div className="node-title">{source.name || "-"}</div>
                <div className="muted small">{sourceTypeLabel(source.type)}</div>
              </div>
            </div>
          ))}
        </div>
      </div>

      <div className="actions">
        <button type="button" className="secondary" onClick={onClose}>{t("Close")}</button>
        <button type="button" onClick={onApply} disabled={Boolean(state.busy)}>
          {state.busy ? t("Loading...") : t("Apply template")}
        </button>
      </div>
    </Modal>
  );
}
