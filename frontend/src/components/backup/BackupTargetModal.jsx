import React from "react";
import { Modal, createTargetForm, STORAGE_TYPE_OPTIONS, defaultPortForStorage, humanStatus } from "./shared.jsx";

function buildFormFromTarget(target) {
  const cfg = target?.config || {};
  return {
    id: target?.id || "",
    name: target?.name || "",
    type: target?.type || "sftp",
    enabled: target?.enabled !== false,
    host: cfg.host || "",
    port: cfg.port || defaultPortForStorage(target?.type || "sftp"),
    username: cfg.username || "",
    password: "",
    base_path: cfg.base_path || "",
    timeout_sec: cfg.timeout_sec || 30,
    passive_mode: Boolean(cfg.passive_mode),
    insecure_skip_verify: Boolean(cfg.insecure_skip_verify),
    auth_method: cfg.auth_method || (target?.type === "sftp" ? "password" : ""),
    private_key_pem: "",
    url: cfg.url || "",
    bucket: cfg.bucket || "",
    region: cfg.region || "",
    access_key: cfg.access_key || "",
    secret_key: "",
    use_ssl: Boolean(cfg.use_ssl),
    path_style: Boolean(cfg.path_style),
    local_path: cfg.local_path || "",
  };
}

export function openTargetEditorState(target = null) {
  if (!target) {
    return { open: true, mode: "create", form: createTargetForm(), busy: false, error: "", result: null };
  }
  return { open: true, mode: "edit", form: buildFormFromTarget(target), busy: false, error: "", result: null };
}

