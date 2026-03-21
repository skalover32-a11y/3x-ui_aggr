import React, { useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useI18n } from "../../i18n.js";
import {
  listOrgScopedNodes,
  listBackupStorageTargets,
  createBackupStorageTarget,
  updateBackupStorageTarget,
  deleteBackupStorageTarget,
  testBackupStorageTarget,
  listBackupJobs,
  getBackupJob,
  createBackupJob,
  updateBackupJob,
  deleteBackupJob,
  runBackupJob,
  enableBackupJob,
  disableBackupJob,
  createBackupSource,
  updateBackupSource,
  deleteBackupSource,
  reorderBackupSources,
  listBackupRuns,
  getBackupRun,
  getBackupRunLog,
  retryBackupRun,
  cancelBackupRun,
  listBackupTemplates,
  createBackupJobFromTemplate,
  listBackupCatalogCommonPaths,
  listBackupCatalogDockerVolumes,
  listBackupCatalogSystemDetected,
  listBackupCatalogPostgresContainers,
} from "../../api.js";
import BackupTargetModal, { openTargetEditorState } from "./BackupTargetModal.jsx";
import BackupJobModal, { openJobEditorState } from "./BackupJobModal.jsx";
import BackupSourceModal, { emptySourceEditorState, openSourceEditorState } from "./BackupSourceModal.jsx";
import BackupRunModal, { emptyRunViewerState } from "./BackupRunModal.jsx";
import BackupTemplateModal, { openTemplateApplyState } from "./BackupTemplateModal.jsx";
import {
  TABS,
  SummaryCard,
  StatusPill,
  createJobForm,
  formatTS,
  formatBytes,
  formatDuration,
  formatCronPreview,
  normalizeSourceForForm,
  parseTemplateDefinition,
  summarizeTarget,
  summarizeSource,
  storageTypeLabel,
  sourceTypeLabel,
} from "./shared.jsx";

function tmpID() {
  return `tmp:${Date.now()}:${Math.random().toString(36).slice(2, 8)}`;
}

function isTempID(value) {
  return String(value || "").startsWith("tmp:");
}

function currentTab(search) {
  const value = new URLSearchParams(search).get("tab") || "jobs";
  return TABS.includes(value) ? value : "jobs";
}

function currentRunFilters(search) {
  const params = new URLSearchParams(search);
  return {
    jobId: params.get("job") || "",
    status: params.get("status") || "",
  };
}

function buildTargetPayload(form, isEdit) {
  const payload = {
    name: (form.name || "").trim(),
    type: form.type || "sftp",
    enabled: form.enabled !== false,
    base_path: (form.base_path || "").trim(),
    timeout_sec: Number(form.timeout_sec || 30),
    passive_mode: Boolean(form.passive_mode),
    insecure_skip_verify: Boolean(form.insecure_skip_verify),
    auth_method: (form.auth_method || "").trim(),
    use_ssl: Boolean(form.use_ssl),
    path_style: Boolean(form.path_style),
  };
  if (form.type === "ftp" || form.type === "ftps" || form.type === "sftp") {
    payload.host = (form.host || "").trim();
    payload.port = Number(form.port || 0);
    payload.username = (form.username || "").trim();
  }
  if (form.type === "ftp" || form.type === "ftps") {
    if ((form.password || "").trim() || !isEdit) payload.password = form.password || "";
  }
  if (form.type === "sftp") {
    payload.auth_method = form.auth_method || "password";
    if (payload.auth_method === "key") {
      if ((form.private_key_pem || "").trim() || !isEdit) payload.private_key_pem = form.private_key_pem || "";
    } else if ((form.password || "").trim() || !isEdit) {
      payload.password = form.password || "";
    }
  }
  if (form.type === "webdav") {
    payload.url = (form.url || "").trim();
    payload.username = (form.username || "").trim();
    if ((form.password || "").trim() || !isEdit) payload.password = form.password || "";
  }
  if (form.type === "s3") {
    payload.host = (form.host || "").trim();
    payload.bucket = (form.bucket || "").trim();
    payload.region = (form.region || "").trim();
    payload.access_key = (form.access_key || "").trim();
    if ((form.secret_key || "").trim() || !isEdit) payload.secret_key = form.secret_key || "";
  }
  if (form.type === "local") payload.local_path = (form.local_path || "").trim();
  return payload;
}

