import React, { useEffect, useMemo, useState } from "react";
import { Routes, Route, useNavigate, useLocation, useParams, Link } from "react-router-dom";
import { request, getToken, convertSSHKey, getTelegramSettings, saveTelegramSettings, setAuth, clearAuth, getRole, getUser } from "./api.js";
import InboundEditor from "./components/InboundEditor.jsx";
import NodeSSHModal from "./components/NodeSSHModal.jsx";

function formatTS(ts) {
  if (!ts) return "";
  const date = new Date(ts);
  return date.toLocaleString();
}

function deriveStatus(panelOK, sshOK) {
  if (panelOK && sshOK) return "online";
  if (panelOK || sshOK) return "degraded";
  return "offline";
}

function computeUptime(points) {
  if (!points || points.length === 0) {
    return { percent: 0, success: 0, total: 0 };
  }
  const success = points.filter((p) => p.panel_ok || p.ssh_ok).length;
  const total = points.length;
  const percent = Math.round((success / total) * 1000) / 10;
  return { percent, success, total };
}

function formatBytes(bytes) {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = bytes;
  let idx = 0;
  while (v >= 1024 && idx < units.length-1) {
    v /= 1024;
    idx++;
  }
  return `${v.toFixed(1)} ${units[idx]}`;
}

function StatusBadge({ status }) {
  const label = status || "unknown";
  return <span className={`badge ${label}`}>{label}</span>;
}

function Sparkline({ points }) {
  if (!points || points.length === 0) return <div className="availability empty">no data</div>;
  const first = points[0];
  const last = points[points.length - 1];
  return (
    <div className="availability">
      <div className="availability-bars">
        {points.map((p, idx) => {
          const status = deriveStatus(p.panel_ok, p.ssh_ok);
          const title = `${formatTS(p.ts)} | latency ${p.latency_ms || 0}ms${p.error ? ` | ${p.error}` : ""}`;
          return (
            <span key={`${p.ts}-${idx}`} className={`bar ${status}`} title={title} />
          );
        })}
      </div>
      <div className="availability-meta">
        <span>{formatTS(first?.ts)}</span>
        <span>{formatTS(last?.ts)}</span>
      </div>
    </div>
  );
}

function MetricSparks({ metrics }) {
  if (!metrics || metrics.length === 0) return <div className="metrics empty">no metrics</div>;
  const latest = metrics[metrics.length - 1];
  const memPercents = metrics
    .map((m) => {
      if (!m.mem_total_bytes || !m.mem_available_bytes) return null;
      return Math.max(0, Math.min(100, ((m.mem_total_bytes - m.mem_available_bytes) / m.mem_total_bytes) * 100));
    })
    .filter((v) => v !== null);
  const diskPercents = metrics
    .map((m) => {
      if (!m.disk_total_bytes || !m.disk_used_bytes) return null;
      return Math.max(0, Math.min(100, (m.disk_used_bytes / m.disk_total_bytes) * 100));
    })
    .filter((v) => v !== null);
  const renderBars = (values, className) => (
    <div className="metric-bars">
      {values.slice(-60).map((v, idx) => (
        <span key={`${className}-${idx}`} className={`metric-bar ${className}`} style={{ height: `${8 + (v / 2)}px` }} title={`${v.toFixed(1)}%`} />
      ))}
    </div>
  );
  const memLatest = memPercents.length > 0 ? memPercents[memPercents.length - 1] : null;
  const diskLatest = diskPercents.length > 0 ? diskPercents[diskPercents.length - 1] : null;
  const load1 = latest.load1;

  return (
    <div className="metrics">
      <div className="metric">
        <div className="metric-header">
          <span>CPU Load</span>
          <span className="muted small">{load1 != null ? load1.toFixed(2) : "—"}</span>
        </div>
        {renderBars(metrics.map((m) => (m.load1 != null ? Math.min(m.load1 * 100, 200) : 0)), "cpu")}
      </div>
      <div className="metric">
        <div className="metric-header">
          <span>Memory</span>
          <span className="muted small">
            {memLatest != null ? `${memLatest.toFixed(1)}%` : "—"}
            {latest.mem_total_bytes ? ` / ${formatBytes(latest.mem_total_bytes)}` : ""}
          </span>
        </div>
        {renderBars(memPercents, "mem")}
      </div>
      <div className="metric">
        <div className="metric-header">
          <span>Disk</span>
          <span className="muted small">
            {diskLatest != null ? `${diskLatest.toFixed(1)}%` : "—"}
            {latest.disk_total_bytes ? ` / ${formatBytes(latest.disk_total_bytes)}` : ""}
          </span>
        </div>
        {renderBars(diskPercents, "disk")}
      </div>
    </div>
  );
}

function ValidationBadge({ label, status, detail }) {
  if (!status) return null;
  const cls = status === "ok" ? "badge online" : "badge offline";
  return (
    <span className="validation-badge">
      <span className={cls}>{label}</span>
      {detail && <span className="validation-detail">{detail}</span>}
    </span>
  );
}

function ListInput({ label, values, onChange, placeholder }) {
  const [value, setValue] = useState("");
  return (
    <div className="list-editor">
      <div className="list-label">{label}</div>
      <div className="chips">
        {values.map((item, idx) => (
          <span className="chip" key={`${item}-${idx}`}>
            {item}
            <button type="button" onClick={() => onChange(values.filter((_, i) => i !== idx))}>×</button>
          </span>
        ))}
      </div>
      <div className="list-input">
        <input
          autoComplete="off"
          placeholder={placeholder}
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
        <button
          type="button"
          onClick={() => {
            if (!value.trim()) return;
            onChange([...values, value.trim()]);
            setValue("");
          }}
        >
          Add
        </button>
      </div>
    </div>
  );
}

function RequireAuth({ children }) {
  const navigate = useNavigate();
  const location = useLocation();
  useEffect(() => {
    if (!getToken()) {
      navigate("/login", { replace: true, state: { from: location.pathname } });
    }
  }, [navigate, location]);
  return children;
}

