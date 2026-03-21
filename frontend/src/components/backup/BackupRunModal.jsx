import React from "react";
import { Modal, SummaryCard, StatusPill, formatBytes, formatDuration, formatTS } from "./shared.jsx";

export function emptyRunViewerState() {
  return { open: false, busy: false, error: "", run: null, items: [], log: "" };
}

export default function BackupRunModal({ t, state, onClose, onRetry, onCancel }) {
  if (!state?.open) return null;

  const run = state.run || {};
  const items = Array.isArray(state.items) ? state.items : [];

  return (
    <Modal
      title={run.id ? t("Backup run details") : t("Run details")}
      subtitle={run.id || ""}
      wide
      onClose={onClose}
    >
      {state.error ? <div className="error">{state.error}</div> : null}
      {state.busy ? <div className="hint">{t("Loading...")}</div> : null}

      {!state.busy && run.id ? (
        <>
          <div className="backup-summary-grid">
            <SummaryCard title={t("Status")} value={<StatusPill value={run.status} />} />
            <SummaryCard title={t("Trigger")} value={run.trigger_type || "-"} />
            <SummaryCard title={t("Duration")} value={formatDuration(run.duration_ms)} />
            <SummaryCard title={t("Files")} value={String(run.file_count || 0)} note={formatBytes(run.total_size_bytes || 0)} />
            <SummaryCard title={t("Uploaded")} value={formatBytes(run.uploaded_size_bytes || 0)} note={run.remote_path || "-"} />
            <SummaryCard title={t("Cleanup")} value={run.cleanup_status || "-"} note={run.checksum_status || "-"} />
          </div>

          <div className="table-card backup-inline-card">
            <div className="backup-detail-grid">
              <div>
                <div className="muted small">{t("Started")}</div>
                <div>{formatTS(run.started_at)}</div>
              </div>
              <div>
                <div className="muted small">{t("Finished")}</div>
                <div>{formatTS(run.finished_at)}</div>
              </div>
              <div>
                <div className="muted small">{t("Remote path")}</div>
                <div className="mono">{run.remote_path || "-"}</div>
              </div>
              <div>
                <div className="muted small">{t("Workdir")}</div>
                <div className="mono">{run.local_workdir || run.remote_workdir || "-"}</div>
              </div>
            </div>
            {run.error_summary ? <div className="error" style={{ marginTop: 12 }}>{run.error_summary}</div> : null}
          </div>

          <div className="table-card backup-inline-card">
            <div className="section-head">
              <div>
                <div className="section-title">{t("Run items")}</div>
                <div className="muted small">{t("Per-source execution results")}</div>
              </div>
            </div>
            <div className="data-table backup-run-items-table">
              <div className="data-row head">
                <div>{t("Item")}</div>
                <div>{t("Type")}</div>
                <div>{t("Status")}</div>
                <div>{t("Size")}</div>
                <div>{t("Finished")}</div>
                <div>{t("Details")}</div>
              </div>
              {items.map((item) => (
                <div className="data-row" key={item.id}>
                  <div>
                    <div className="node-title">{item.logical_name || "-"}</div>
                    <div className="muted small mono">{item.output_file_name || "-"}</div>
                  </div>
                  <div>{item.item_type || "-"}</div>
                  <div><StatusPill value={item.status} /></div>
                  <div>{formatBytes(item.size_bytes || 0)}</div>
                  <div>{formatTS(item.finished_at)}</div>
                  <div className="muted small">{item.error_text || item.remote_source_path || "-"}</div>
                </div>
              ))}
              {items.length === 0 && (
                <div className="data-row backup-run-empty-row">
                  <div className="muted small">{t("No items yet")}</div>
                </div>
              )}
            </div>
          </div>

          <div className="table-card backup-inline-card">
            <div className="section-head">
              <div>
                <div className="section-title">{t("Execution log")}</div>
                <div className="muted small">{t("Persisted backend excerpt and full run log")}</div>
              </div>
            </div>
            <pre className="code-block backup-run-log">{state.log || run.log_excerpt || ""}</pre>
          </div>
        </>
      ) : null}

      <div className="actions">
        {run.status === "running" ? (
          <button type="button" className="secondary" onClick={onCancel}>{t("Cancel")}</button>
        ) : null}
        {run.status === "failed" || run.status === "partial_success" || run.status === "cancelled" ? (
          <button type="button" className="secondary" onClick={onRetry}>{t("Retry")}</button>
        ) : null}
        <button type="button" onClick={onClose}>{t("Close")}</button>
      </div>
    </Modal>
  );
}