function buildJobPayload(form) {
  return {
    name: (form.name || "").trim(),
    description: form.description || "",
    node_id: form.node_id || "",
    enabled: form.enabled !== false,
    timezone: (form.timezone || "").trim(),
    cron_expression: (form.cron_expression || "").trim(),
    retention_days: Number(form.retention_days || 14),
    storage_target_id: form.storage_target_id || "",
    compression_enabled: form.compression_enabled !== false,
    compression_level: form.compression_enabled === false ? null : Number(form.compression_level || 6),
    upload_concurrency: Number(form.upload_concurrency || 2),
  };
}

function buildSourcePayload(source, index) {
  return {
    name: (source.name || "").trim(),
    type: source.type,
    enabled: source.enabled !== false,
    order_index: index,
    config: source.config || {},
  };
}

function normalizeJobForm(job) {
  return {
    id: job.id || "",
    name: job.name || "",
    description: job.description || "",
    node_id: job.node_id || "",
    enabled: job.enabled !== false,
    timezone: job.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
    cron_expression: job.cron_expression || "0 3 * * *",
    retention_days: Number(job.retention_days || 14),
    storage_target_id: job.storage_target_id || "",
    compression_enabled: job.compression_enabled !== false,
    compression_level: Number(job.compression_level || 6),
    upload_concurrency: Number(job.upload_concurrency || 2),
    last_status: job.last_status || "",
    last_success_at: job.last_success_at || "",
    last_size_bytes: Number(job.last_size_bytes || 0),
    last_error: job.last_error || "",
  };
}