function LoginPage() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [otp, setOtp] = useState("");
  const [recoveryCode, setRecoveryCode] = useState("");
  const [recoveryStatus, setRecoveryStatus] = useState("");
  const [error, setError] = useState("");
  const navigate = useNavigate();

  async function onSubmit(e) {
    e.preventDefault();
    setError("");
    setRecoveryStatus("");
    try {
      const data = await request("POST", "/auth/login", {
        username,
        password,
        otp: otp.trim(),
        recovery_code: recoveryCode.trim(),
      });
      setAuth(data.token, data.role, data.username);
      navigate("/nodes");
    } catch (err) {
      setError(err.message);
    }
  }

  async function onSendRecovery() {
    setError("");
    setRecoveryStatus("");
    try {
      await request("POST", "/auth/2fa/recovery", { username, password });
      setRecoveryStatus("Recovery code sent to Telegram");
    } catch (err) {
      setError(err.message);
    }
  }

  return (
    <div className="page center">
      <form className="card" onSubmit={onSubmit} autoComplete="on">
        <h1>3x-ui Aggregator</h1>
        <label>
          Username
          <input name="username" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} />
        </label>
        <label>
          Password
          <input name="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        <label>
          2FA Code
          <input name="otp" autoComplete="one-time-code" placeholder="123456" value={otp} onChange={(e) => setOtp(e.target.value)} />
        </label>
        <label>
          Recovery code (optional)
          <input name="recovery_code" autoComplete="off" value={recoveryCode} onChange={(e) => setRecoveryCode(e.target.value)} />
        </label>
        <button type="button" className="ghost" onClick={onSendRecovery}>
          Send recovery code via Telegram
        </button>
        {error && <div className="error">{error}</div>}
        {recoveryStatus && <div className="hint">{recoveryStatus}</div>}
        <button type="submit">Login</button>
      </form>
    </div>
  );
}

function NodesPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const role = getRole();
  const user = getUser();
  const isAdmin = role === "admin";
  const isOperator = role === "operator";
  const isViewer = role === "viewer";
  const [nodes, setNodes] = useState([]);
  const [error, setError] = useState("");
  const [keyPassphrase, setKeyPassphrase] = useState("");
  const [keyFingerprint, setKeyFingerprint] = useState("");
  const [statusMap, setStatusMap] = useState({});
  const [uptimeMap, setUptimeMap] = useState({});
  const [metricsMap, setMetricsMap] = useState({});
  const [validation, setValidation] = useState(null);
  const [validating, setValidating] = useState(false);
  const [editValidation, setEditValidation] = useState(null);
  const [editValidating, setEditValidating] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const [auditOpen, setAuditOpen] = useState(false);
  const [auditLogs, setAuditLogs] = useState([]);
  const [auditNodeID, setAuditNodeID] = useState("");
  const [auditOffset, setAuditOffset] = useState(0);
  const [telegramOpen, setTelegramOpen] = useState(false);
  const [telegramForm, setTelegramForm] = useState({
    bot_token: "",
    admin_chat_ids: [],
    alert_connection: true,
    alert_cpu: true,
    alert_memory: true,
    alert_disk: true,
  });
  const [telegramTokenSet, setTelegramTokenSet] = useState(false);
  const [telegramSaved, setTelegramSaved] = useState("");
  const [telegramTestMsg, setTelegramTestMsg] = useState("");
  const [telegramTestStatus, setTelegramTestStatus] = useState("");
  const [telegramTestResults, setTelegramTestResults] = useState([]);
  const [usersOpen, setUsersOpen] = useState(false);
  const [usersDraft, setUsersDraft] = useState({ name: "", role: "operator", password: "" });
  const [usersList, setUsersList] = useState([]);
  const [usersBusy, setUsersBusy] = useState(false);
  const [totpOpen, setTotpOpen] = useState(false);
  const [totpStatus, setTotpStatus] = useState(null);
  const [totpSetup, setTotpSetup] = useState(null);
  const [totpCode, setTotpCode] = useState("");
  const [totpDisableCode, setTotpDisableCode] = useState("");
  const [totpRecoveryCode, setTotpRecoveryCode] = useState("");
  const [totpMessage, setTotpMessage] = useState("");
  const [sshModal, setSshModal] = useState({ open: false, node: null, confirmClose: false });
  const [sshChoice, setSshChoice] = useState({ open: false, node: null });
  const [sshAutoOpened, setSshAutoOpened] = useState("");
  const [collapsedNodes, setCollapsedNodes] = useState({});
  const [actionPlan, setActionPlan] = useState({ open: false, node: null, action: null, steps: [], confirm: "" });
  const [actionBusy, setActionBusy] = useState(false);
  const [editModal, setEditModal] = useState({ open: false, node: null });
  const [form, setForm] = useState({
    name: "",
    tags: "",
    base_url: "",
    panel_username: "",
    panel_password: "",
    ssh_host: "",
    ssh_port: 22,
    ssh_user: "",
    ssh_key: "",
    verify_tls: true,
  });

  async function loadNodes() {
    try {
      const data = await request("GET", "/nodes");
      setNodes(data);
    } catch (err) {
      setError(err.message);
    }
  }

  useEffect(() => {
    loadNodes();
  }, []);

  useEffect(() => {
    if (nodes.length === 0) return;
    setCollapsedNodes((prev) => {
      const next = { ...prev };
      nodes.forEach((node) => {
        if (next[node.id] === undefined) {
          next[node.id] = true;
        }
      });
      return next;
    });
    const params = new URLSearchParams(location.search);
    const sshId = params.get("ssh");
    if (sshId && sshAutoOpened !== sshId) {
      const node = nodes.find((n) => n.id === sshId);
      if (node) {
        setSshModal({ open: true, node, confirmClose: false });
        setSshAutoOpened(sshId);
      }
    }
    const fetchChecks = async () => {
      try {
        const statusEntries = await Promise.all(
          nodes.map((node) => request("GET", `/nodes/${node.id}/status`).catch(() => null))
        );
        const uptimeEntries = await Promise.all(
          nodes.map((node) => request("GET", `/nodes/${node.id}/uptime?minutes=60`).catch(() => []))
        );
        const metricEntries = await Promise.all(
          nodes.map((node) => request("GET", `/nodes/${node.id}/metrics?minutes=720`).catch(() => []))
        );
        const statusNext = {};
        const uptimeNext = {};
        const metricsNext = {};
        nodes.forEach((node, idx) => {
          statusNext[node.id] = statusEntries[idx];
          uptimeNext[node.id] = uptimeEntries[idx] || [];
          metricsNext[node.id] = metricEntries[idx] || [];
        });
        setStatusMap(statusNext);
        setUptimeMap(uptimeNext);
        setMetricsMap(metricsNext);
      } catch {
        // ignore
      }
    };
    fetchChecks();
  }, [nodes, location.search, sshAutoOpened]);

  async function onCreate(e) {
    e.preventDefault();
    setError("");
    try {
      const payload = {
        ...form,
        tags: form.tags ? form.tags.split(",").map((t) => t.trim()).filter(Boolean) : [],
      };
      await request("POST", "/nodes", payload);
      setForm({ ...form, name: "", tags: "" });
      setKeyPassphrase("");
      setKeyFingerprint("");
      setAddOpen(false);
      loadNodes();
    } catch (err) {
      setError(err.message);
    }
  }

  async function onKeyUpload(e) {
    const file = e.target.files?.[0];
    if (!file) return;
    setError("");
    try {
      const data = await convertSSHKey(file, keyPassphrase);
      setForm({ ...form, ssh_key: data.privateKey });
      setKeyFingerprint(data.fingerprint);
    } catch (err) {
      setError(err.message);
    }
  }

  async function validateNodePayload(payload, setResult, setBusy) {
    setBusy(true);
    setResult(null);
    try {
      const res = await request("POST", "/validate/node", payload);
      setResult(res);
    } catch (err) {
      setResult({ error: err.message });
    } finally {
      setBusy(false);
    }
  }

  async function onValidateCreate() {
    const payload = {
      base_url: form.base_url,
      verify_tls: form.verify_tls,
      ssh_host: form.ssh_host,
      ssh_port: form.ssh_port,
      ssh_user: form.ssh_user,
      ssh_key: form.ssh_key,
      ssh_key_passphrase: keyPassphrase || "",
      panel_username: form.panel_username,
      panel_password: form.panel_password,
    };
    validateNodePayload(payload, setValidation, setValidating);
  }

  async function onTest(id) {
    setError("");
    try {
      await request("POST", `/nodes/${id}/test`, {});
      alert("Test OK");
    } catch (err) {
      setError(err?.data?.error || err.message);
    }
  }

  async function onRestart(id) {
    const node = nodes.find((n) => n.id === id);
    if (node) openActionPlan("restart_xray", node);
  }

  async function onReboot(id) {
    const node = nodes.find((n) => n.id === id);
    if (node) openActionPlan("reboot", node);
  }

  function openEdit(node) {
    setEditModal({ open: true, node });
    setEditValidation(null);
    setEditValidating(false);
  }

  function openSSH(node) {
    setSshChoice({ open: true, node });
  }

  async function onUpdate(e) {
    e.preventDefault();
    setError("");
    if (!editModal.node) return;
    const formEl = e.currentTarget;
    const payload = {
      name: formEl.name.value,
      tags: formEl.tags.value ? formEl.tags.value.split(",").map((t) => t.trim()).filter(Boolean) : [],
      base_url: formEl.base_url.value,
      panel_username: formEl.panel_username.value,
      ssh_host: formEl.ssh_host.value,
      ssh_port: Number(formEl.ssh_port.value || 22),
      ssh_user: formEl.ssh_user.value,
      verify_tls: formEl.verify_tls.checked,
    };
    const panelPass = formEl.panel_password.value;
    const sshKey = formEl.ssh_key.value;
    if (panelPass) payload.panel_password = panelPass;
    if (sshKey) payload.ssh_key = sshKey;
    try {
      await request("PATCH", `/nodes/${editModal.node.id}`, payload);
      setEditModal({ open: false, node: null });
      loadNodes();
    } catch (err) {
      setError(err.message);
    }
  }

  async function onValidateEdit(formEl) {
    const payload = {
      base_url: formEl.base_url.value,
      verify_tls: formEl.verify_tls.checked,
      ssh_host: formEl.ssh_host.value,
      ssh_port: Number(formEl.ssh_port.value || 22),
      ssh_user: formEl.ssh_user.value,
      ssh_key: formEl.ssh_key.value,
      panel_username: formEl.panel_username.value,
      panel_password: formEl.panel_password.value,
    };
    validateNodePayload(payload, setEditValidation, setEditValidating);
  }

  async function onDelete(node) {
    openActionPlan("delete_node", node);
  }

  function actionConfirmToken(action) {
    if (action === "reboot") return "REBOOT";
    if (action === "delete_node") return "DELETE";
    return "";
  }

  async function openActionPlan(action, node) {
    setActionBusy(true);
    setError("");
    try {
      const res = await request("POST", `/nodes/${node.id}/actions/${action}/plan`, {});
      setActionPlan({ open: true, node, action, steps: res.steps || [], confirm: "" });
    } catch (err) {
      setError(err.message);
    } finally {
      setActionBusy(false);
    }
  }

  async function runActionPlan() {
    if (!actionPlan.open || !actionPlan.node) return;
    const required = actionConfirmToken(actionPlan.action);
    if (required && actionPlan.confirm.trim() !== required) {
      setError(`Type ${required} to confirm`);
      return;
    }
    setActionBusy(true);
    setError("");
    try {
      const payload = required ? { confirm: required } : {};
      await request("POST", `/nodes/${actionPlan.node.id}/actions/${actionPlan.action}/run`, payload);
      setActionPlan({ open: false, node: null, action: null, steps: [], confirm: "" });
      loadNodes();
    } catch (err) {
      setError(err.message);
    } finally {
      setActionBusy(false);
    }
  }

  function openAddForm() {
    setAddOpen(true);
    setMenuOpen(false);
  }

  async function openAudit() {
    setMenuOpen(false);
    setAuditOpen(true);
    setAuditOffset(0);
    try {
      const data = await request("GET", "/audit?limit=100");
      setAuditLogs(data);
    } catch (err) {
      setError(err.message);
    }
  }

  async function loadUsers() {
    try {
      const data = await request("GET", "/users");
      setUsersList(data);
    } catch (err) {
      setError(err.message);
    }
  }

  async function openTOTP() {
    setMenuOpen(false);
    setTotpOpen(true);
    setTotpSetup(null);
    setTotpMessage("");
    try {
      const data = await request("GET", "/auth/2fa/status");
      setTotpStatus(data);
    } catch (err) {
      setError(err.message);
    }
  }

  async function setupTOTP() {
    setTotpMessage("");
    try {
      const data = await request("POST", "/auth/2fa/setup", {});
      setTotpSetup(data);
      setTotpMessage("Scan the QR in Google Authenticator and enter the code below.");
    } catch (err) {
      setError(err.message);
    }
  }

  async function verifyTOTP() {
    setTotpMessage("");
    try {
      await request("POST", "/auth/2fa/verify", { code: totpCode.trim() });
      const data = await request("GET", "/auth/2fa/status");
      setTotpStatus(data);
      setTotpSetup(null);
      setTotpCode("");
      setTotpMessage("2FA enabled");
    } catch (err) {
      setError(err.message);
    }
  }

  async function disableTOTP() {
    setTotpMessage("");
    try {
      await request("POST", "/auth/2fa/disable", {
        code: totpDisableCode.trim(),
        recovery_code: totpRecoveryCode.trim(),
      });
      const data = await request("GET", "/auth/2fa/status");
      setTotpStatus(data);
      setTotpDisableCode("");
      setTotpRecoveryCode("");
      setTotpMessage("2FA disabled");
    } catch (err) {
      setError(err.message);
    }
  }

  async function createUser() {
    if (!usersDraft.name.trim() || !usersDraft.password.trim()) return;
    setUsersBusy(true);
    try {
      await request("POST", "/users", {
        username: usersDraft.name.trim(),
        password: usersDraft.password,
        role: usersDraft.role,
      });
      setUsersDraft({ name: "", role: "operator", password: "" });
      await loadUsers();
    } catch (err) {
      setError(err.message);
    } finally {
      setUsersBusy(false);
    }
  }

  async function updateUserRole(user) {
    setUsersBusy(true);
    try {
      await request("PATCH", `/users/${user.id}`, { role: user.role });
      await loadUsers();
    } catch (err) {
      setError(err.message);
    } finally {
      setUsersBusy(false);
    }
  }

  async function deleteUser(user) {
    if (!confirm(`Delete user ${user.username}?`)) return;
    setUsersBusy(true);
    try {
      await request("DELETE", `/users/${user.id}`, {});
      await loadUsers();
    } catch (err) {
      setError(err.message);
    } finally {
      setUsersBusy(false);
    }
  }

  async function loadAudit({ offset = auditOffset, nodeID = auditNodeID } = {}) {
    const params = new URLSearchParams();
    params.set("limit", "100");
    params.set("offset", String(offset));
    if (nodeID) {
      params.set("node_id", nodeID);
    }
    const data = await request("GET", `/audit?${params.toString()}`);
    setAuditLogs(data);
    setAuditOffset(offset);
  }

  return (
    <div className="page">
      <header className="header">
        <div className="header-left">
          <button className="icon-button" onClick={() => setMenuOpen((v) => !v)} aria-label="Menu">
            ☰
          </button>
          <h2>Nodes</h2>
          {menuOpen && (
            <div className="menu">
              {(isAdmin || isOperator) && <button type="button" onClick={openAddForm}>Add node</button>}
              {isAdmin && <button type="button" onClick={async () => { setUsersOpen(true); setMenuOpen(false); await loadUsers(); }}>Users & roles</button>}
              {!isViewer && <button type="button" onClick={openTOTP}>2FA settings</button>}
              {isAdmin && (
                <button type="button" onClick={async () => {
                  setMenuOpen(false);
                  setTelegramSaved("");
                  setTelegramTestMsg("");
                  setTelegramTestStatus("");
                  setTelegramTestResults([]);
                  setTelegramOpen(true);
                  try {
                    const data = await getTelegramSettings();
                    setTelegramForm((prev) => ({
                      ...prev,
                      bot_token: "",
                      admin_chat_ids: data.admin_chat_ids || (data.admin_chat_id ? [data.admin_chat_id] : []),
                      alert_connection: data.alert_connection ?? true,
                      alert_cpu: data.alert_cpu ?? true,
                      alert_memory: data.alert_memory ?? true,
                      alert_disk: data.alert_disk ?? true,
                    }));
                    setTelegramTokenSet(Boolean(data.bot_token_set));
                  } catch (err) {
                    setError(err.message);
                  }
                }}>Telegram alerts</button>
              )}
              {isAdmin && <button type="button" onClick={openAudit}>Audit log</button>}
            </div>
          )}
        </div>
        <div className="header-user">
          {user && <span className="muted small">Signed in: {user}</span>}
          <button onClick={() => { clearAuth(); navigate("/login", { replace: true }); }}>Logout</button>
        </div>
      </header>

      {error && <div className="error">{error}</div>}

      {actionPlan.open && (
        <div className="modal">
          <div className="modal-content">
            <h3>Confirm action</h3>
            <div className="plan-steps">
              <div className="muted small">Will run on node {actionPlan.node?.name}:</div>
              <ul>
                {actionPlan.steps.map((step, idx) => (
                  <li key={`${actionPlan.action}-${idx}`}>{step}</li>
                ))}
              </ul>
            </div>
            {actionConfirmToken(actionPlan.action) && (
              <label>
                Type {actionConfirmToken(actionPlan.action)} to confirm
                <input
                  autoComplete="off"
                  name="action_confirm"
                  value={actionPlan.confirm}
                  onChange={(e) => setActionPlan({ ...actionPlan, confirm: e.target.value })}
                />
              </label>
            )}
            <div className="actions">
              <button type="button" onClick={() => setActionPlan({ open: false, node: null, action: null, steps: [], confirm: "" })}>
                Cancel
              </button>
              <button type="button" onClick={runActionPlan} disabled={actionBusy}>
                {actionBusy ? "Running..." : "Run"}
              </button>
            </div>
          </div>
        </div>
      )}

      <div className="nodes-cards">
        <div className="nodes-cards-head">
          <div>
            <h3>Nodes Manager</h3>
            <div className="muted">{nodes.length} servers configured</div>
          </div>
        </div>

        {nodes.map((node) => {
          const uptimePoints = uptimeMap[node.id] || [];
          const { percent, success, total } = computeUptime(uptimePoints);
          const lastTs = uptimePoints[uptimePoints.length - 1]?.ts;
          const isCollapsed = collapsedNodes[node.id] !== false;

          return (
            <div className="node-card" key={node.id}>
              <div className="node-card-top">
                <div className="node-card-title">
                  <div className="node-name-row">
                    <div className="node-name">{node.name || "Unnamed node"}</div>
                    <StatusBadge status={statusMap[node.id]?.status} />
                  </div>
                  <div className="tag-row">
                    {(node.tags || []).length > 0 ? (
                      (node.tags || []).map((tag, idx) => (
                        <span className="chip subtle" key={`${node.id}-tag-${idx}`}>{tag}</span>
                      ))
                    ) : (
                      <span className="muted small">No tags</span>
                    )}
                  </div>
                  <div className="node-link">
                    {node.base_url ? (
                      <a href={node.base_url} target="_blank" rel="noreferrer">
                        {node.base_url} ↗
                      </a>
                    ) : (
                      <span className="muted small">No base URL</span>
                    )}
                  </div>
                  <div className="node-versions">
                    <span className="muted small">Panel: {node.panel_version || "unknown"}</span>
                    <span className="muted small">Xray: {node.xray_version || "unknown"}</span>
                  </div>
                  {lastTs && <div className="muted small">Last check: {formatTS(lastTs)}</div>}
                </div>
                <div className="node-uptime">
                  <div className="uptime-value">{percent.toFixed(1)}%</div>
                  <div className="uptime-label">Uptime</div>
                  <button
                    type="button"
                    className="icon-button"
                    onClick={() => setCollapsedNodes((prev) => ({ ...prev, [node.id]: !isCollapsed }))}
                    aria-label="Toggle node details"
                  >
                    {isCollapsed ? "▸" : "▾"}
                  </button>
                </div>
              </div>

              {!isCollapsed && (
                <>
                  <div className="node-availability">
                    <div className="availability-header">
                      <div className="muted small">Last {total || 0} checks</div>
                      <div className="muted small">{success}/{total || 0} successful</div>
                    </div>
                    <Sparkline points={uptimePoints} />
                  </div>

                  <MetricSparks metrics={metricsMap[node.id]} />

                  <div className="node-meta-grid">
                    <div className="meta-box">
                      <div className="meta-label">SSH Host</div>
                      <div className="meta-value">{node.ssh_host || "-"}</div>
                    </div>
                    <div className="meta-box">
                      <div className="meta-label">Port</div>
                      <div className="meta-value">{node.ssh_port || "-"}</div>
                    </div>
                    <div className="meta-box">
                      <div className="meta-label">Panel User</div>
                      <div className="meta-value">{node.panel_username || "-"}</div>
                    </div>
                  </div>

                  <div className="node-actions">
                    {!isViewer && (
                      <>
                        <Link to={`/nodes/${node.id}/inbounds`} className="link-button">Inbounds</Link>
                        <button className="secondary" onClick={() => openEdit(node)}>Edit</button>
                        {isAdmin && <button className="secondary" onClick={() => openSSH(node)}>SSH</button>}
                        <button className="warning" onClick={() => onRestart(node.id)}>Restart Xray</button>
                        <button className="danger" onClick={() => onReboot(node.id)}>Reboot</button>
                      </>
                    )}
                    {isAdmin && <button className="danger ghost" onClick={() => onDelete(node)}>Delete</button>}
                  </div>
                </>
              )}
            </div>
          );
        })}
      </div>

      {editModal.open && editModal.node && (
        <div className="modal">
          <div className="modal-content">
            <h3>Edit Node</h3>
            <form className="form-grid" onSubmit={onUpdate} autoComplete="off">
              <input name="node_name" autoComplete="off" placeholder="Name" defaultValue={editModal.node.name} />
              <input name="node_tags" autoComplete="off" placeholder="Tags (comma)" defaultValue={(editModal.node.tags || []).join(", ")} />
              <input name="node_base_url" autoComplete="off" placeholder="Base URL" defaultValue={editModal.node.base_url} />
              <input name="node_panel_user" autoComplete="off" placeholder="Panel Username" defaultValue={editModal.node.panel_username} />
              <input name="node_panel_password" autoComplete="new-password" placeholder="Panel Password (leave blank to keep)" type="password" />
              <input name="node_ssh_host" autoComplete="off" placeholder="SSH Host" defaultValue={editModal.node.ssh_host} />
              <input name="node_ssh_port" autoComplete="off" placeholder="SSH Port" type="number" defaultValue={editModal.node.ssh_port} />
              <input name="node_ssh_user" autoComplete="off" placeholder="SSH User" defaultValue={editModal.node.ssh_user} />
              <textarea name="node_ssh_key" autoComplete="off" placeholder="SSH Private Key (leave blank to keep)" rows="3" />
              <label className="checkbox">
                <input name="verify_tls" type="checkbox" defaultChecked={editModal.node.verify_tls} />
                Verify TLS
              </label>
              {editValidation && (
                <div className="validation-summary">
                  {editValidation.error && <div className="error">{editValidation.error}</div>}
                  <ValidationBadge
                    label="SSH"
                    status={editValidation.ssh?.ok ? "ok" : "error"}
                    detail={editValidation.ssh?.ok ? editValidation.ssh.fingerprint : editValidation.ssh?.error}
                  />
                  <ValidationBadge
                    label="Base URL"
                    status={editValidation.base_url?.ok ? "ok" : "error"}
                    detail={editValidation.base_url?.ok ? `HTTP ${editValidation.base_url.status_code}` : editValidation.base_url?.error}
                  />
                  <ValidationBadge
                    label="Panel"
                    status={editValidation.panel_version && editValidation.panel_version !== "unknown" ? "ok" : "error"}
                    detail={editValidation.panel_version || "unknown"}
                  />
                  <ValidationBadge
                    label="Xray"
                    status={editValidation.xray_version && editValidation.xray_version !== "unknown" ? "ok" : "error"}
                    detail={editValidation.xray_version || "unknown"}
                  />
                  {editValidation.ssh?.passphrase_required && (
                    <span className="muted small">Passphrase required for SSH key</span>
                  )}
                </div>
              )}
              <div className="actions">
                <button type="button" onClick={() => onTest(editModal.node.id)}>Test</button>
                <button type="button" onClick={() => setEditModal({ open: false, node: null })}>Cancel</button>
                <button type="button" onClick={(e) => onValidateEdit(e.currentTarget.form)} disabled={editValidating}>
                  {editValidating ? "Validating..." : "Validate"}
                </button>
                <button type="submit">Save</button>
              </div>
            </form>
          </div>
        </div>
      )}

      {addOpen && (
        <div className="modal">
          <div className="modal-content wide">
            <h3>Add Node</h3>
            <form className="form-grid" onSubmit={onCreate} autoComplete="off">
              <input name="node_name" autoComplete="off" placeholder="Name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
              <input name="node_tags" autoComplete="off" placeholder="Tags (comma)" value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} />
              <input name="node_base_url" autoComplete="off" placeholder="Base URL" value={form.base_url} onChange={(e) => setForm({ ...form, base_url: e.target.value })} />
              <input name="node_panel_user" autoComplete="off" placeholder="Panel Username" value={form.panel_username} onChange={(e) => setForm({ ...form, panel_username: e.target.value })} />
              <input name="node_panel_password" autoComplete="new-password" placeholder="Panel Password" type="password" value={form.panel_password} onChange={(e) => setForm({ ...form, panel_password: e.target.value })} />
              <input name="node_ssh_host" autoComplete="off" placeholder="SSH Host" value={form.ssh_host} onChange={(e) => setForm({ ...form, ssh_host: e.target.value })} />
              <input name="node_ssh_port" autoComplete="off" placeholder="SSH Port" type="number" value={form.ssh_port} onChange={(e) => setForm({ ...form, ssh_port: Number(e.target.value) })} />
              <input name="node_ssh_user" autoComplete="off" placeholder="SSH User" value={form.ssh_user} onChange={(e) => setForm({ ...form, ssh_user: e.target.value })} />
              <input name="node_key_passphrase" autoComplete="new-password" placeholder="Key Passphrase (optional)" type="password" value={keyPassphrase} onChange={(e) => setKeyPassphrase(e.target.value)} />
              <label className="file-input">
                Upload SSH Key (.ppk/.pem/.key)
                <input type="file" accept=".ppk,.pem,.key" onChange={onKeyUpload} />
              </label>
              <textarea name="node_ssh_key" autoComplete="off" placeholder="SSH Private Key" rows="3" value={form.ssh_key} onChange={(e) => setForm({ ...form, ssh_key: e.target.value })} />
              <div className="hint">Paste OpenSSH private key or upload .ppk</div>
              {keyFingerprint && <div className="hint">Fingerprint: {keyFingerprint}</div>}
              <label className="checkbox">
                <input type="checkbox" checked={form.verify_tls} onChange={(e) => setForm({ ...form, verify_tls: e.target.checked })} />
                Verify TLS
              </label>
              <div className="actions">
                <button type="button" onClick={onValidateCreate} disabled={validating}>
                  {validating ? "Validating..." : "Validate"}
                </button>
                <button type="submit">Create</button>
                <button type="button" onClick={() => setAddOpen(false)}>Close</button>
              </div>
            </form>

            {validation && (
              <div className="validation-summary">
                {validation.error && <div className="error">{validation.error}</div>}
                <ValidationBadge
                  label="SSH"
                  status={validation.ssh?.ok ? "ok" : "error"}
                  detail={validation.ssh?.ok ? validation.ssh.fingerprint : validation.ssh?.error}
                />
                <ValidationBadge
                  label="Base URL"
                  status={validation.base_url?.ok ? "ok" : "error"}
                  detail={validation.base_url?.ok ? `HTTP ${validation.base_url.status_code}` : validation.base_url?.error}
                />
                <ValidationBadge
                  label="Panel"
                  status={validation.panel_version && validation.panel_version !== "unknown" ? "ok" : "error"}
                  detail={validation.panel_version || "unknown"}
                />
                <ValidationBadge
                  label="Xray"
                  status={validation.xray_version && validation.xray_version !== "unknown" ? "ok" : "error"}
                  detail={validation.xray_version || "unknown"}
                />
                {validation.ssh?.passphrase_required && (
                  <span className="muted small">Passphrase required for SSH key</span>
                )}
              </div>
            )}
          </div>
        </div>
      )}

      {telegramOpen && (
        <div className="modal">
          <div className="modal-content">
            <h3>Telegram alerts</h3>
            <div className="form-grid" autoComplete="off">
              <input
                placeholder={telegramTokenSet ? "Bot token (leave blank to keep)" : "Bot token"}
                type="password"
                name="telegram_bot_token"
                autoComplete="new-password"
                value={telegramForm.bot_token}
                onChange={(e) => setTelegramForm({ ...telegramForm, bot_token: e.target.value })}
              />
              <ListInput
                label="Admin chat IDs"
                values={telegramForm.admin_chat_ids}
                placeholder="123456789"
                onChange={(values) => setTelegramForm({ ...telegramForm, admin_chat_ids: values })}
              />
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={telegramForm.alert_connection}
                  onChange={(e) => setTelegramForm({ ...telegramForm, alert_connection: e.target.checked })}
                />
                Connection loss
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={telegramForm.alert_cpu}
                  onChange={(e) => setTelegramForm({ ...telegramForm, alert_cpu: e.target.checked })}
                />
                High CPU
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={telegramForm.alert_memory}
                  onChange={(e) => setTelegramForm({ ...telegramForm, alert_memory: e.target.checked })}
                />
                High memory
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={telegramForm.alert_disk}
                  onChange={(e) => setTelegramForm({ ...telegramForm, alert_disk: e.target.checked })}
                />
                Low disk space
              </label>
            </div>
            <div className="audit-controls">
              <input
                name="telegram_test_message"
                autoComplete="off"
                placeholder="Test message (optional)"
                value={telegramTestMsg}
                onChange={(e) => setTelegramTestMsg(e.target.value)}
              />
              <button
                type="button"
                onClick={async () => {
                  setTelegramTestStatus("");
                  try {
                    const res = await request("POST", "/telegram/test", {
                      message: telegramTestMsg,
                      admin_chat_ids: telegramForm.admin_chat_ids,
                      bot_token: telegramForm.bot_token,
                    });
                    if (res.ok && res.sent === res.total) {
                      setTelegramTestStatus(`Sent to ${res.sent}/${res.total}`);
                    } else {
                      setTelegramTestStatus(`Sent to ${res.sent}/${res.total} (some failed)`);
                    }
                    setTelegramTestResults(res.results || []);
                  } catch (err) {
                    setError(err.message);
                  }
                }}
              >
                Send test
              </button>
            </div>
            {telegramSaved && <div className="hint">{telegramSaved}</div>}
            {telegramTestStatus && <div className="hint">{telegramTestStatus}</div>}
            {telegramTestResults.length > 0 && (
              <div className="table compact">
                <div className="table-row head">
                  <div>Chat ID</div>
                  <div>Status</div>
                  <div>Error</div>
                </div>
                {telegramTestResults.map((row) => (
                  <div className="table-row" key={row.chat_id}>
                    <div>{row.chat_id}</div>
                    <div>{row.ok ? "ok" : "error"}</div>
                    <div>{row.error || "-"}</div>
                  </div>
                ))}
              </div>
            )}
            <div className="actions">
              <button
                type="button"
                onClick={async () => {
                  setTelegramSaved("");
                  try {
                    await saveTelegramSettings(telegramForm);
                    setTelegramForm({ ...telegramForm, bot_token: "" });
                    setTelegramSaved("Saved");
                  } catch (err) {
                    setError(err.message);
                  }
                }}
              >
                Save
              </button>
              <button type="button" onClick={() => setTelegramOpen(false)}>Close</button>
            </div>
          </div>
        </div>
      )}

      {totpOpen && (
        <div className="modal">
          <div className="modal-content">
            <h3>2FA (TOTP)</h3>
            <div className="form-grid" autoComplete="off">
              <div className="hint">
                Steps: Generate QR → scan in Google Authenticator → enter code → enable.
              </div>
              <div className="hint">
                {totpStatus?.required ? "Required for your role." : "Optional for your role."}
              </div>
              <div className="hint">
                Status: {totpStatus?.enabled ? "enabled" : "disabled"}
              </div>
              {!totpStatus?.enabled && (
                <button type="button" className="btn-inline" onClick={setupTOTP}>
                  Generate QR
                </button>
              )}
              {totpSetup && (
                <>
                  <img className="qr-img" src={totpSetup.qr_png} alt="TOTP QR" />
                  <div className="hint">Secret: {totpSetup.secret}</div>
                  <input
                    name="totp_code"
                    autoComplete="one-time-code"
                    placeholder="Enter code"
                    value={totpCode}
                    onChange={(e) => setTotpCode(e.target.value)}
                  />
                  <button type="button" className="btn-inline" onClick={verifyTOTP}>
                    Enable 2FA
                  </button>
                </>
              )}
              {totpStatus?.enabled && (
                <>
                  <input
                    name="totp_disable_code"
                    autoComplete="one-time-code"
                    placeholder="Code to disable"
                    value={totpDisableCode}
                    onChange={(e) => setTotpDisableCode(e.target.value)}
                  />
                  <input
                    name="totp_recovery_code"
                    autoComplete="off"
                    placeholder="Recovery code (optional)"
                    value={totpRecoveryCode}
                    onChange={(e) => setTotpRecoveryCode(e.target.value)}
                  />
                  <button type="button" className="btn-inline" onClick={disableTOTP}>
                    Disable 2FA
                  </button>
                </>
              )}
            </div>
            {totpMessage && <div className="hint">{totpMessage}</div>}
            <div className="actions">
              <button type="button" onClick={() => setTotpOpen(false)}>Close</button>
            </div>
          </div>
        </div>
      )}

      {usersOpen && (
        <div className="modal">
          <div className="modal-content">
            <h3>Users & roles</h3>
            <div className="form-grid" autoComplete="off">
              <input
                name="user_name"
                autoComplete="off"
                placeholder="Username or email"
                value={usersDraft.name}
                onChange={(e) => setUsersDraft({ ...usersDraft, name: e.target.value })}
              />
              <input
                name="user_password"
                autoComplete="new-password"
                type="password"
                placeholder="Password"
                value={usersDraft.password}
                onChange={(e) => setUsersDraft({ ...usersDraft, password: e.target.value })}
              />
              <select
                name="user_role"
                value={usersDraft.role}
                onChange={(e) => setUsersDraft({ ...usersDraft, role: e.target.value })}
              >
                <option value="admin">Administrator</option>
                <option value="operator">Operator (no node delete)</option>
                <option value="viewer">Viewer (status only)</option>
              </select>
            </div>
            <div className="actions">
              <button
                type="button"
                onClick={createUser}
                disabled={usersBusy}
              >
                Add user
              </button>
              <button type="button" onClick={() => setUsersOpen(false)}>Close</button>
            </div>
            <div className="table compact users-table">
              <div className="table-row head">
                <div>User</div>
                <div>Role</div>
                <div>Actions</div>
              </div>
              {usersList.map((user) => (
                <div className="table-row" key={user.id}>
                  <div>{user.username}</div>
                  <div>
                    <select
                      value={user.role}
                      onChange={(e) => setUsersList(usersList.map((u) => u.id === user.id ? { ...u, role: e.target.value } : u))}
                    >
                      <option value="admin">Administrator</option>
                      <option value="operator">Operator</option>
                      <option value="viewer">Viewer</option>
                    </select>
                  </div>
                  <div className="actions">
                    <button type="button" onClick={() => updateUserRole(user)} disabled={usersBusy}>Save</button>
                    <button className="danger ghost" type="button" onClick={() => deleteUser(user)} disabled={usersBusy}>
                      Remove
                    </button>
                  </div>
                </div>
              ))}
              {usersList.length === 0 && (
                <div className="table-row">
                  <div className="muted small">No users yet</div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {auditOpen && (
        <div className="modal">
          <div className="modal-content wide">
            <h3>Audit log</h3>
            <div className="audit-controls">
              <input
                placeholder="Filter by node_id"
                autoComplete="off"
                name="audit_node_id"
                value={auditNodeID}
                onChange={(e) => setAuditNodeID(e.target.value)}
              />
              <button type="button" onClick={() => loadAudit({ offset: 0, nodeID: auditNodeID })}>Apply</button>
              <button type="button" onClick={() => { setAuditNodeID(""); loadAudit({ offset: 0, nodeID: "" }); }}>Clear</button>
            </div>
            <div className="table compact">
              <div className="table-row head">
                <div>Time</div>
                <div>Actor</div>
                <div>Action</div>
                <div>Status</div>
                <div>Node</div>
                <div>Message</div>
              </div>
              {auditLogs.map((row) => (
                <div className="table-row" key={row.id}>
                  <div>{formatTS(row.ts || row.created_at)}</div>
                  <div>{row.actor_user || row.actor}</div>
                  <div>{row.action}</div>
                  <div>{row.status}</div>
                  <div>{row.node_id || "-"}</div>
                  <div>{row.message || row.error || "-"}</div>
                </div>
              ))}
            </div>
            <div className="actions">
              <button type="button" onClick={() => loadAudit({ offset: Math.max(0, auditOffset - 100) })} disabled={auditOffset === 0}>Prev</button>
              <button type="button" onClick={() => loadAudit({ offset: auditOffset + 100 })} disabled={auditLogs.length < 100}>Next</button>
              <button type="button" onClick={() => setAuditOpen(false)}>Close</button>
            </div>
          </div>
        </div>
      )}

      <NodeSSHModal
        open={sshModal.open}
        node={sshModal.node}
        onClose={() => {
          if (sshModal.confirmClose && !confirm("Вы уверены?")) return;
          setSshModal({ open: false, node: null, confirmClose: false });
        }}
      />

      {sshChoice.open && sshChoice.node && (
        <div className="modal">
          <div className="modal-content">
            <h3>Открыть SSH</h3>
            <div className="hint">Открыть здесь или в новом окне?</div>
            <div className="actions">
              <button
                type="button"
                onClick={() => {
                  setSshModal({ open: true, node: sshChoice.node, confirmClose: true });
                  setSshChoice({ open: false, node: null });
                }}
              >
                Здесь
              </button>
              <button
                type="button"
                onClick={() => {
                  window.open(`/nodes?ssh=${sshChoice.node.id}`, "_blank");
                  setSshChoice({ open: false, node: null });
                }}
              >
                В новом окне
              </button>
              <button type="button" onClick={() => setSshChoice({ open: false, node: null })}>Отмена</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function InboundsPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [data, setData] = useState(null);
  const [error, setError] = useState("");
  const [editor, setEditor] = useState({ open: false, mode: "add", inbound: null });

  async function loadInbounds() {
    setError("");
    try {
      const res = await request("GET", `/nodes/${id}/inbounds`);
      setData(res);
    } catch (err) {
      setError(err.message);
    }
  }

  useEffect(() => {
    loadInbounds();
  }, [id]);

  const inbounds = useMemo(() => {
    if (!data) return [];
    return Array.isArray(data.obj) ? data.obj : [];
  }, [data]);

  function openAdd() {
    setEditor({ open: true, mode: "add", inbound: null });
  }

  function openEdit(inbound) {
    setEditor({ open: true, mode: "edit", inbound });
  }

  async function onDelete(inboundId) {
    if (!confirm("Delete inbound?")) return;
    setError("");
    try {
      await request("DELETE", `/nodes/${id}/inbounds/${inboundId}`, {});
      loadInbounds();
    } catch (err) {
      setError(err.message);
    }
  }

  return (
    <div className="page">
      <header className="header">
        <h2>Inbounds</h2>
        <div className="actions">
          <button onClick={() => navigate("/nodes")}>Back</button>
          <button onClick={openAdd}>Add</button>
        </div>
      </header>

      {error && <div className="error">{error}</div>}

      <div className="inbounds-table-desktop">
        <div className="table inbounds">
          <div className="table-row head">
            <div>ID</div>
            <div>Remark</div>
            <div>Protocol</div>
            <div>Port</div>
            <div>Actions</div>
          </div>
          {inbounds.map((inbound) => (
            <div className="table-row" key={inbound.id}>
              <div>{inbound.id}</div>
              <div>{inbound.remark}</div>
              <div>{inbound.protocol}</div>
              <div>{inbound.port}</div>
              <div className="actions">
                <button onClick={() => openEdit(inbound)}>Edit</button>
                <button className="danger" onClick={() => onDelete(inbound.id)}>Delete</button>
              </div>
            </div>
          ))}
        </div>
      </div>
      <div className="inbounds-cards-mobile">
        {inbounds.map((inbound) => (
          <div className="inbound-card" key={`card-${inbound.id}`}>
            <div className="inbound-card-row">
              <span className="field-label">ID</span>
              <span>{inbound.id}</span>
            </div>
            <div className="inbound-card-row">
              <span className="field-label">Remark</span>
              <span>{inbound.remark || "—"}</span>
            </div>
            <div className="inbound-card-row">
              <span className="field-label">Protocol</span>
              <span>{inbound.protocol || "—"}</span>
            </div>
            <div className="inbound-card-row">
              <span className="field-label">Port</span>
              <span>{inbound.port || "—"}</span>
            </div>
            <div className="actions">
              <button onClick={() => openEdit(inbound)}>Edit</button>
              <button className="danger" onClick={() => onDelete(inbound.id)}>Delete</button>
            </div>
          </div>
        ))}
      </div>

      <InboundEditor
        open={editor.open}
        mode={editor.mode}
        inbound={editor.inbound}
        onClose={() => setEditor({ open: false, mode: "add", inbound: null })}
        onSave={async (payload) => {
          setError("");
          try {
            if (editor.mode === "add") {
              await request("POST", `/nodes/${id}/inbounds`, payload);
            } else {
              await request("PATCH", `/nodes/${id}/inbounds/${editor.inbound?.id}`, payload);
            }
            setEditor({ open: false, mode: "add", inbound: null });
            loadInbounds();
          } catch (err) {
            setError(err.message);
          }
        }}
      />
    </div>
  );
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/nodes"
        element={
          <RequireAuth>
            <NodesPage />
          </RequireAuth>
        }
      />
      <Route
        path="/nodes/:id/inbounds"
        element={
          <RequireAuth>
            <InboundsPage />
          </RequireAuth>
        }
      />
      <Route path="*" element={<LoginPage />} />
    </Routes>
  );
}