export default function BackupTargetModal({ t, state, onClose, onChange, onSave, onTest }) {
  if (!state?.open) return null;

  function patchForm(patch) {
    onChange({ ...state, form: { ...state.form, ...patch }, result: null });
  }

  function changeType(nextType) {
    onChange({
      ...state,
      form: {
        ...createTargetForm(nextType),
        ...state.form,
        type: nextType,
        port: state.form.port || defaultPortForStorage(nextType),
      },
      result: null,
    });
  }

  const form = state.form || createTargetForm();

  return (
    <Modal
      title={state.mode === "create" ? t("Create storage target") : t("Edit storage target")}
      subtitle={t("Secrets stay masked and are never returned to the UI")}
      wide
      onClose={onClose}
    >
      {state.error ? <div className="error">{state.error}</div> : null}
      {state.result ? (
        <div className="hint">
          {t("Test result: {status}", { status: humanStatus(state.result.last_test_status) })}
          {state.result.last_test_error ? ` — ${state.result.last_test_error}` : ""}
        </div>
      ) : null}
      <div className="form-grid">
        <label>
          <span>{t("Name")}</span>
          <input value={form.name} onChange={(event) => patchForm({ name: event.target.value })} />
        </label>
        <label>
          <span>{t("Type")}</span>
          <select value={form.type} onChange={(event) => changeType(event.target.value)}>
            {STORAGE_TYPE_OPTIONS.map((item) => (
              <option key={item.value} value={item.value}>{item.label}</option>
            ))}
          </select>
        </label>
        <label className="checkbox">
          <input type="checkbox" checked={form.enabled} onChange={(event) => patchForm({ enabled: event.target.checked })} />
          <span>{t("Enabled")}</span>
        </label>
        <label>
          <span>{t("Base path")}</span>
          <input value={form.base_path} onChange={(event) => patchForm({ base_path: event.target.value })} placeholder="/backups" />
        </label>
        <label>
          <span>{t("Timeout (sec)")}</span>
          <input type="number" min="1" value={form.timeout_sec} onChange={(event) => patchForm({ timeout_sec: event.target.value })} />
        </label>

        {(form.type === "ftp" || form.type === "ftps" || form.type === "sftp") && (
          <>
            <label>
              <span>{t("Host")}</span>
              <input value={form.host} onChange={(event) => patchForm({ host: event.target.value })} />
            </label>
            <label>
              <span>{t("Port")}</span>
              <input type="number" min="1" value={form.port} onChange={(event) => patchForm({ port: event.target.value })} />
            </label>
            <label>
              <span>{t("Username")}</span>
              <input value={form.username} onChange={(event) => patchForm({ username: event.target.value })} />
            </label>
            {form.type === "sftp" ? (
              <label>
                <span>{t("Auth method")}</span>
                <select value={form.auth_method} onChange={(event) => patchForm({ auth_method: event.target.value })}>
                  <option value="password">{t("Password")}</option>
                  <option value="key">{t("Private key")}</option>
                </select>
              </label>
            ) : null}
            {form.type === "sftp" && form.auth_method === "key" ? (
              <label>
                <span>{t("Private key PEM")}</span>
                <textarea rows="6" value={form.private_key_pem} onChange={(event) => patchForm({ private_key_pem: event.target.value })} placeholder={t("Leave blank to keep existing key")} />
              </label>
            ) : (
              <label>
                <span>{t("Password")}</span>
                <input type="password" value={form.password} onChange={(event) => patchForm({ password: event.target.value })} placeholder={t("Leave blank to keep existing secret")} />
              </label>
            )}
          </>
        )}

        {(form.type === "ftp" || form.type === "ftps") && (
          <>
            <label className="checkbox">
              <input type="checkbox" checked={form.passive_mode} onChange={(event) => patchForm({ passive_mode: event.target.checked })} />
              <span>{t("Passive mode")}</span>
            </label>
            <label className="checkbox">
              <input type="checkbox" checked={form.insecure_skip_verify} onChange={(event) => patchForm({ insecure_skip_verify: event.target.checked })} />
              <span>{t("Skip TLS verification")}</span>
            </label>
          </>
        )}

        {form.type === "webdav" && (
          <>
            <label>
              <span>{t("URL")}</span>
              <input value={form.url} onChange={(event) => patchForm({ url: event.target.value })} placeholder="https://example.com/remote.php/dav/files/admin" />
            </label>
            <label>
              <span>{t("Username")}</span>
              <input value={form.username} onChange={(event) => patchForm({ username: event.target.value })} />
            </label>
            <label>
              <span>{t("Password")}</span>
              <input type="password" value={form.password} onChange={(event) => patchForm({ password: event.target.value })} placeholder={t("Leave blank to keep existing secret")} />
            </label>
            <label className="checkbox">
              <input type="checkbox" checked={form.insecure_skip_verify} onChange={(event) => patchForm({ insecure_skip_verify: event.target.checked })} />
              <span>{t("Skip TLS verification")}</span>
            </label>
          </>
        )}

        {form.type === "s3" && (
          <>
            <label>
              <span>{t("Host")}</span>
              <input value={form.host} onChange={(event) => patchForm({ host: event.target.value })} placeholder="s3.example.com" />
            </label>
            <label>
              <span>{t("Bucket")}</span>
              <input value={form.bucket} onChange={(event) => patchForm({ bucket: event.target.value })} />
            </label>
            <label>
              <span>{t("Region")}</span>
              <input value={form.region} onChange={(event) => patchForm({ region: event.target.value })} />
            </label>
            <label>
              <span>{t("Access key")}</span>
              <input value={form.access_key} onChange={(event) => patchForm({ access_key: event.target.value })} />
            </label>
            <label>
              <span>{t("Secret key")}</span>
              <input type="password" value={form.secret_key} onChange={(event) => patchForm({ secret_key: event.target.value })} placeholder={t("Leave blank to keep existing secret")} />
            </label>
            <label className="checkbox">
              <input type="checkbox" checked={form.use_ssl} onChange={(event) => patchForm({ use_ssl: event.target.checked })} />
              <span>{t("Use SSL")}</span>
            </label>
            <label className="checkbox">
              <input type="checkbox" checked={form.path_style} onChange={(event) => patchForm({ path_style: event.target.checked })} />
              <span>{t("Path style")}</span>
            </label>
          </>
        )}

        {form.type === "local" && (
          <label>
            <span>{t("Local path")}</span>
            <input value={form.local_path} onChange={(event) => patchForm({ local_path: event.target.value })} placeholder="/mnt/backups" />
          </label>
        )}
      </div>
      <div className="actions">
        {state.mode === "edit" ? (
          <button type="button" className="secondary" onClick={onTest} disabled={state.busy}>{t("Test connection")}</button>
        ) : null}
        <button type="button" className="secondary" onClick={onClose}>{t("Close")}</button>
        <button type="button" onClick={onSave} disabled={state.busy}>{state.busy ? t("Loading...") : t("Save")}</button>
      </div>
    </Modal>
  );
}