export default function BackupCenterView() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();

  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const [nodesBusy, setNodesBusy] = useState(false);
  const [targetsBusy, setTargetsBusy] = useState(false);
  const [jobsBusy, setJobsBusy] = useState(false);
  const [runsBusy, setRunsBusy] = useState(false);
  const [templatesBusy, setTemplatesBusy] = useState(false);

  const [nodes, setNodes] = useState([]);
  const [targets, setTargets] = useState([]);
  const [jobs, setJobs] = useState([]);
  const [runs, setRuns] = useState([]);
  const [templates, setTemplates] = useState([]);

  const [targetEditor, setTargetEditor] = useState({ open: false, mode: "create", form: null, busy: false, error: "", result: null });
  const [jobEditor, setJobEditor] = useState({ open: false, mode: "create", form: null, sources: [], initialSources: [], busy: false, loading: false, error: "", plan: null });
  const [sourceEditor, setSourceEditor] = useState(emptySourceEditorState());
  const [runViewer, setRunViewer] = useState(emptyRunViewerState());
  const [templateApply, setTemplateApply] = useState({ open: false, template: null, busy: false, error: "", form: null });

  const [catalogs, setCatalogs] = useState({
    nodeId: "",
    loaded: false,
    busy: false,
    error: "",
    commonPaths: [],
    dockerVolumes: [],
    postgresContainers: [],
    systemDetected: [],
  });

  const activeTab = useMemo(() => currentTab(location.search), [location.search]);
  const runFilters = useMemo(() => currentRunFilters(location.search), [location.search]);
  const nodesById = useMemo(() => Object.fromEntries(nodes.map((node) => [node.id, node])), [nodes]);
  const targetsById = useMemo(() => Object.fromEntries(targets.map((target) => [target.id, target])), [targets]);
  const jobsById = useMemo(() => Object.fromEntries(jobs.map((job) => [job.id, job])), [jobs]);

  const summary = useMemo(() => {
    const enabledJobs = jobs.filter((job) => job.enabled).length;
    const running = runs.filter((run) => run.status === "running" || run.status === "queued").length;
    const latestSuccess = runs.find((run) => run.status === "success");
    const healthyTargets = targets.filter((target) => target.last_test_status === "success").length;
    return { enabledJobs, running, latestSuccess, healthyTargets };
  }, [jobs, runs, targets]);

  useEffect(() => {
    loadNodes();
    loadTargets();
    loadJobs();
    loadTemplates();
    loadRuns();
    loadCatalogs("");
  }, []);

  useEffect(() => {
    loadRuns();
  }, [runFilters.jobId, runFilters.status]);

  useEffect(() => {
    if (!sourceEditor.open) return;
    loadCatalogs(jobEditor.form?.node_id || "");
  }, [sourceEditor.open, jobEditor.form?.node_id]);

  async function loadNodes() {
    setNodesBusy(true);
    try {
      const data = await listOrgScopedNodes();
      setNodes(Array.isArray(data) ? data : []);
    } catch (err) {
      setError(err.message);
    } finally {
      setNodesBusy(false);
    }
  }

  async function loadTargets() {
    setTargetsBusy(true);
    try {
      const data = await listBackupStorageTargets();
      setTargets(Array.isArray(data) ? data : []);
    } catch (err) {
      setError(err.message);
    } finally {
      setTargetsBusy(false);
    }
  }

  async function loadJobs() {
    setJobsBusy(true);
    try {
      const data = await listBackupJobs();
      setJobs(Array.isArray(data) ? data : []);
    } catch (err) {
      setError(err.message);
    } finally {
      setJobsBusy(false);
    }
  }

  async function loadRuns() {
    setRunsBusy(true);
    try {
      const data = await listBackupRuns({
        job_id: runFilters.jobId || undefined,
        status: runFilters.status || undefined,
        limit: 100,
      });
      setRuns(Array.isArray(data) ? data : []);
    } catch (err) {
      setError(err.message);
    } finally {
      setRunsBusy(false);
    }
  }

  async function loadTemplates() {
    setTemplatesBusy(true);
    try {
      const data = await listBackupTemplates();
      setTemplates(Array.isArray(data) ? data : []);
    } catch (err) {
      setError(err.message);
    } finally {
      setTemplatesBusy(false);
    }
  }

  async function loadCatalogs(nodeId, force = false) {
    const normalizedNodeId = nodeId || "";
    if (!force && catalogs.loaded && catalogs.nodeId === normalizedNodeId) {
      return;
    }
    setCatalogs((prev) => ({ ...prev, nodeId: normalizedNodeId, busy: true, error: "" }));
    let catalogError = "";
    try {
      const commonPromise = listBackupCatalogCommonPaths().catch((err) => {
        catalogError = err.message;
        return [];
      });
      const remotePromises = normalizedNodeId
        ? Promise.all([
            listBackupCatalogDockerVolumes(normalizedNodeId).catch((err) => {
              catalogError = catalogError || err.message;
              return [];
            }),
            listBackupCatalogPostgresContainers(normalizedNodeId).catch((err) => {
              catalogError = catalogError || err.message;
              return [];
            }),
            listBackupCatalogSystemDetected(normalizedNodeId).catch((err) => {
              catalogError = catalogError || err.message;
              return [];
            }),
          ])
        : Promise.resolve([[], [], []]);
      const [commonPaths, remote] = await Promise.all([commonPromise, remotePromises]);
      const [dockerVolumes, postgresContainers, systemDetected] = remote;
      setCatalogs({
        nodeId: normalizedNodeId,
        loaded: true,
        busy: false,
        error: catalogError,
        commonPaths: Array.isArray(commonPaths) ? commonPaths : [],
        dockerVolumes: Array.isArray(dockerVolumes) ? dockerVolumes : [],
        postgresContainers: Array.isArray(postgresContainers) ? postgresContainers : [],
        systemDetected: Array.isArray(systemDetected) ? systemDetected : [],
      });
    } catch (err) {
      setCatalogs({
        nodeId: normalizedNodeId,
        loaded: true,
        busy: false,
        error: err.message,
        commonPaths: [],
        dockerVolumes: [],
        postgresContainers: [],
        systemDetected: [],
      });
    }
  }

  function setTab(tab, extras = {}) {
    const params = new URLSearchParams(location.search);
    params.set("tab", tab);
    if (tab !== "runs") {
      params.delete("job");
      params.delete("status");
    }
    if (Object.prototype.hasOwnProperty.call(extras, "jobId")) {
      if (extras.jobId) params.set("job", extras.jobId);
      else params.delete("job");
    }
    if (Object.prototype.hasOwnProperty.call(extras, "status")) {
      if (extras.status) params.set("status", extras.status);
      else params.delete("status");
    }
    navigate(`/backup?${params.toString()}`);
  }
  function openCreateTarget() {
    setTargetEditor(openTargetEditorState());
  }

  function openEditTarget(target) {
    setTargetEditor(openTargetEditorState(target));
  }

  async function saveTargetEditor() {
    setTargetEditor((prev) => ({ ...prev, busy: true, error: "" }));
    try {
      const payload = buildTargetPayload(targetEditor.form, targetEditor.mode === "edit");
      if (targetEditor.mode === "edit" && targetEditor.form?.id) {
        await updateBackupStorageTarget(targetEditor.form.id, payload);
        setMessage(t("Storage target updated"));
      } else {
        await createBackupStorageTarget(payload);
        setMessage(t("Storage target created"));
      }
      setTargetEditor({ open: false, mode: "create", form: null, busy: false, error: "", result: null });
      await loadTargets();
    } catch (err) {
      setTargetEditor((prev) => ({ ...prev, busy: false, error: err.message }));
    }
  }

  async function testTargetFromEditor() {
    if (!targetEditor.form?.id) return;
    setTargetEditor((prev) => ({ ...prev, busy: true, error: "", result: null }));
    try {
      const result = await testBackupStorageTarget(targetEditor.form.id);
      setTargetEditor((prev) => ({ ...prev, busy: false, result }));
      await loadTargets();
    } catch (err) {
      setTargetEditor((prev) => ({ ...prev, busy: false, error: err.message }));
    }
  }

  async function testTargetRow(target) {
    try {
      const result = await testBackupStorageTarget(target.id);
      setMessage(`${target.name}: ${result.last_test_status || "unknown"}`);
      await loadTargets();
    } catch (err) {
      setError(err.message);
    }
  }

  async function removeTarget(target) {
    if (!window.confirm(t("Delete storage target?"))) return;
    try {
      await deleteBackupStorageTarget(target.id);
      setMessage(t("Storage target deleted"));
      await loadTargets();
      await loadJobs();
    } catch (err) {
      setError(err.message);
    }
  }

  function openCreateJob() {
    setJobEditor(
      openJobEditorState({
        mode: "create",
        form: {
          ...createJobForm({
            timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
            storage_target_id: targets[0]?.id || "",
          }),
        },
        sources: [],
        initialSources: [],
        plan: null,
      })
    );
  }

  async function openEditJob(job) {
    setJobEditor({ open: true, mode: "edit", loading: true, busy: false, error: "", form: normalizeJobForm(job), sources: [], initialSources: [], plan: null });
    try {
      const data = await getBackupJob(job.id);
      const normalizedSources = (data.sources || []).map((source) => normalizeSourceForForm(source));
      setJobEditor({
        open: true,
        mode: "edit",
        loading: false,
        busy: false,
        error: "",
        form: normalizeJobForm(data.job || job),
        sources: normalizedSources,
        initialSources: normalizedSources.map((source) => ({ ...source })),
        plan: data.plan || null,
      });
    } catch (err) {
      setJobEditor((prev) => ({ ...prev, loading: false, error: err.message }));
    }
  }

  async function refreshJobPlan() {
    if (!jobEditor.form?.id) return;
    setJobEditor((prev) => ({ ...prev, loading: true, error: "" }));
    try {
      const data = await getBackupJob(jobEditor.form.id);
      setJobEditor((prev) => ({
        ...prev,
        loading: false,
        form: normalizeJobForm(data.job || prev.form),
        plan: data.plan || null,
      }));
    } catch (err) {
      setJobEditor((prev) => ({ ...prev, loading: false, error: err.message }));
    }
  }

  function openCreateSource() {
    loadCatalogs(jobEditor.form?.node_id || "");
    setSourceEditor(openSourceEditorState());
  }

  function openEditSource(source) {
    loadCatalogs(jobEditor.form?.node_id || "");
    setSourceEditor(openSourceEditorState(source));
  }

  function saveSourceEditor() {
    const form = sourceEditor.form;
    if (!form) return;
    if (!(form.name || "").trim()) {
      setSourceEditor((prev) => ({ ...prev, error: t("Name is required") }));
      return;
    }
    setJobEditor((prev) => {
      const nextSources = [...(prev.sources || [])];
      if (sourceEditor.mode === "edit" && form.id) {
        const index = nextSources.findIndex((source) => source.id === form.id || source._tmpId === form.id);
        if (index >= 0) nextSources[index] = { ...form, order_index: index };
      } else {
        const tempID = tmpID();
        nextSources.push({ ...form, id: tempID, _tmpId: tempID, order_index: nextSources.length });
      }
      return { ...prev, sources: nextSources.map((source, index) => ({ ...source, order_index: index })) };
    });
    setSourceEditor(emptySourceEditorState());
  }

  function removeSourceLocal(sourceId) {
    setJobEditor((prev) => ({
      ...prev,
      sources: (prev.sources || [])
        .filter((source) => (source.id || source._tmpId) !== sourceId)
        .map((source, index) => ({ ...source, order_index: index })),
    }));
  }

  function moveSourceLocal(sourceId, direction) {
    setJobEditor((prev) => {
      const current = [...(prev.sources || [])];
      const index = current.findIndex((source) => (source.id || source._tmpId) === sourceId);
      if (index < 0) return prev;
      const nextIndex = index + direction;
      if (nextIndex < 0 || nextIndex >= current.length) return prev;
      const next = [...current];
      const [item] = next.splice(index, 1);
      next.splice(nextIndex, 0, item);
      return { ...prev, sources: next.map((source, idx) => ({ ...source, order_index: idx })) };
    });
  }

  function toggleSourceEnabledLocal(sourceId) {
    setJobEditor((prev) => ({
      ...prev,
      sources: (prev.sources || []).map((source) => {
        if ((source.id || source._tmpId) !== sourceId) return source;
        return { ...source, enabled: source.enabled === false };
      }),
    }));
  }

  async function saveJobEditor() {
    const form = jobEditor.form;
    if (!form) return;
    setJobEditor((prev) => ({ ...prev, busy: true, error: "" }));
    try {
      const payload = buildJobPayload(form);
      let jobID = form.id;
      if (jobEditor.mode === "edit" && jobID) {
        await updateBackupJob(jobID, payload);
      } else {
        const created = await createBackupJob(payload);
        jobID = created.id;
      }

      const keptExisting = new Set();
      const orderedIDs = [];
      for (let index = 0; index < (jobEditor.sources || []).length; index += 1) {
        const source = jobEditor.sources[index];
        const sourcePayload = buildSourcePayload(source, index);
        if (source.id && !isTempID(source.id)) {
          const updated = await updateBackupSource(source.id, sourcePayload);
          keptExisting.add(source.id);
          orderedIDs.push(updated.id || source.id);
        } else {
          const created = await createBackupSource(jobID, sourcePayload);
          orderedIDs.push(created.id);
        }
      }

      for (const source of jobEditor.initialSources || []) {
        if (source.id && !keptExisting.has(source.id)) {
          await deleteBackupSource(source.id);
        }
      }
      if (orderedIDs.length > 0) {
        await reorderBackupSources(jobID, orderedIDs);
      }

      setMessage(jobEditor.mode === "edit" ? t("Backup job updated") : t("Backup job created"));
      setJobEditor({ open: false, mode: "create", form: null, sources: [], initialSources: [], busy: false, loading: false, error: "", plan: null });
      await loadJobs();
      await loadRuns();
    } catch (err) {
      setJobEditor((prev) => ({ ...prev, busy: false, error: err.message }));
    }
  }

  async function runJob(job) {
    try {
      await runBackupJob(job.id);
      setMessage(t("Backup run queued"));
      setTab("runs", { jobId: job.id });
      await loadJobs();
      await loadRuns();
    } catch (err) {
      setError(err.message);
    }
  }

  async function toggleJobEnabled(job) {
    try {
      if (job.enabled) await disableBackupJob(job.id);
      else await enableBackupJob(job.id);
      await loadJobs();
    } catch (err) {
      setError(err.message);
    }
  }

  async function removeJob(job) {
    if (!window.confirm(t("Delete backup job?"))) return;
    try {
      await deleteBackupJob(job.id);
      setMessage(t("Backup job deleted"));
      await loadJobs();
      await loadRuns();
    } catch (err) {
      setError(err.message);
    }
  }
  async function openRun(run) {
    setRunViewer({ open: true, busy: true, error: "", run: null, items: [], log: "" });
    try {
      const [detail, log] = await Promise.all([
        getBackupRun(run.id),
        getBackupRunLog(run.id).catch(() => ({ content: "" })),
      ]);
      setRunViewer({
        open: true,
        busy: false,
        error: "",
        run: detail.run || run,
        items: Array.isArray(detail.items) ? detail.items : [],
        log: log.content || "",
      });
    } catch (err) {
      setRunViewer({ open: true, busy: false, error: err.message, run: run || null, items: [], log: "" });
    }
  }

  async function retryRunCurrent() {
    if (!runViewer.run?.id) return;
    try {
      await retryBackupRun(runViewer.run.id);
      setMessage(t("Retry queued"));
      setRunViewer(emptyRunViewerState());
      await loadRuns();
      await loadJobs();
    } catch (err) {
      setRunViewer((prev) => ({ ...prev, error: err.message }));
    }
  }

  async function cancelRunCurrent() {
    if (!runViewer.run?.id) return;
    try {
      await cancelBackupRun(runViewer.run.id);
      setMessage(t("Cancellation requested"));
      await openRun(runViewer.run);
      await loadRuns();
    } catch (err) {
      setRunViewer((prev) => ({ ...prev, error: err.message }));
    }
  }

  function openTemplate(template) {
    setTemplateApply(openTemplateApplyState(template, { storage_target_id: targets[0]?.id || "" }));
  }

  async function applyTemplate() {
    if (!templateApply.template || !templateApply.form) return;
    setTemplateApply((prev) => ({ ...prev, busy: true, error: "" }));
    try {
      const payload = {
        template_id: templateApply.template.id,
        ...buildJobPayload(templateApply.form),
      };
      await createBackupJobFromTemplate(payload);
      setMessage(t("Backup job created from template"));
      setTemplateApply({ open: false, template: null, busy: false, error: "", form: null });
      setTab("jobs");
      await loadJobs();
    } catch (err) {
      setTemplateApply((prev) => ({ ...prev, busy: false, error: err.message }));
    }
  }

  const filteredRuns = runs;

  return (
    <div className="page page-wide backup-center-page">
      <div className="page-header">
        <div>
          <h1>{t("Backup Center")}</h1>
          <div className="muted small">{t("Jobs, storage targets, execution runs and reusable presets for production backups")}</div>
        </div>
        <div className="page-actions">
          <button type="button" className={activeTab === "jobs" ? "" : "secondary"} onClick={() => setTab("jobs")}>{t("Jobs")}</button>
          <button type="button" className={activeTab === "targets" ? "" : "secondary"} onClick={() => setTab("targets")}>{t("Storage Targets")}</button>
          <button type="button" className={activeTab === "runs" ? "" : "secondary"} onClick={() => setTab("runs")}>{t("Runs")}</button>
          <button type="button" className={activeTab === "templates" ? "" : "secondary"} onClick={() => setTab("templates")}>{t("Templates")}</button>
          {activeTab === "jobs" && <button type="button" onClick={openCreateJob}>{t("Add job")}</button>}
          {activeTab === "targets" && <button type="button" onClick={openCreateTarget}>{t("Add storage")}</button>}
          <button
            type="button"
            className="secondary"
            onClick={() => {
              if (activeTab === "jobs") loadJobs();
              if (activeTab === "targets") loadTargets();
              if (activeTab === "runs") loadRuns();
              if (activeTab === "templates") loadTemplates();
            }}
            disabled={jobsBusy || targetsBusy || runsBusy || templatesBusy || nodesBusy}
          >
            {t("Refresh")}
          </button>
        </div>
      </div>

      {error ? <div className="error">{error}</div> : null}
      {message ? <div className="hint">{message}</div> : null}

      <div className="backup-summary-grid">
        <SummaryCard title={t("Jobs")} value={String(jobs.length)} note={t("{count} enabled", { count: summary.enabledJobs })} />
        <SummaryCard title={t("Storage targets")} value={String(targets.length)} note={t("{count} healthy", { count: summary.healthyTargets })} />
        <SummaryCard title={t("In progress")} value={String(summary.running)} note={t("Queued or running executions")} />
        <SummaryCard
          title={t("Last success")}
          value={summary.latestSuccess ? formatTS(summary.latestSuccess.finished_at || summary.latestSuccess.started_at) : "-"}
          note={summary.latestSuccess ? jobsById[summary.latestSuccess.job_id]?.name || summary.latestSuccess.job_id : t("No successful runs yet")}
        />
      </div>

      {activeTab === "jobs" && (
        <div className="table-card">
          <div className="data-table backup-jobs-table">
            <div className="data-row head">
              <div>{t("Job")}</div>
              <div>{t("Node")}</div>
              <div>{t("Schedule")}</div>
              <div>{t("Storage")}</div>
              <div>{t("Last status")}</div>
              <div>{t("Last size")}</div>
              <div>{t("Actions")}</div>
            </div>
            {jobs.map((job) => {
              const node = job.node_id ? nodesById[job.node_id] : null;
              const target = targetsById[job.storage_target_id];
              return (
                <div className="data-row" key={job.id}>
                  <div>
                    <div className="node-title">{job.name}</div>
                    <div className="muted small">{job.description || t("No description")}</div>
                    <div className="muted small">{t("{count} sources", { count: Number(job.sources_count || 0) })}</div>
                  </div>
                  <div>
                    <div>{node?.name || t("Local backend host")}</div>
                    <div className="muted small">{node?.ssh_host || node?.base_url || "-"}</div>
                  </div>
                  <div>
                    <div>{formatCronPreview(job.cron_expression, job.timezone, t)}</div>
                    <div className="muted small">{job.enabled ? t("Enabled") : t("Disabled")}</div>
                  </div>
                  <div>
                    <div>{target?.name || "-"}</div>
                    <div className="muted small">{target ? summarizeTarget(target) : "-"}</div>
                  </div>
                  <div className="status-cell">
                    <div className="status-main">
                      <StatusPill value={job.last_status || (job.enabled ? "idle" : "disabled")} />
                    </div>
                    <div className="status-text">{job.last_success_at ? formatTS(job.last_success_at) : t("No successful run yet")}</div>
                    {job.last_error ? <div className="status-error">{job.last_error}</div> : null}
                  </div>
                  <div>{formatBytes(job.last_size_bytes || 0)}</div>
                  <div className="actions">
                    <button type="button" onClick={() => runJob(job)}>{t("Run now")}</button>
                    <button type="button" className="secondary" onClick={() => setTab("runs", { jobId: job.id })}>{t("View runs")}</button>
                    <button type="button" className="secondary" onClick={() => openEditJob(job)}>{t("Edit")}</button>
                    <button type="button" className="secondary" onClick={() => toggleJobEnabled(job)}>{job.enabled ? t("Disable") : t("Enable")}</button>
                    <button type="button" className="danger" onClick={() => removeJob(job)}>{t("Delete")}</button>
                  </div>
                </div>
              );
            })}
            {jobs.length === 0 && !jobsBusy && (
              <div className="data-row backup-empty-row">
                <div className="muted small">{t("No backup jobs configured yet")}</div>
              </div>
            )}
          </div>
        </div>
      )}

      {activeTab === "targets" && (
        <div className="table-card">
          <div className="data-table backup-targets-table">
            <div className="data-row head">
              <div>{t("Storage target")}</div>
              <div>{t("Type")}</div>
              <div>{t("Location")}</div>
              <div>{t("Last test")}</div>
              <div>{t("Actions")}</div>
            </div>
            {targets.map((target) => (
              <div className="data-row" key={target.id}>
                <div>
                  <div className="node-title">{target.name}</div>
                  <div className="muted small">{target.enabled ? t("Enabled") : t("Disabled")}</div>
                </div>
                <div>{storageTypeLabel(target.type, t)}</div>
                <div>
                  <div>{summarizeTarget(target)}</div>
                  <div className="muted small">{target.config?.username || target.config?.access_key || "-"}</div>
                </div>
                <div className="status-cell">
                  <div className="status-main">
                    <StatusPill value={target.last_test_status || (target.enabled ? "idle" : "disabled")} />
                  </div>
                  <div className="status-text">{formatTS(target.last_tested_at)}</div>
                  {target.last_test_error ? <div className="status-error">{target.last_test_error}</div> : null}
                </div>
                <div className="actions">
                  <button type="button" className="secondary" onClick={() => testTargetRow(target)}>{t("Test")}</button>
                  <button type="button" className="secondary" onClick={() => openEditTarget(target)}>{t("Edit")}</button>
                  <button type="button" className="danger" onClick={() => removeTarget(target)}>{t("Delete")}</button>
                </div>
              </div>
            ))}
            {targets.length === 0 && !targetsBusy && (
              <div className="data-row backup-empty-row">
                <div className="muted small">{t("No storage targets configured yet")}</div>
              </div>
            )}
          </div>
        </div>
      )}

      {activeTab === "runs" && (
        <>
          <div className="table-card backup-inline-card">
            <div className="section-head">
              <div>
                <div className="section-title">{t("Run filters")}</div>
                <div className="muted small">{t("Filter by job or status without leaving the page")}</div>
              </div>
              <div className="section-actions backup-filter-row">
                <select value={runFilters.jobId} onChange={(event) => setTab("runs", { jobId: event.target.value, status: runFilters.status })}>
                  <option value="">{t("All jobs")}</option>
                  {jobs.map((job) => (
                    <option key={job.id} value={job.id}>{job.name}</option>
                  ))}
                </select>
                <select value={runFilters.status} onChange={(event) => setTab("runs", { jobId: runFilters.jobId, status: event.target.value })}>
                  <option value="">{t("All statuses")}</option>
                  {["queued", "running", "success", "failed", "partial_success", "cancelled"].map((status) => (
                    <option key={status} value={status}>{t(status.replaceAll("_", " ").replace(/\b\w/g, (m) => m.toUpperCase()))}</option>
                  ))}
                </select>
              </div>
            </div>
          </div>
          <div className="table-card">
            <div className="data-table backup-runs-table">
              <div className="data-row head">
                <div>{t("Run")}</div>
                <div>{t("Job")}</div>
                <div>{t("Started")}</div>
                <div>{t("Duration")}</div>
                <div>{t("Size")}</div>
                <div>{t("Actions")}</div>
              </div>
              {filteredRuns.map((run) => (
                <div className="data-row" key={run.id}>
                  <div className="status-cell">
                    <div className="status-main">
                      <StatusPill value={run.status} />
                    </div>
                    <div className="muted small">{run.trigger_type || "-"}</div>
                    {run.error_summary ? <div className="status-error">{run.error_summary}</div> : null}
                  </div>
                  <div>
                    <div className="node-title">{jobsById[run.job_id]?.name || run.job_id}</div>
                    <div className="muted small">{run.remote_path || "-"}</div>
                  </div>
                  <div>{formatTS(run.started_at)}</div>
                  <div>{formatDuration(run.duration_ms)}</div>
                  <div>{formatBytes(run.total_size_bytes || 0)}</div>
                  <div className="actions">
                    <button type="button" className="secondary" onClick={() => openRun(run)}>{t("Open")}</button>
                    {(run.status === "failed" || run.status === "partial_success" || run.status === "cancelled") && (
                      <button
                        type="button"
                        className="secondary"
                        onClick={() => retryBackupRun(run.id).then(() => { setMessage(t("Retry queued")); loadRuns(); }).catch((err) => setError(err.message))}
                      >
                        {t("Retry")}
                      </button>
                    )}
                    {run.status === "running" && (
                      <button
                        type="button"
                        className="secondary"
                        onClick={() => cancelBackupRun(run.id).then(() => { setMessage(t("Cancellation requested")); loadRuns(); }).catch((err) => setError(err.message))}
                      >
                        {t("Cancel")}
                      </button>
                    )}
                  </div>
                </div>
              ))}
              {filteredRuns.length === 0 && !runsBusy && (
                <div className="data-row backup-empty-row">
                  <div className="muted small">{t("No runs found for current filter")}</div>
                </div>
              )}
            </div>
          </div>
        </>
      )}

      {activeTab === "templates" && (
        <div className="backup-templates-grid">
          {templates.map((template) => {
            const definition = parseTemplateDefinition(template.definition);
            const sources = Array.isArray(definition.sources) ? definition.sources : [];
            return (
              <div className="table-card backup-template-card" key={template.id}>
                <div className="section-head">
                  <div>
                    <div className="section-title">{template.name}</div>
                    <div className="muted small">{template.slug}</div>
                  </div>
                  <button type="button" onClick={() => openTemplate(template)}>{t("Use template")}</button>
                </div>
                <div className="muted small">{template.description || t("No description")}</div>
                <div className="backup-template-count">{t("{count} sources", { count: sources.length })}</div>
                <div className="backup-template-sources">
                  {sources.slice(0, 6).map((source, index) => (
                    <div className="backup-template-source" key={`${source.name || source.type}-${index}`}>
                      <span>{source.name || source.type}</span>
                      <span className="muted small">{sourceTypeLabel(source.type, t)}</span>
                    </div>
                  ))}
                </div>
              </div>
            );
          })}
          {templates.length === 0 && !templatesBusy && (
            <div className="table-card">
              <div className="muted small">{t("No templates available")}</div>
            </div>
          )}
        </div>
      )}

      <BackupTargetModal
        t={t}
        state={targetEditor}
        onClose={() => setTargetEditor({ open: false, mode: "create", form: null, busy: false, error: "", result: null })}
        onChange={setTargetEditor}
        onSave={saveTargetEditor}
        onTest={testTargetFromEditor}
      />

      <BackupJobModal
        t={t}
        state={jobEditor}
        nodes={nodes}
        targets={targets}
        onClose={() => setJobEditor({ open: false, mode: "create", form: null, sources: [], initialSources: [], busy: false, loading: false, error: "", plan: null })}
        onChange={setJobEditor}
        onSave={saveJobEditor}
        onAddSource={openCreateSource}
        onEditSource={openEditSource}
        onRemoveSource={removeSourceLocal}
        onMoveSource={moveSourceLocal}
        onToggleSourceEnabled={toggleSourceEnabledLocal}
        onRefreshPlan={refreshJobPlan}
      />

      <BackupSourceModal
        t={t}
        state={sourceEditor}
        nodeId={jobEditor.form?.node_id || ""}
        catalogs={catalogs}
        onClose={() => setSourceEditor(emptySourceEditorState())}
        onChange={setSourceEditor}
        onSave={saveSourceEditor}
        onRefreshCatalogs={() => loadCatalogs(jobEditor.form?.node_id || "", true)}
      />

      <BackupRunModal
        t={t}
        state={runViewer}
        onClose={() => setRunViewer(emptyRunViewerState())}
        onRetry={retryRunCurrent}
        onCancel={cancelRunCurrent}
      />

      <BackupTemplateModal
        t={t}
        state={templateApply}
        nodes={nodes}
        targets={targets}
        onClose={() => setTemplateApply({ open: false, template: null, busy: false, error: "", form: null })}
        onChange={setTemplateApply}
        onApply={applyTemplate}
      />
    </div>
  );
}
