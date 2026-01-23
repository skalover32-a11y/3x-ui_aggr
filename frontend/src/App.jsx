import React, { useEffect, useMemo, useRef, useState } from "react";
import { Routes, Route, useNavigate, useLocation, useParams, Link } from "react-router-dom";
import { request, getToken, refreshAuth, convertSSHKey, getTelegramSettings, saveTelegramSettings, setAuth, clearAuth, getRole, getUser, API_BASE } from "./api.js";
import { useI18n } from "./i18n.js";
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

function formatPanelIssue(point, t) {
  if (!point || point.panel_ok !== false || !point.panel_error_code) return null;
  const code = point.panel_error_code;
  switch (code) {
    case "CERT_EXPIRED":
      return t("TLS cert expired");
    case "CERT_NOT_YET_VALID":
      return t("TLS cert not yet valid");
    case "UNKNOWN_CA":
      return t("TLS unknown CA");
    case "HOSTNAME_MISMATCH":
      return t("TLS hostname mismatch");
    case "HANDSHAKE":
      return t("TLS handshake failed");
    case "GENERIC_HTTP_ERROR":
      return t("HTTP error");
    default:
      return t("Panel error");
  }
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

function formatBps(value) {
  if (value == null) return "-";
  return `${formatBytes(value)}/s`;
}

function formatPercent(value) {
  if (value == null || Number.isNaN(value)) return "-";
  return `${value.toFixed(1)}%`;
}

function formatDuration(sec) {
  if (sec == null || sec <= 0) return "-";
  const total = Math.floor(sec);
  const days = Math.floor(total / 86400);
  const hours = Math.floor((total % 86400) / 3600);
  const mins = Math.floor((total % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${mins}m`;
  return `${mins}m`;
}

function buildWsUrl(path) {
  const protocol = window.location.protocol === "https:" ? "wss" : "ws";
  return `${protocol}://${window.location.host}${API_BASE}${path}`;
}

function base64urlToBuffer(value) {
  if (!value) return new ArrayBuffer(0);
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const pad = base64.length % 4 === 0 ? "" : "=".repeat(4 - (base64.length % 4));
  const str = atob(base64 + pad);
  const bytes = new Uint8Array(str.length);
  for (let i = 0; i < str.length; i++) {
    bytes[i] = str.charCodeAt(i);
  }
  return bytes.buffer;
}

function bufferToBase64url(buffer) {
  const bytes = new Uint8Array(buffer);
  let str = "";
  for (let i = 0; i < bytes.length; i++) {
    str += String.fromCharCode(bytes[i]);
  }
  return btoa(str).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function publicKeyCredentialToJSON(cred) {
  if (!cred) return null;
  return {
    id: cred.id,
    type: cred.type,
    rawId: bufferToBase64url(cred.rawId),
    response: {
      clientDataJSON: bufferToBase64url(cred.response.clientDataJSON),
      authenticatorData: cred.response.authenticatorData ? bufferToBase64url(cred.response.authenticatorData) : undefined,
      signature: cred.response.signature ? bufferToBase64url(cred.response.signature) : undefined,
      userHandle: cred.response.userHandle ? bufferToBase64url(cred.response.userHandle) : undefined,
      attestationObject: cred.response.attestationObject ? bufferToBase64url(cred.response.attestationObject) : undefined,
      publicKeyAlgorithm: cred.response.publicKeyAlgorithm,
    },
    clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {},
    authenticatorAttachment: cred.authenticatorAttachment,
  };
}

function prepareCreationOptions(options) {
  const publicKey = { ...options };
  publicKey.challenge = base64urlToBuffer(publicKey.challenge);
  if (publicKey.user && publicKey.user.id) {
    publicKey.user = { ...publicKey.user, id: base64urlToBuffer(publicKey.user.id) };
  }
  if (Array.isArray(publicKey.excludeCredentials)) {
    publicKey.excludeCredentials = publicKey.excludeCredentials.map((cred) => ({
      ...cred,
      id: base64urlToBuffer(cred.id),
    }));
  }
  return publicKey;
}

function prepareRequestOptions(options) {
  const publicKey = { ...options };
  publicKey.challenge = base64urlToBuffer(publicKey.challenge);
  if (Array.isArray(publicKey.allowCredentials)) {
    publicKey.allowCredentials = publicKey.allowCredentials.map((cred) => ({
      ...cred,
      id: base64urlToBuffer(cred.id),
    }));
  }
  return publicKey;
}

function StatusBadge({ status }) {
  const { t } = useI18n();
  const label = status || "unknown";
  const textKey = label === "online" ? "Online" : label === "degraded" ? "Degraded" : label === "offline" ? "Offline" : label;
  return <span className={`badge ${label}`}>{t(textKey)}</span>;
}

function DashboardStatusBadge({ status }) {
  const { t } = useI18n();
  const label = status || "unknown";
  const textKey = label === "online" ? "Online" : label === "stale" ? "Stale" : label === "offline" ? "Offline" : label === "no_agent" ? "No agent" : label;
  return <span className={`badge ${label}`}>{t(textKey)}</span>;
}

function Sparkline({ points }) {
  const { t } = useI18n();
  if (!points || points.length === 0) return <div className="availability empty">{t("No data")}</div>;
  const first = points[0];
  const last = points[points.length - 1];
  return (
    <div className="availability">
      <div className="availability-bars">
        {points.map((p, idx) => {
          const status = deriveStatus(p.panel_ok, p.ssh_ok);
          const title = `${formatTS(p.ts)} | ${t("Latency")} ${p.latency_ms || 0}ms${p.error ? ` | ${p.error}` : ""}`;
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
  const { t } = useI18n();
  if (!metrics || metrics.length === 0) return <div className="metrics empty">{t("No metrics")}</div>;
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
          <span>{t("CPU Load")}</span>
          <span className="muted small">{load1 != null ? load1.toFixed(2) : "—"}</span>
        </div>
        {renderBars(metrics.map((m) => (m.load1 != null ? Math.min(m.load1 * 100, 200) : 0)), "cpu")}
      </div>
      <div className="metric">
        <div className="metric-header">
          <span>{t("Memory")}</span>
          <span className="muted small">
            {memLatest != null ? `${memLatest.toFixed(1)}%` : "—"}
            {latest.mem_total_bytes ? ` / ${formatBytes(latest.mem_total_bytes)}` : ""}
          </span>
        </div>
        {renderBars(memPercents, "mem")}
      </div>
      <div className="metric">
        <div className="metric-header">
          <span>{t("Disk")}</span>
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
  const { t } = useI18n();
  const [value, setValue] = useState("");
  return (
    <div className="list-editor">
      <div className="list-label">{label}</div>
      <div className="chips">
        {values.map((item, idx) => (
          <span className="chip" key={`${item}-${idx}`}>
            {item}
            <button type="button" onClick={() => onChange(values.filter((_, i) => i !== idx))} aria-label={t("Remove")}>×</button>
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
          {t("Add")}
        </button>
      </div>
    </div>
  );
}

function RequireAuth({ children }) {
  const navigate = useNavigate();
  const location = useLocation();
  const [checking, setChecking] = useState(true);
  useEffect(() => {
    let active = true;
    async function ensureAuth() {
      if (getToken()) {
        if (active) setChecking(false);
        return;
      }
      try {
        const data = await refreshAuth();
        if (data?.token) {
          setAuth(data.token, data.role, data.username);
        }
      } catch {
        navigate("/login", { replace: true, state: { from: location.pathname } });
      } finally {
        if (active) setChecking(false);
      }
    }
    ensureAuth();
    return () => {
      active = false;
    };
  }, [navigate, location]);
  if (checking) {
    return <div className="page center"><div className="muted">Loading...</div></div>;
  }
  return children;
}

function LoginPage() {
  const { t } = useI18n();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [otp, setOtp] = useState("");
  const [recoveryCode, setRecoveryCode] = useState("");
  const [recoveryStatus, setRecoveryStatus] = useState("");
  const [error, setError] = useState("");
  const [passkeyBusy, setPasskeyBusy] = useState(false);
  const navigate = useNavigate();
  const webAuthnSupported = typeof window !== "undefined" && Boolean(window.PublicKeyCredential);

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
      setRecoveryStatus(t("Recovery code sent to Telegram"));
    } catch (err) {
      setError(err.message);
    }
  }

  async function onPasskeyLogin() {
    setError("");
    setRecoveryStatus("");
    if (!webAuthnSupported) {
      setError(t("Passkeys are not supported in this browser."));
      return;
    }
    setPasskeyBusy(true);
    try {
      const options = await request("POST", "/auth/webauthn/login/options", { username: username.trim() });
      const publicKey = prepareRequestOptions(options.publicKey || options);
      const cred = await navigator.credentials.get({ publicKey });
      const payload = publicKeyCredentialToJSON(cred);
      const data = await request("POST", "/auth/webauthn/login/verify", {
        username: username.trim(),
        challenge_id: options.challenge_id,
        credential: payload,
      });
      setAuth(data.token, data.role, data.username);
      navigate("/nodes");
    } catch (err) {
      setError(err.message);
    } finally {
      setPasskeyBusy(false);
    }
  }

  return (
    <div className="page center">
      <form className="card" onSubmit={onSubmit} autoComplete="on">
        <h1>3x-ui Aggregator</h1>
        <label>
          {t("Username")}
          <input name="username" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} />
        </label>
        <label>
          {t("Password")}
          <input name="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        <label>
          {t("2FA Code")}
          <input name="otp" autoComplete="one-time-code" placeholder="123456" value={otp} onChange={(e) => setOtp(e.target.value)} />
        </label>
        <label>
          {t("Recovery code (optional)")}
          <input name="recovery_code" autoComplete="off" value={recoveryCode} onChange={(e) => setRecoveryCode(e.target.value)} />
        </label>
        <button type="button" className="ghost" onClick={onSendRecovery}>
          {t("Send recovery code via Telegram")}
        </button>
        {error && <div className="error">{error}</div>}
        {recoveryStatus && <div className="hint">{t("Recovery code sent to Telegram")}</div>}
        <button type="submit">{t("Login")}</button>
        <button type="button" className="secondary" onClick={onPasskeyLogin} disabled={!webAuthnSupported || passkeyBusy}>
          {passkeyBusy ? t("Loading...") : t("Login with Passkey")}
        </button>
        {!webAuthnSupported && <div className="hint">{t("Passkeys are not supported in this browser.")}</div>}
      </form>
    </div>
  );
}

function NodesPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const { t, lang, setLang } = useI18n();
  const role = getRole();
  const user = getUser();
  const isAdmin = role === "admin";
  const isOperator = role === "operator";
  const isViewer = role === "viewer";
  const [nodes, setNodes] = useState([]);
  const [selectedNodes, setSelectedNodes] = useState({});
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
  const menuRef = useRef(null);
  const menuButtonRef = useRef(null);
  const deployWsRef = useRef(null);
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
  const [telegramSaved, setTelegramSaved] = useState(false);
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
  const [passkeysOpen, setPasskeysOpen] = useState(false);
  const [passkeysList, setPasskeysList] = useState([]);
  const [passkeysBusy, setPasskeysBusy] = useState(false);
  const [passkeysError, setPasskeysError] = useState("");
  const [passkeysOTP, setPasskeysOTP] = useState("");
  const [passkeysTotpRequired, setPasskeysTotpRequired] = useState(false);
  const [sshModal, setSshModal] = useState({ open: false, node: null, confirmClose: false });
  const [sshChoice, setSshChoice] = useState({ open: false, node: null });
  const [sshAutoOpened, setSshAutoOpened] = useState("");
  const [nodeDetails, setNodeDetails] = useState({ open: false, node: null });
  const [nodeTab, setNodeTab] = useState("overview");
  const [nodeTypeFilter, setNodeTypeFilter] = useState("PANEL");
  const [servicesMap, setServicesMap] = useState({});
  const [serviceResults, setServiceResults] = useState({});
  const [servicesBusy, setServicesBusy] = useState(false);
  const [servicesError, setServicesError] = useState("");
  const [serviceEditor, setServiceEditor] = useState({ open: false, mode: "add", node: null, service: null });
  const [serviceForm, setServiceForm] = useState({
    kind: "CUSTOM_HTTP",
    url: "",
    health_path: "/",
    expected_status: ["200"],
    headers_json: "{}",
    is_enabled: true,
  });
  const [botsMap, setBotsMap] = useState({});
  const [botResults, setBotResults] = useState({});
  const [botsBusy, setBotsBusy] = useState(false);
  const [botsError, setBotsError] = useState("");
  const [botEditor, setBotEditor] = useState({ open: false, mode: "add", node: null, bot: null });
  const [botForm, setBotForm] = useState({
    name: "",
    kind: "HTTP",
    docker_container: "",
    systemd_unit: "",
    health_url: "",
    health_path: "/",
    expected_status: ["200"],
    is_enabled: true,
  });
  const [actionPlan, setActionPlan] = useState({ open: false, node: null, action: null, steps: [], confirm: "" });
  const [actionBusy, setActionBusy] = useState(false);
  const [deployOpen, setDeployOpen] = useState(false);
  const [deployProgress, setDeployProgress] = useState({ open: false, jobId: "", status: null });
  const [deployItems, setDeployItems] = useState([]);
  const [deployLogs, setDeployLogs] = useState({});
  const [deployError, setDeployError] = useState("");
  const [taskOpen, setTaskOpen] = useState(false);
  const [taskProgress, setTaskProgress] = useState({ open: false, jobId: "", status: null, title: "" });
  const [taskItems, setTaskItems] = useState([]);
  const [taskLogs, setTaskLogs] = useState({});
  const [taskError, setTaskError] = useState("");
  const [taskForm, setTaskForm] = useState({
    type: "update_panel",
    service: "xray",
    parallelism: 3,
    all: false,
    confirm: "",
  });
  const [deployForm, setDeployForm] = useState({
    agent_port: 9191,
    agent_token_mode: "per-node",
    shared_agent_token: "",
    allow_cidr: "",
    stats_mode: "log",
    xray_access_log_path: "/var/log/xray/access.log",
    rate_limit_rps: 5,
    enable_ufw: true,
    health_check: true,
    parallelism: 3,
    all: false,
    sandbox_only: false,
    confirm: "",
  });
  const [editModal, setEditModal] = useState({ open: false, node: null });
  const [editKind, setEditKind] = useState("PANEL");
  const [form, setForm] = useState({
    kind: "PANEL",
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
    setSelectedNodes((prev) => {
      const next = {};
      nodes.forEach((node) => {
        if (prev[node.id]) {
          next[node.id] = true;
        }
      });
      return next;
    });
  }, [nodes]);

  const selectedNodeIDs = useMemo(() => {
    return Object.keys(selectedNodes).filter((id) => selectedNodes[id]);
  }, [selectedNodes]);

  function toggleNodeSelection(id) {
    setSelectedNodes((prev) => ({ ...prev, [id]: !prev[id] }));
  }

  function selectAllFilteredNodes(list) {
    const next = {};
    list.forEach((node) => {
      next[node.id] = true;
    });
    setSelectedNodes(next);
  }

  function clearSelectedNodes() {
    setSelectedNodes({});
  }

  async function loadServices(nodeID) {
    if (!nodeID) return;
    setServicesBusy(true);
    setServicesError("");
    try {
      const data = await request("GET", `/nodes/${nodeID}/services`);
      setServicesMap((prev) => ({ ...prev, [nodeID]: data }));
      const resultEntries = await Promise.all(
        data.map((svc) => request("GET", `/services/${svc.id}/results?limit=1`).catch(() => []))
      );
      const resultsNext = {};
      data.forEach((svc, idx) => {
        const rows = resultEntries[idx] || [];
        if (rows.length > 0) {
          resultsNext[svc.id] = rows[0];
        }
      });
      setServiceResults((prev) => ({ ...prev, ...resultsNext }));
    } catch (err) {
      setServicesError(err.message);
    } finally {
      setServicesBusy(false);
    }
  }

  async function loadBots(nodeID) {
    if (!nodeID) return;
    setBotsBusy(true);
    setBotsError("");
    try {
      const data = await request("GET", `/nodes/${nodeID}/bots`);
      setBotsMap((prev) => ({ ...prev, [nodeID]: data }));
      const resultEntries = await Promise.all(
        data.map((bot) => request("GET", `/bots/${bot.id}/results?limit=1`).catch(() => []))
      );
      const resultsNext = {};
      data.forEach((bot, idx) => {
        const rows = resultEntries[idx];
        if (Array.isArray(rows) && rows.length > 0) {
          resultsNext[bot.id] = rows[0];
        }
      });
      if (Object.keys(resultsNext).length > 0) {
        setBotResults((prev) => ({ ...prev, ...resultsNext }));
      }
    } catch (err) {
      setBotsError(err.message);
    } finally {
      setBotsBusy(false);
    }
  }

  async function loadAllBots() {
    if (nodes.length === 0) {
      setBotsMap({});
      return;
    }
    setBotsBusy(true);
    setBotsError("");
    try {
      const entries = await Promise.all(
        nodes.map((node) =>
          request("GET", `/nodes/${node.id}/bots`)
            .then((data) => ({ node, data }))
            .catch(() => ({ node, data: [] }))
        )
      );
      const mapNext = {};
      const allBots = [];
      entries.forEach(({ node, data }) => {
        mapNext[node.id] = data;
        allBots.push(...data);
      });
      setBotsMap(mapNext);
      const resultEntries = await Promise.all(
        allBots.map((bot) => request("GET", `/bots/${bot.id}/results?limit=1`).catch(() => []))
      );
      const resultsNext = {};
      allBots.forEach((bot, idx) => {
        const rows = resultEntries[idx];
        if (Array.isArray(rows) && rows.length > 0) {
          resultsNext[bot.id] = rows[0];
        }
      });
      if (Object.keys(resultsNext).length > 0) {
        setBotResults((prev) => ({ ...prev, ...resultsNext }));
      }
    } catch (err) {
      setBotsError(err.message);
    } finally {
      setBotsBusy(false);
    }
  }

  useEffect(() => {
    loadNodes();
  }, []);

  useEffect(() => {
    if (!menuOpen) return;
    function onDocClick(e) {
      if (menuRef.current && menuRef.current.contains(e.target)) return;
      if (menuButtonRef.current && menuButtonRef.current.contains(e.target)) return;
      setMenuOpen(false);
    }
    document.addEventListener("click", onDocClick);
    return () => document.removeEventListener("click", onDocClick);
  }, [menuOpen]);

  useEffect(() => {
    if (nodes.length === 0) return;
    const params = new URLSearchParams(location.search);
    const sshId = params.get("ssh");
    if (sshId && sshAutoOpened !== sshId) {
      const node = nodes.find((n) => n.id === sshId);
      if (node) {
        setSshModal({ open: true, node, confirmClose: false });
        setSshAutoOpened(sshId);
      }
    }
  }, [nodes, location.search, sshAutoOpened]);

  useEffect(() => {
    if (nodes.length === 0) return;
    const fetchChecks = async () => {
      try {
        const uptimeEntries = await Promise.all(
          nodes.map((node) => request("GET", `/nodes/${node.id}/uptime?minutes=60`).catch(() => []))
        );
        const metricEntries = await Promise.all(
          nodes.map((node) => request("GET", `/nodes/${node.id}/metrics?minutes=720`).catch(() => []))
        );
        const uptimeNext = {};
        const metricsNext = {};
        nodes.forEach((node, idx) => {
          uptimeNext[node.id] = uptimeEntries[idx] || [];
          metricsNext[node.id] = metricEntries[idx] || [];
        });
        setUptimeMap(uptimeNext);
        setMetricsMap(metricsNext);
      } catch {
        // ignore
      }
    };
    fetchChecks();
    const interval = setInterval(fetchChecks, 30000);
    return () => clearInterval(interval);
  }, [nodes]);

  useEffect(() => {
    const next = {};
    nodes.forEach((node) => {
      if (node.online === true) {
        next[node.id] = { status: "online" };
      } else if (node.online === false) {
        next[node.id] = { status: "offline" };
      }
    });
    if (Object.keys(next).length > 0) {
      setStatusMap(next);
    }
  }, [nodes]);

  const showingBots = nodeTypeFilter === "BOT";
  const filteredNodes = useMemo(() => {
    if (nodeTypeFilter === "HOST") {
      return nodes.filter((node) => (node.kind || "PANEL") === "HOST");
    }
    if (nodeTypeFilter === "PANEL") {
      return nodes.filter((node) => (node.kind || "PANEL") === "PANEL");
    }
    return nodes;
  }, [nodes, nodeTypeFilter]);
  const botCount = useMemo(() => Object.values(botsMap).flat().length, [botsMap]);

  useEffect(() => {
    if (!nodeDetails.open || !nodeDetails.node) return;
    if (nodeTab !== "services") return;
    loadServices(nodeDetails.node.id);
  }, [nodeDetails.open, nodeDetails.node, nodeTab]);

  useEffect(() => {
    if (!nodeDetails.open || !nodeDetails.node) return;
    if (nodeTab !== "bots") return;
    loadBots(nodeDetails.node.id);
  }, [nodeDetails.open, nodeDetails.node, nodeTab]);

  useEffect(() => {
    if (!showingBots) return;
    loadAllBots();
  }, [showingBots, nodes]);

  async function onCreate(e) {
    e.preventDefault();
    setError("");
    try {
      const isBotNode = form.kind === "BOT";
      const kind = isBotNode ? "HOST" : form.kind;
      const basePayload = isBotNode
        ? { ...form, base_url: "", panel_username: "", panel_password: "" }
        : { ...form };
      const tags = basePayload.tags
        ? basePayload.tags.split(",").map((t) => t.trim()).filter(Boolean)
        : [];
      if (isBotNode && !tags.includes("bot")) {
        tags.push("bot");
      }
      const { kind: _ignoredKind, ...rest } = basePayload;
      const payload = {
        kind,
        ...rest,
        tags,
      };
      const created = await request("POST", "/nodes", payload);
      setForm({ ...form, kind: "PANEL", name: "", tags: "" });
      setKeyPassphrase("");
      setKeyFingerprint("");
      setAddOpen(false);
      loadNodes();
      if (isBotNode) {
        openBotAdd(created);
      }
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
      kind: form.kind === "BOT" ? "HOST" : form.kind,
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
      alert(t("Test OK"));
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
    setEditKind(node?.kind || "PANEL");
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
      kind: formEl.node_kind?.value,
      name: formEl.node_name.value,
      tags: formEl.node_tags.value ? formEl.node_tags.value.split(",").map((t) => t.trim()).filter(Boolean) : [],
      base_url: formEl.node_base_url?.value,
      panel_username: formEl.node_panel_user?.value,
      ssh_host: formEl.node_ssh_host.value,
      ssh_port: Number(formEl.node_ssh_port.value || 22),
      ssh_user: formEl.node_ssh_user.value,
    };
    if (formEl.verify_tls) {
      payload.verify_tls = formEl.verify_tls.checked;
    }
    const panelPass = formEl.node_panel_password?.value;
    const sshKey = formEl.node_ssh_key.value;
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
      kind: formEl.node_kind?.value,
      base_url: formEl.node_base_url?.value,
      ssh_host: formEl.node_ssh_host.value,
      ssh_port: Number(formEl.node_ssh_port.value || 22),
      ssh_user: formEl.node_ssh_user.value,
      ssh_key: formEl.node_ssh_key.value,
      panel_username: formEl.node_panel_user?.value,
      panel_password: formEl.node_panel_password?.value,
    };
    payload.verify_tls = formEl.verify_tls ? formEl.verify_tls.checked : true;
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
      setError(t("Type {token} to confirm", { token: required }));
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

  function openDeployAgent() {
    setDeployError("");
    setDeployOpen(true);
    loadAgentDeployDefaults();
  }

  function openTaskModal(type) {
    setTaskError("");
    setTaskForm((prev) => ({
      ...prev,
      type,
      confirm: "",
    }));
    setTaskOpen(true);
  }

  function closeTaskProgress() {
    setTaskProgress({ open: false, jobId: "", status: null, title: "" });
    setTaskItems([]);
    setTaskLogs({});
  }

  function closeDeployProgress() {
    if (deployWsRef.current) {
      deployWsRef.current.close();
      deployWsRef.current = null;
    }
    setDeployProgress({ open: false, jobId: "", status: null });
    setDeployItems([]);
    setDeployLogs({});
  }

  function updateDeployItem(itemId, patch) {
    setDeployItems((prev) => {
      let updated = false;
      const next = prev.map((item) => {
        if (item.id !== itemId) return item;
        updated = true;
        return { ...item, ...patch };
      });
      if (!updated) {
        next.push({ id: itemId, ...patch });
      }
      return next;
    });
  }

  function appendDeployLog(itemId, chunk) {
    if (!chunk) return;
    setDeployLogs((prev) => {
      const next = { ...prev };
      const current = next[itemId] || "";
      let merged = current ? `${current}\n${chunk}` : chunk;
      if (merged.length > 20000) {
        merged = `...trimmed...\n${merged.slice(-18000)}`;
      }
      next[itemId] = merged;
      return next;
    });
  }

  async function loadDeployItems(jobId) {
    try {
      const items = await request("GET", `/ops/jobs/${jobId}/items`);
      setDeployItems(items || []);
      const logs = {};
      (items || []).forEach((item) => {
        if (item.log) logs[item.id] = item.log;
      });
      setDeployLogs(logs);
    } catch (err) {
      setDeployError(err.message);
    }
  }

  function updateTaskItem(itemId, patch) {
    setTaskItems((prev) => {
      let updated = false;
      const next = prev.map((item) => {
        if (item.id !== itemId) return item;
        updated = true;
        return { ...item, ...patch };
      });
      if (!updated) {
        next.push({ id: itemId, ...patch });
      }
      return next;
    });
  }

  function appendTaskLog(itemId, chunk) {
    if (!chunk) return;
    setTaskLogs((prev) => {
      const next = { ...prev };
      const current = next[itemId] || "";
      let merged = current ? `${current}\n${chunk}` : chunk;
      if (merged.length > 20000) {
        merged = `...trimmed...\n${merged.slice(-18000)}`;
      }
      next[itemId] = merged;
      return next;
    });
  }

  async function loadTaskItems(jobId) {
    try {
      const items = await request("GET", `/ops/jobs/${jobId}/items`);
      setTaskItems(items || []);
      const logs = {};
      (items || []).forEach((item) => {
        if (item.log) logs[item.id] = item.log;
      });
      setTaskLogs(logs);
    } catch (err) {
      setTaskError(err.message);
    }
  }

  async function startDeployAgent() {
    setDeployError("");
    const params = {
      agent_port: Number(deployForm.agent_port) || 9191,
      agent_token_mode: deployForm.agent_token_mode,
      shared_agent_token: deployForm.agent_token_mode === "shared" ? deployForm.shared_agent_token.trim() : "",
      allow_cidr: deployForm.allow_cidr.trim(),
      stats_mode: deployForm.stats_mode,
      xray_access_log_path: deployForm.xray_access_log_path.trim(),
      rate_limit_rps: Number(deployForm.rate_limit_rps) || 5,
      enable_ufw: !!deployForm.enable_ufw,
      health_check: !!deployForm.health_check,
      confirm: deployForm.confirm.trim(),
      sandbox: !!deployForm.sandbox_only,
    };
    if (!deployForm.all && selectedNodeIDs.length === 0) {
      setDeployError(t("Select at least one node"));
      return;
    }
    if (!params.allow_cidr) {
      setDeployError(t("Allow CIDR is required"));
      return;
    }
    if (deployForm.agent_token_mode === "shared" && !params.shared_agent_token) {
      setDeployError(t("Shared token required"));
      return;
    }
    if (deployForm.all && params.confirm !== "DEPLOY_AGENT") {
      setDeployError(t("Type {token} to confirm", { token: "DEPLOY_AGENT" }));
      return;
    }
    const payload = {
      node_ids: deployForm.all ? [] : selectedNodeIDs,
      all: !!deployForm.all,
      parallelism: Number(deployForm.parallelism) || 3,
      params,
    };
    try {
      const job = await request("POST", "/ops/deploy-agent", payload);
      setDeployOpen(false);
      setDeployProgress({ open: true, jobId: job.id, status: job.status || "queued" });
      await loadDeployItems(job.id);
    } catch (err) {
      setDeployError(err.message);
    }
  }

  async function startTask() {
    setTaskError("");
    const selectedNodeIDs = Object.keys(selectedNodes).filter((id) => selectedNodes[id]);
    if (!taskForm.all && selectedNodeIDs.length === 0) {
      setTaskError(t("Select at least one node"));
      return;
    }
    const params = {
      confirm: taskForm.confirm.trim(),
    };
    if (taskForm.type === "restart_service") {
      params.restart_service = taskForm.service;
    }
    if (taskForm.all && params.confirm !== "REALLY_DO_IT") {
      setTaskError(t("Type {token} to confirm", { token: "REALLY_DO_IT" }));
      return;
    }
    const payload = {
      type: taskForm.type,
      node_ids: taskForm.all ? [] : selectedNodeIDs,
      all: !!taskForm.all,
      parallelism: Number(taskForm.parallelism) || 3,
      params,
    };
    try {
      const job = await request("POST", "/tasks/bulk", payload);
      setTaskOpen(false);
      setTaskProgress({ open: true, jobId: job.id, status: job.status || "queued", title: taskForm.type });
      await loadTaskItems(job.id);
    } catch (err) {
      setTaskError(err.message);
    }
  }

  useEffect(() => {
    if (!deployProgress.open || !deployProgress.jobId) return;
    const token = getToken();
    if (!token) return;
    const wsUrl = buildWsUrl(`/ops/jobs/${deployProgress.jobId}/stream?token=${encodeURIComponent(token)}`);
    const ws = new WebSocket(wsUrl);
    deployWsRef.current = ws;
    ws.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data);
        if (payload.type === "job_status") {
          setDeployProgress((prev) => ({ ...prev, status: payload.data?.status || prev.status }));
        }
        if (payload.type === "item_status") {
          updateDeployItem(payload.data?.item_id, {
            status: payload.data?.status,
            stage: payload.data?.stage,
            started_at: payload.data?.started_at,
            finished_at: payload.data?.finished_at,
            node_id: payload.data?.node_id,
          });
        }
        if (payload.type === "item_log_append") {
          appendDeployLog(payload.data?.item_id, payload.data?.chunk);
        }
        if (payload.type === "item_done") {
          updateDeployItem(payload.data?.item_id, {
            status: payload.data?.status,
            exit_code: payload.data?.exit_code,
            error: payload.data?.error,
          });
        }
      } catch {
      }
    };
    ws.onerror = () => {
      // WS is optional; polling will keep status fresh.
    };
    ws.onclose = () => {
      if (deployWsRef.current === ws) {
        deployWsRef.current = null;
      }
    };
    return () => {
      ws.close();
    };
  }, [deployProgress.open, deployProgress.jobId, t]);

  useEffect(() => {
    if (!deployProgress.open || !deployProgress.jobId) return;
    let stopped = false;
    const poll = async () => {
      if (stopped) return;
      try {
        const job = await request("GET", `/ops/jobs/${deployProgress.jobId}`);
        if (job?.status) {
          setDeployProgress((prev) => ({ ...prev, status: job.status }));
        }
        const items = await request("GET", `/ops/jobs/${deployProgress.jobId}/items`);
        if (Array.isArray(items)) {
          setDeployItems(items);
          const logs = {};
          items.forEach((item) => {
            if (item.log) logs[item.id] = item.log;
          });
          setDeployLogs((prev) => ({ ...prev, ...logs }));
        }
        setDeployError("");
        const status = job?.status;
        if (status === "success" || status === "failed") {
          stopped = true;
          return;
        }
      } catch (err) {
        setDeployError(`${t("Failed to get status")}: ${err.message}`);
      }
    };
    const interval = setInterval(poll, 5000);
    poll();
    return () => {
      stopped = true;
      clearInterval(interval);
    };
  }, [deployProgress.open, deployProgress.jobId]);

  useEffect(() => {
    if (!taskProgress.open || !taskProgress.jobId) return;
    const token = getToken();
    if (!token) return;
    const wsUrl = buildWsUrl(`/ops/jobs/${taskProgress.jobId}/stream?token=${encodeURIComponent(token)}`);
    const ws = new WebSocket(wsUrl);
    ws.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data);
        if (payload.type === "job_status") {
          setTaskProgress((prev) => ({ ...prev, status: payload.data?.status || prev.status }));
        }
        if (payload.type === "item_status") {
          updateTaskItem(payload.data?.item_id, {
            status: payload.data?.status,
            stage: payload.data?.stage,
            started_at: payload.data?.started_at,
            finished_at: payload.data?.finished_at,
            node_id: payload.data?.node_id,
          });
        }
        if (payload.type === "item_log_append") {
          appendTaskLog(payload.data?.item_id, payload.data?.chunk);
        }
        if (payload.type === "item_done") {
          updateTaskItem(payload.data?.item_id, {
            status: payload.data?.status,
            exit_code: payload.data?.exit_code,
            error: payload.data?.error,
          });
        }
      } catch {
      }
    };
    ws.onerror = () => {
      setTaskError(`${t("Failed to get status")}: ${t("Disconnected")}`);
    };
    ws.onclose = () => {};
    return () => ws.close();
  }, [taskProgress.open, taskProgress.jobId, t]);

  useEffect(() => {
    if (!taskProgress.open || !taskProgress.jobId) return;
    let stopped = false;
    const poll = async () => {
      if (stopped) return;
      try {
        const job = await request("GET", `/ops/jobs/${taskProgress.jobId}`);
        if (job?.status) {
          setTaskProgress((prev) => ({ ...prev, status: job.status }));
        }
        const items = await request("GET", `/ops/jobs/${taskProgress.jobId}/items`);
        if (Array.isArray(items)) {
          setTaskItems(items);
          const logs = {};
          items.forEach((item) => {
            if (item.log) logs[item.id] = item.log;
          });
          setTaskLogs((prev) => ({ ...prev, ...logs }));
        }
        const status = job?.status;
        if (status === "success" || status === "failed") {
          stopped = true;
          return;
        }
      } catch (err) {
        setTaskError(`${t("Failed to get status")}: ${err.message}`);
      }
    };
    const interval = setInterval(poll, 5000);
    poll();
    return () => {
      stopped = true;
      clearInterval(interval);
    };
  }, [taskProgress.open, taskProgress.jobId]);

  function openAddForm() {
    setAddOpen(true);
    setMenuOpen(false);
  }

  function openServiceAdd(node) {
    setServicesError("");
    setServiceForm({
      kind: "CUSTOM_HTTP",
      url: "",
      health_path: "/",
      expected_status: ["200"],
      headers_json: "{}",
      is_enabled: true,
    });
    setServiceEditor({ open: true, mode: "add", node, service: null });
  }

  function openServiceEdit(node, service) {
    setServicesError("");
    const expected = Array.isArray(service.expected_status)
      ? service.expected_status.map((val) => `${val}`)
      : [];
    const headers = service.headers ? JSON.stringify(service.headers, null, 2) : "{}";
    setServiceForm({
      kind: service.kind || "CUSTOM_HTTP",
      url: service.url || "",
      health_path: service.health_path || "/",
      expected_status: expected.length > 0 ? expected : ["200"],
      headers_json: headers,
      is_enabled: service.is_enabled !== false,
    });
    setServiceEditor({ open: true, mode: "edit", node, service });
  }

  function parseExpected(values) {
    return (values || [])
      .map((val) => parseInt(val, 10))
      .filter((val) => !Number.isNaN(val));
  }

  async function saveService() {
    if (!serviceEditor.node) return;
    setServicesError("");
    let headers = {};
    const rawHeaders = serviceForm.headers_json?.trim();
    if (rawHeaders) {
      try {
        headers = JSON.parse(rawHeaders);
      } catch (err) {
        setServicesError(err.message);
        return;
      }
    }
    const payload = {
      kind: serviceForm.kind,
      url: serviceForm.url || null,
      health_path: serviceForm.health_path || null,
      expected_status: parseExpected(serviceForm.expected_status),
      headers,
      is_enabled: !!serviceForm.is_enabled,
    };
    try {
      if (serviceEditor.mode === "add") {
        const created = await request("POST", `/nodes/${serviceEditor.node.id}/services`, payload);
        setServiceResults((prev) => ({ ...prev, [created.id]: null }));
      } else if (serviceEditor.service) {
        await request("PUT", `/services/${serviceEditor.service.id}`, payload);
      }
      setServiceEditor({ open: false, mode: "add", node: null, service: null });
      loadServices(serviceEditor.node.id);
    } catch (err) {
      setServicesError(err.message);
    }
  }

  async function runService(service) {
    setServicesError("");
    try {
      const res = await request("POST", `/services/${service.id}/run`, {});
      setServiceResults((prev) => ({ ...prev, [service.id]: res }));
    } catch (err) {
      setServicesError(err.message);
    }
  }

  async function toggleService(service, enabled) {
    setServicesError("");
    try {
      await request("PUT", `/services/${service.id}`, { is_enabled: enabled });
      loadServices(service.node_id);
    } catch (err) {
      setServicesError(err.message);
    }
  }

  async function deleteService(service) {
    if (!confirm(t("Delete service?"))) return;
    setServicesError("");
    try {
      await request("DELETE", `/services/${service.id}`, {});
      loadServices(service.node_id);
    } catch (err) {
      setServicesError(err.message);
    }
  }

  function resetBotForm() {
    setBotForm({
      name: "",
      kind: "HTTP",
      docker_container: "",
      systemd_unit: "",
      health_url: "",
      health_path: "/",
      expected_status: ["200"],
      is_enabled: true,
    });
  }

  function openBotAdd(node) {
    resetBotForm();
    setBotEditor({ open: true, mode: "add", node, bot: null });
  }

  function openBotEdit(node, bot) {
    const expected = Array.isArray(bot.expected_status)
      ? bot.expected_status.map((val) => `${val}`)
      : [];
    setBotForm({
      name: bot.name || "",
      kind: bot.kind || "HTTP",
      docker_container: bot.docker_container || "",
      systemd_unit: bot.systemd_unit || "",
      health_url: bot.health_url || "",
      health_path: bot.health_path || "/",
      expected_status: expected.length > 0 ? expected : ["200"],
      is_enabled: bot.is_enabled !== false,
    });
    setBotEditor({ open: true, mode: "edit", node, bot });
  }

  async function saveBot() {
    if (!botEditor.node) return;
    setBotsError("");
    const payload = {
      name: botForm.name,
      kind: botForm.kind,
      docker_container: botForm.docker_container || null,
      systemd_unit: botForm.systemd_unit || null,
      health_url: botForm.health_url || null,
      health_path: botForm.health_path || null,
      expected_status: parseExpected(botForm.expected_status),
      is_enabled: !!botForm.is_enabled,
    };
    try {
      if (botEditor.mode === "add") {
        const created = await request("POST", `/nodes/${botEditor.node.id}/bots`, payload);
        setBotResults((prev) => ({ ...prev, [created.id]: null }));
      } else if (botEditor.bot) {
        await request("PUT", `/bots/${botEditor.bot.id}`, payload);
      }
      setBotEditor({ open: false, mode: "add", node: null, bot: null });
      loadBots(botEditor.node.id);
    } catch (err) {
      setBotsError(err.message);
    }
  }

  async function runBot(bot) {
    setBotsError("");
    try {
      const res = await request("POST", `/bots/${bot.id}/run-now`, {});
      setBotResults((prev) => ({ ...prev, [bot.id]: res }));
    } catch (err) {
      setBotsError(err.message);
    }
  }

  async function toggleBot(bot, enabled) {
    setBotsError("");
    try {
      await request("PUT", `/bots/${bot.id}`, { is_enabled: enabled });
      loadBots(bot.node_id);
    } catch (err) {
      setBotsError(err.message);
    }
  }

  async function deleteBot(bot) {
    if (!confirm(t("Delete bot?"))) return;
    setBotsError("");
    try {
      await request("DELETE", `/bots/${bot.id}`, {});
      loadBots(bot.node_id);
    } catch (err) {
      setBotsError(err.message);
    }
  }

  async function muteBot(bot) {
    setBotsError("");
    try {
      const rows = await request("GET", `/alerts?bot_id=${bot.id}&active=true&limit=1`);
      if (!Array.isArray(rows) || rows.length === 0) {
        setBotsError(t("No active alerts for this bot"));
        return;
      }
      await request("POST", `/alerts/${rows[0].fingerprint}/mute`, { duration: 3600 });
    } catch (err) {
      setBotsError(err.message);
    }
  }

  function botTargetLabel(bot) {
    if (!bot) return "-";
    if (bot.kind === "DOCKER") return bot.docker_container || "-";
    if (bot.kind === "SYSTEMD") return bot.systemd_unit || "-";
    return bot.health_url || "-";
  }

  function renderNodeDetails(node, uptimePoints, metrics) {
    const { success, total } = computeUptime(uptimePoints);
    return (
      <>
        <div className="node-availability">
          <div className="availability-header">
            <div className="muted small">{t("Last {total} checks", { total: total || 0 })}</div>
            <div className="muted small">{t("{success}/{total} successful", { success, total: total || 0 })}</div>
          </div>
          <Sparkline points={uptimePoints} />
        </div>

        <MetricSparks metrics={metrics} />

        <div className="node-meta-grid">
          <div className="meta-box">
            <div className="meta-label">{t("SSH Host")}</div>
            <div className="meta-value">{node.ssh_host || "-"}</div>
          </div>
          <div className="meta-box">
            <div className="meta-label">{t("SSH Port")}</div>
            <div className="meta-value">{node.ssh_port || "-"}</div>
          </div>
          <div className="meta-box">
            <div className="meta-label">{node.kind === "HOST" ? t("Panel") : t("Panel Username")}</div>
            <div className="meta-value">{node.kind === "HOST" ? t("Not used") : (node.panel_username || "-")}</div>
          </div>
        </div>

        <div className="node-actions">
          {!isViewer && (
            <>
              {node.kind !== "HOST" && <Link to={`/nodes/${node.id}/inbounds`} className="link-button">{t("Inbounds")}</Link>}
              <button className="secondary" onClick={() => openEdit(node)}>{t("Edit")}</button>
              {isAdmin && <button className="secondary" onClick={() => openSSH(node)}>{t("SSH")}</button>}
              {node.kind !== "HOST" && <button className="warning" onClick={() => onRestart(node.id)}>{t("Restart Xray")}</button>}
              <button className="danger" onClick={() => onReboot(node.id)}>{t("Reboot")}</button>
            </>
          )}
          {isAdmin && <button className="danger ghost" onClick={() => onDelete(node)}>{t("Delete")}</button>}
        </div>
      </>
    );
  }

  function renderServicesTab(node) {
    const services = servicesMap[node.id] || [];
    return (
      <>
        <div className="services-header">
          <div className="muted small">
            {servicesBusy ? t("Loading...") : t("{count} services", { count: services.length })}
          </div>
          <div className="actions">
            {!isViewer && <button type="button" onClick={() => openServiceAdd(node)}>{t("Add")}</button>}
            <button type="button" className="secondary" onClick={() => loadServices(node.id)}>{t("Refresh")}</button>
          </div>
        </div>
        {servicesError && <div className="error">{servicesError}</div>}
        <div className="table services">
          <div className="table-row head">
            <div>{t("Kind")}</div>
            <div>{t("URL")}</div>
            <div>{t("Path")}</div>
            <div>{t("Expected")}</div>
            <div>{t("Enabled")}</div>
            <div>{t("Last status")}</div>
            <div>{t("Last seen")}</div>
            <div>{t("Latency")}</div>
            <div>{t("Actions")}</div>
          </div>
          {services.map((service) => {
            const last = serviceResults[service.id];
            const expected = (service.expected_status || []).join(", ") || "-";
            return (
              <div className="table-row" key={service.id}>
                <div>{service.kind || "-"}</div>
                <div>{service.url || "-"}</div>
                <div>{service.health_path || "-"}</div>
                <div>{expected}</div>
                <div>{service.is_enabled ? t("On") : t("Off")}</div>
                <div>{last?.status || "-"}</div>
                <div>{last?.ts ? formatTS(last.ts) : "-"}</div>
                <div>{last?.latency_ms != null ? `${last.latency_ms}ms` : "-"}</div>
                <div className="actions">
                  {!isViewer && (
                    <>
                      <button type="button" onClick={() => runService(service)}>{t("Run now")}</button>
                      <button type="button" className="secondary" onClick={() => openServiceEdit(node, service)}>{t("Edit")}</button>
                      <button
                        type="button"
                        className="secondary"
                        onClick={() => toggleService(service, !service.is_enabled)}
                      >
                        {service.is_enabled ? t("Disable") : t("Enable")}
                      </button>
                      <button type="button" className="danger" onClick={() => deleteService(service)}>{t("Delete")}</button>
                    </>
                  )}
                </div>
              </div>
            );
          })}
          {services.length === 0 && (
            <div className="table-row">
              <div className="muted small">{t("No services yet")}</div>
            </div>
          )}
        </div>
      </>
    );
  }

  function renderBotsTable(bots, showNode) {
    return (
      <div className="table bots">
        <div className="table-row head">
          {showNode && <div>{t("Node")}</div>}
          <div>{t("Name")}</div>
          <div>{t("Kind")}</div>
          <div>{t("Target")}</div>
          <div>{t("Enabled")}</div>
          <div>{t("Last status")}</div>
          <div>{t("Last seen")}</div>
          <div>{t("Latency")}</div>
          <div>{t("Actions")}</div>
        </div>
        {bots.map((bot) => {
          const last = botResults[bot.id];
          const node = nodes.find((n) => n.id === bot.node_id);
          const nodeRef = node || { id: bot.node_id, name: "-" };
          const statusValue = (last?.status || "").toLowerCase();
          const badgeStatus = statusValue === "ok"
            ? "online"
            : statusValue === "warn"
              ? "degraded"
              : statusValue === "fail"
                ? "offline"
                : "unknown";
          return (
            <div className="table-row" key={bot.id}>
              {showNode && <div title={nodeRef.name || ""} data-label={t("Node")}>{nodeRef.name || "-"}</div>}
              <div title={bot.name || ""} data-label={t("Name")}>{bot.name || "-"}</div>
              <div data-label={t("Kind")}>{bot.kind || "-"}</div>
              <div title={botTargetLabel(bot)} data-label={t("Target")}>{botTargetLabel(bot)}</div>
              <div data-label={t("Enabled")}>{bot.is_enabled ? t("On") : t("Off")}</div>
              <div className="status-cell" data-label={t("Last status")}>
                <StatusBadge status={badgeStatus} />
                <span>{last?.status || "-"}</span>
                {last?.error && <span className="status-error" title={last.error}>{last.error}</span>}
              </div>
              <div data-label={t("Last seen")}>{last?.ts ? formatTS(last.ts) : "-"}</div>
              <div data-label={t("Latency")}>{last?.latency_ms != null ? `${last.latency_ms}ms` : "-"}</div>
              <div className="actions" data-label={t("Actions")}>
                {!isViewer && (
                  <>
                    <button type="button" onClick={() => runBot(bot)}>{t("Run now")}</button>
                    <button type="button" className="secondary" onClick={() => muteBot(bot)}>{t("Mute 1h")}</button>
                    <button type="button" className="secondary" onClick={() => openBotEdit(nodeRef, bot)}>{t("Edit")}</button>
                    <button
                      type="button"
                      className="secondary"
                      onClick={() => toggleBot(bot, !bot.is_enabled)}
                    >
                      {bot.is_enabled ? t("Disable") : t("Enable")}
                    </button>
                    <button type="button" className="danger" onClick={() => deleteBot(bot)}>{t("Delete")}</button>
                  </>
                )}
              </div>
            </div>
          );
        })}
        {bots.length === 0 && (
          <div className="table-row">
            <div className="muted small">{t("No bots yet")}</div>
          </div>
        )}
      </div>
    );
  }

  function renderBotsTab(node) {
    const bots = botsMap[node.id] || [];
    return (
      <>
        <div className="services-header">
          <div className="muted small">
            {botsBusy ? t("Loading...") : t("{count} bots", { count: bots.length })}
          </div>
          <div className="actions">
            {!isViewer && <button type="button" onClick={() => openBotAdd(node)}>{t("Add")}</button>}
            <button type="button" className="secondary" onClick={() => loadBots(node.id)}>{t("Refresh")}</button>
          </div>
        </div>
        {botsError && <div className="error">{botsError}</div>}
        {renderBotsTable(bots, false)}
      </>
    );
  }

  function renderBotsView() {
    const bots = Object.values(botsMap).flat();
    return (
      <>
        {botsBusy && (
          <div className="muted small bots-status">{t("Loading...")}</div>
        )}
        {botsError && <div className="error bots-status">{botsError}</div>}
        {bots.map((bot) => {
          const last = botResults[bot.id];
          const node = nodes.find((n) => n.id === bot.node_id);
          const nodeRef = node || { id: bot.node_id, name: "-" };
          const statusValue = (last?.status || "").toLowerCase();
          const badgeStatus = statusValue === "ok"
            ? "online"
            : statusValue === "warn"
              ? "degraded"
              : statusValue === "fail"
                ? "offline"
                : "unknown";
          return (
            <div className="node-card" key={bot.id}>
              <div className="node-card-top">
                <div className="node-card-title">
                  <div className="node-name-row">
                    <div className="node-name">{bot.name || "-"}</div>
                    <StatusBadge status={badgeStatus} />
                    <span className="chip subtle">{bot.kind || "-"}</span>
                  </div>
                  <div className="tag-row">
                    <span className="chip subtle">{nodeRef.name || "-"}</span>
                    <span className="chip subtle">{botTargetLabel(bot)}</span>
                  </div>
                  <div className="muted small">{t("Last status")}: {last?.status || "-"}</div>
                  <div className="muted small">{t("Last seen")}: {last?.ts ? formatTS(last.ts) : "-"}</div>
                  <div className="muted small">{t("Latency")}: {last?.latency_ms != null ? `${last.latency_ms}ms` : "-"}</div>
                  {last?.error && <div className="bot-error">{last.error}</div>}
                </div>
                <div className="node-uptime">
                  <div className="uptime-value">{last?.status ? last.status.toUpperCase() : "-"}</div>
                  <div className="uptime-label">{t("Last status")}</div>
                </div>
              </div>
              <div className="node-actions">
                {!isViewer && (
                  <>
                    <button type="button" onClick={() => runBot(bot)}>{t("Run now")}</button>
                    <button type="button" className="secondary" onClick={() => muteBot(bot)}>{t("Mute 1h")}</button>
                    <button type="button" className="secondary" onClick={() => openBotEdit(nodeRef, bot)}>{t("Edit")}</button>
                    <button
                      type="button"
                      className="secondary"
                      onClick={() => toggleBot(bot, !bot.is_enabled)}
                    >
                      {bot.is_enabled ? t("Disable") : t("Enable")}
                    </button>
                    <button type="button" className="danger" onClick={() => deleteBot(bot)}>{t("Delete")}</button>
                  </>
                )}
              </div>
            </div>
          );
        })}
        {bots.length === 0 && !botsBusy && (
          <div className="muted small bots-status">{t("No bots yet")}</div>
        )}
      </>
    );
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

  async function openPasskeys() {
    setMenuOpen(false);
    setPasskeysOpen(true);
    setPasskeysError("");
    setPasskeysOTP("");
    try {
      const status = await request("GET", "/auth/2fa/status");
      setPasskeysTotpRequired(Boolean(status?.enabled));
    } catch (err) {
      setPasskeysTotpRequired(false);
    }
    await loadPasskeys();
  }

  async function loadPasskeys() {
    setPasskeysBusy(true);
    setPasskeysError("");
    try {
      const data = await request("GET", "/auth/webauthn/credentials");
      setPasskeysList(Array.isArray(data) ? data : []);
    } catch (err) {
      setPasskeysError(err.message);
    } finally {
      setPasskeysBusy(false);
    }
  }

  async function loadAgentDeployDefaults() {
    try {
      let data = null;
      try {
        data = await request("GET", "/settings/public");
      } catch {
        data = await request("GET", "/settings/agent-deploy-defaults");
      }
      if (!data) return;
      setDeployForm((prev) => ({
        ...prev,
        allow_cidr: prev.allow_cidr || data.default_allow_cidr || "",
        agent_port: prev.agent_port || data.default_agent_port || 9191,
        stats_mode: prev.stats_mode || data.default_stats_mode || "log",
        xray_access_log_path: prev.xray_access_log_path || data.default_xray_access_log_path || "/var/log/xray/access.log",
        rate_limit_rps: prev.rate_limit_rps || data.default_rate_limit_rps || 5,
        health_check: typeof data.default_health_check === "boolean" ? data.default_health_check : prev.health_check,
        enable_ufw: typeof data.default_enable_ufw === "boolean" ? data.default_enable_ufw : prev.enable_ufw,
        parallelism: prev.parallelism || data.default_parallelism || 3,
      }));
    } catch (err) {
      setDeployError(err.message);
    }
  }

  async function registerPasskey() {
    setPasskeysError("");
    if (!window.PublicKeyCredential) {
      setPasskeysError(t("Passkeys are not supported in this browser."));
      return;
    }
    setPasskeysBusy(true);
    try {
      const attemptRegister = async (retry) => {
        const options = await request("POST", "/auth/webauthn/register/options", {
          otp: passkeysTotpRequired ? passkeysOTP.trim() : "",
        });
        const publicKey = prepareCreationOptions(options.publicKey || options.options || options);
        const cred = await navigator.credentials.create({ publicKey });
        const payload = publicKeyCredentialToJSON(cred);
        try {
          await request("POST", "/auth/webauthn/register/verify", { credential: payload });
        } catch (err) {
          const code = err?.data?.error?.code;
          if (!retry && (code === "WEBAUTHN_CHALLENGE_EXPIRED" || code === "WEBAUTHN_CHALLENGE_NOT_FOUND")) {
            return attemptRegister(true);
          }
          throw err;
        }
      };
      await attemptRegister(false);
      await loadPasskeys();
      setPasskeysOTP("");
    } catch (err) {
      setPasskeysError(err.message);
    } finally {
      setPasskeysBusy(false);
    }
  }

  async function deletePasskey(id) {
    setPasskeysError("");
    try {
      await request("DELETE", `/auth/webauthn/credentials/${id}`);
      await loadPasskeys();
    } catch (err) {
      setPasskeysError(err.message);
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
      setTotpMessage(t("Scan the QR in Google Authenticator and enter the code below."));
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
      setTotpMessage(t("2FA enabled"));
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
      setTotpMessage(t("2FA disabled"));
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
    if (!confirm(t("Delete user {name}?", { name: user.username }))) return;
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
          <button ref={menuButtonRef} className="icon-button" onClick={() => setMenuOpen((v) => !v)} aria-label={t("Menu")}>
            ☰
          </button>
          <h2>{t("Nodes")}</h2>
          {menuOpen && (
            <div className="menu" ref={menuRef}>
              {(isAdmin || isOperator) && <button type="button" onClick={openAddForm}>{t("Add node")}</button>}
              <button type="button" onClick={() => { setMenuOpen(false); navigate("/dashboard"); }}>{t("Dashboard")}</button>
              <button type="button" onClick={() => { setMenuOpen(false); navigate("/files"); }}>{t("Files")}</button>
              {isAdmin && <button type="button" onClick={async () => { setUsersOpen(true); setMenuOpen(false); await loadUsers(); }}>{t("Users & roles")}</button>}
              {!isViewer && <button type="button" onClick={openTOTP}>{t("2FA settings")}</button>}
              {!isViewer && <button type="button" onClick={openPasskeys}>{t("Passkeys")}</button>}
              {isAdmin && (
                <button type="button" onClick={async () => {
                  setMenuOpen(false);
                  setTelegramSaved(false);
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
                }}>{t("Telegram alerts")}</button>
              )}
              {isAdmin && <button type="button" onClick={openAudit}>{t("Audit log")}</button>}
            </div>
          )}
        </div>
        <div className="header-right">
          <div className="language-card">
            <div className="muted small">{t("Language")}</div>
            <select value={lang} onChange={(e) => setLang(e.target.value)}>
              <option value="en">{t("English")}</option>
              <option value="ru">{t("Russian")}</option>
              <option value="fa">{t("Persian")}</option>
            </select>
          </div>
          <div className="header-user">
            {user && <span className="muted small">{t("Signed in: {user}", { user })}</span>}
            <button
              onClick={async () => {
                try {
                  await request("POST", "/auth/logout", {});
                } catch {
                }
                clearAuth();
                navigate("/login", { replace: true });
              }}
            >
              {t("Logout")}
            </button>
          </div>
        </div>
      </header>

      {error && <div className="error">{error}</div>}

      {actionPlan.open && (
        <div className="modal action-plan-modal">
          <div className="modal-content">
            <h3>{t("Confirm action")}</h3>
            <div className="plan-steps">
              <div className="muted small">{t("Will run on node {name}:", { name: actionPlan.node?.name || "" })}</div>
              <ul>
                {actionPlan.steps.map((step, idx) => (
                  <li key={`${actionPlan.action}-${idx}`}>{step}</li>
                ))}
              </ul>
            </div>
            {actionConfirmToken(actionPlan.action) && (
              <label>
                {t("Type {token} to confirm", { token: actionConfirmToken(actionPlan.action) })}
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
                {t("Cancel")}
              </button>
              <button type="button" onClick={runActionPlan} disabled={actionBusy}>
                {actionBusy ? t("Running...") : t("Run")}
              </button>
            </div>
          </div>
        </div>
      )}

      <div className="nodes-cards">
        <div className="nodes-cards-head">
          <div>
            <h3>{t("Nodes Manager")}</h3>
            <div className="muted">
              {showingBots
                ? t("Bots: {count}", { count: botCount })
                : t("Servers configured: {count}", { count: filteredNodes.length })}
            </div>
          </div>
          {!showingBots && (isAdmin || isOperator) && (
            <div className="node-actions">
              <div className="muted small">{t("Selected: {count}", { count: selectedNodeIDs.length })}</div>
              <button type="button" className="secondary" onClick={() => selectAllFilteredNodes(filteredNodes)} disabled={filteredNodes.length === 0}>
                {t("Select all")}
              </button>
              <button type="button" className="secondary" onClick={clearSelectedNodes} disabled={selectedNodeIDs.length === 0}>
                {t("Clear")}
              </button>
              <button type="button" onClick={openDeployAgent} disabled={filteredNodes.length === 0}>
                {t("Deploy agent")}
              </button>
              <button type="button" className="secondary" onClick={() => openTaskModal("update_panel")} disabled={filteredNodes.length === 0}>
                {t("Update panels")}
              </button>
              <button type="button" className="secondary" onClick={() => openTaskModal("reboot_node")} disabled={filteredNodes.length === 0}>
                {t("Reboot nodes")}
              </button>
              <button type="button" className="secondary" onClick={() => openTaskModal("restart_service")} disabled={filteredNodes.length === 0}>
                {t("Restart service")}
              </button>
            </div>
          )}
          <div className="node-type-toggle">
            <button
              type="button"
              className={`toggle-pill ${nodeTypeFilter === "PANEL" ? "active" : ""}`}
              onClick={() => setNodeTypeFilter("PANEL")}
            >
              {t("3x-ui Panels")}
            </button>
            <button
              type="button"
              className={`toggle-pill ${nodeTypeFilter === "HOST" ? "active" : ""}`}
              onClick={() => setNodeTypeFilter("HOST")}
            >
              {t("Hosts")}
            </button>
            <button
              type="button"
              className={`toggle-pill ${nodeTypeFilter === "BOT" ? "active" : ""}`}
              onClick={() => setNodeTypeFilter("BOT")}
            >
              {t("Bots")}
            </button>
          </div>
        </div>

        {showingBots && <div className="bots-view">{renderBotsView()}</div>}

        {!showingBots && filteredNodes.map((node) => {
          const uptimePoints = uptimeMap[node.id] || [];
          const { percent } = computeUptime(uptimePoints);
          const lastTs = uptimePoints[uptimePoints.length - 1]?.ts;
          const lastPoint = uptimePoints[uptimePoints.length - 1];
          const panelIssue = formatPanelIssue(lastPoint, t);

          return (
            <div className="node-card" key={node.id}>
              <div className="node-card-top">
                {(isAdmin || isOperator) && (
                  <label className="node-card-select">
                    <input
                      type="checkbox"
                      checked={!!selectedNodes[node.id]}
                      onChange={() => toggleNodeSelection(node.id)}
                    />
                  </label>
                )}
                <div className="node-card-title">
                  <div className="node-name-row">
                    <div className="node-name">{node.name || t("Unnamed node")}</div>
                    <StatusBadge status={statusMap[node.id]?.status} />
                    <span className="chip subtle">{node.kind || "PANEL"}</span>
                  </div>
                  <div className="tag-row">
                    {(node.tags || []).length > 0 ? (
                      (node.tags || []).map((tag, idx) => (
                        <span className="chip subtle" key={`${node.id}-tag-${idx}`}>{tag}</span>
                      ))
                    ) : (
                      <span className="muted small">{t("No tags")}</span>
                    )}
                  </div>
                  <div className="node-link">
                    {node.kind === "HOST" ? (
                      <span className="muted small">{t("Base URL: not used")}</span>
                    ) : node.base_url ? (
                      <a href={node.base_url} target="_blank" rel="noreferrer">
                        {node.base_url} ↗
                      </a>
                    ) : (
                      <span className="muted small">{t("No base URL")}</span>
                    )}
                  </div>
                  <div className="node-versions">
                    {node.kind === "HOST" ? (
                      <span className="muted small">{t("Panel: not used")}</span>
                    ) : (
                      <span className="muted small">{t("Panel: {panel}", { panel: node.panel_version || t("unknown") })}</span>
                    )}
                    <span className="muted small">{t("Xray: {xray}", { xray: node.xray_version || t("unknown") })}</span>
                    <span className="muted small">
                      {node.agent_installed
                        ? node.agent_online
                          ? t("Agent online")
                          : t("Agent offline")
                        : t("No agent")}
                      {node.agent_version ? ` v${node.agent_version}` : ""}
                    </span>
                  </div>
                  {panelIssue && (
                    <div className="muted small" title={lastPoint?.panel_error_detail || ""}>
                      {t("Panel issue: {reason}", { reason: panelIssue })}
                    </div>
                  )}
                  {lastTs && <div className="muted small">{t("Last check: {ts}", { ts: formatTS(lastTs) })}</div>}
                </div>
                <div className="node-uptime">
                  <div className="uptime-value">{percent.toFixed(1)}%</div>
                  <div className="uptime-label">{t("Uptime")}</div>
                  <button
                    type="button"
                    className="icon-button"
                    onClick={() => {
                      setNodeTab("overview");
                      setServicesError("");
                      setNodeDetails({ open: true, node });
                    }}
                    aria-label={t("Expand")}
                  >
                    {t("Expand")}
                  </button>
                </div>
              </div>

            </div>
          );
        })}
      </div>

      {nodeDetails.open && nodeDetails.node && (
        <div className="modal node-details-modal">
          <div className="modal-content node-details-content">
            <div className="node-details-header">
              <div>
                <div className="node-name">{nodeDetails.node.name || t("Unnamed node")}</div>
                <div className="muted small">{nodeDetails.node.kind || "PANEL"}</div>
                <div className="muted small">{nodeDetails.node.kind === "HOST" ? t("Base URL: not used") : (nodeDetails.node.base_url || t("No base URL"))}</div>
              </div>
              <button type="button" onClick={() => setNodeDetails({ open: false, node: null })}>{t("Close")}</button>
            </div>
            <div className="tabs">
              <button
                type="button"
                className={`tab ${nodeTab === "overview" ? "active" : ""}`}
                onClick={() => setNodeTab("overview")}
              >
                {t("Overview")}
              </button>
              <button
                type="button"
                className={`tab ${nodeTab === "services" ? "active" : ""}`}
                onClick={() => setNodeTab("services")}
              >
                {t("Services")}
              </button>
              <button
                type="button"
                className={`tab ${nodeTab === "bots" ? "active" : ""}`}
                onClick={() => setNodeTab("bots")}
              >
                {t("Bots")}
              </button>
            </div>
            {nodeTab === "overview" && renderNodeDetails(
              nodeDetails.node,
              uptimeMap[nodeDetails.node.id] || [],
              metricsMap[nodeDetails.node.id] || []
            )}
            {nodeTab === "services" && renderServicesTab(nodeDetails.node)}
            {nodeTab === "bots" && renderBotsTab(nodeDetails.node)}
          </div>
        </div>
      )}

      {serviceEditor.open && (
        <div className="modal overlay-modal">
          <div className="modal-content">
            <h3>{serviceEditor.mode === "add" ? t("Add Service") : t("Edit Service")}</h3>
            <div className="form-grid" autoComplete="off">
              <select
                value={serviceForm.kind}
                onChange={(e) => setServiceForm({ ...serviceForm, kind: e.target.value })}
              >
                <option value="CUSTOM_HTTP">CUSTOM_HTTP</option>
              </select>
              <input
                placeholder={t("URL")}
                value={serviceForm.url}
                onChange={(e) => setServiceForm({ ...serviceForm, url: e.target.value })}
              />
              <input
                placeholder={t("Health path")}
                value={serviceForm.health_path}
                onChange={(e) => setServiceForm({ ...serviceForm, health_path: e.target.value })}
              />
              <ListInput
                label={t("Expected status")}
                values={serviceForm.expected_status}
                placeholder="200"
                onChange={(values) => setServiceForm({ ...serviceForm, expected_status: values })}
              />
              <textarea
                rows="4"
                placeholder={t("Headers (JSON)")}
                value={serviceForm.headers_json}
                onChange={(e) => setServiceForm({ ...serviceForm, headers_json: e.target.value })}
              />
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={serviceForm.is_enabled}
                  onChange={(e) => setServiceForm({ ...serviceForm, is_enabled: e.target.checked })}
                />
                {t("Enabled")}
              </label>
            </div>
            {servicesError && <div className="error">{servicesError}</div>}
            <div className="actions">
              <button type="button" onClick={() => saveService()}>{t("Save")}</button>
              <button type="button" onClick={() => setServiceEditor({ open: false, mode: "add", node: null, service: null })}>{t("Cancel")}</button>
            </div>
          </div>
        </div>
      )}

      {botEditor.open && (
        <div className="modal overlay-modal">
          <div className="modal-content">
            <h3>{botEditor.mode === "add" ? t("Add Bot") : t("Edit Bot")}</h3>
            <div className="form-grid" autoComplete="off">
              <input
                placeholder={t("Name")}
                value={botForm.name}
                onChange={(e) => setBotForm({ ...botForm, name: e.target.value })}
              />
              <select
                value={botForm.kind}
                onChange={(e) => setBotForm({ ...botForm, kind: e.target.value })}
              >
                <option value="HTTP">HTTP</option>
                <option value="DOCKER">DOCKER</option>
                <option value="SYSTEMD">SYSTEMD</option>
              </select>
              {botForm.kind === "HTTP" && (
                <>
                  <input
                    placeholder={t("Health URL")}
                    value={botForm.health_url}
                    onChange={(e) => setBotForm({ ...botForm, health_url: e.target.value })}
                  />
                  <input
                    placeholder={t("Health path")}
                    value={botForm.health_path}
                    onChange={(e) => setBotForm({ ...botForm, health_path: e.target.value })}
                  />
                  <ListInput
                    label={t("Expected status")}
                    values={botForm.expected_status}
                    placeholder="200"
                    onChange={(values) => setBotForm({ ...botForm, expected_status: values })}
                  />
                </>
              )}
              {botForm.kind === "DOCKER" && (
                <input
                  placeholder={t("Docker container")}
                  value={botForm.docker_container}
                  onChange={(e) => setBotForm({ ...botForm, docker_container: e.target.value })}
                />
              )}
              {botForm.kind === "SYSTEMD" && (
                <input
                  placeholder={t("Systemd unit")}
                  value={botForm.systemd_unit}
                  onChange={(e) => setBotForm({ ...botForm, systemd_unit: e.target.value })}
                />
              )}
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={botForm.is_enabled}
                  onChange={(e) => setBotForm({ ...botForm, is_enabled: e.target.checked })}
                />
                {t("Enabled")}
              </label>
            </div>
            {botsError && <div className="error">{botsError}</div>}
            <div className="actions">
              <button type="button" onClick={() => saveBot()}>{t("Save")}</button>
              <button type="button" onClick={() => setBotEditor({ open: false, mode: "add", node: null, bot: null })}>
                {t("Cancel")}
              </button>
            </div>
          </div>
        </div>
      )}

      {editModal.open && editModal.node && (
        <div className="modal edit-node-modal">
          <div className="modal-content">
            <h3>{t("Edit Node")}</h3>
            <form className="form-grid" onSubmit={onUpdate} autoComplete="off">
              <select
                name="node_kind"
                value={editKind}
                onChange={(e) => setEditKind(e.target.value)}
              >
                <option value="PANEL">{t("Panel node")}</option>
                <option value="HOST">{t("Host node")}</option>
              </select>
              <input name="node_name" autoComplete="off" placeholder={t("Name")} defaultValue={editModal.node.name} />
              <input name="node_tags" autoComplete="off" placeholder={t("Tags (comma)")} defaultValue={(editModal.node.tags || []).join(", ")} />
              {editKind === "PANEL" && (
                <>
                  <input name="node_base_url" autoComplete="off" placeholder={t("Base URL")} defaultValue={editModal.node.base_url} />
                  <input name="node_panel_user" autoComplete="off" placeholder={t("Panel Username")} defaultValue={editModal.node.panel_username} />
                  <input name="node_panel_password" autoComplete="new-password" placeholder={t("Panel Password (leave blank to keep)")} type="password" />
                </>
              )}
              <input name="node_ssh_host" autoComplete="off" placeholder={t("SSH Host")} defaultValue={editModal.node.ssh_host} />
              <input name="node_ssh_port" autoComplete="off" placeholder={t("SSH Port")} type="number" defaultValue={editModal.node.ssh_port} />
              <input name="node_ssh_user" autoComplete="off" placeholder={t("SSH User")} defaultValue={editModal.node.ssh_user} />
              <textarea name="node_ssh_key" autoComplete="off" placeholder={t("SSH Private Key (leave blank to keep)")} rows="3" />
              {editKind === "PANEL" && (
                <label className="checkbox">
                  <input name="verify_tls" type="checkbox" defaultChecked={editModal.node.verify_tls} />
                  {t("Verify TLS")}
                </label>
              )}
              {editValidation && (
                <div className="validation-summary">
                  {editValidation.error && <div className="error">{editValidation.error}</div>}
                  <ValidationBadge
                    label={t("SSH")}
                    status={editValidation.ssh?.ok ? "ok" : "error"}
                    detail={editValidation.ssh?.ok ? editValidation.ssh.fingerprint : editValidation.ssh?.error}
                  />
                  {editKind === "PANEL" && (
                    <>
                      <ValidationBadge
                        label={t("Base URL")}
                        status={editValidation.base_url?.ok ? "ok" : "error"}
                        detail={editValidation.base_url?.ok ? `HTTP ${editValidation.base_url.status_code}` : editValidation.base_url?.error}
                      />
                      <ValidationBadge
                        label={t("Panel")}
                        status={editValidation.panel_version && editValidation.panel_version !== "unknown" ? "ok" : "error"}
                        detail={editValidation.panel_version || t("unknown")}
                      />
                    </>
                  )}
                  <ValidationBadge
                    label={t("Xray")}
                    status={editValidation.xray_version && editValidation.xray_version !== "unknown" ? "ok" : "error"}
                    detail={editValidation.xray_version || t("unknown")}
                  />
                  {editValidation.ssh?.passphrase_required && (
                    <span className="muted small">{t("Passphrase required for SSH key")}</span>
                  )}
                </div>
              )}
              <div className="actions">
                <button type="button" onClick={() => onTest(editModal.node.id)}>{t("Test")}</button>
                <button type="button" onClick={() => setEditModal({ open: false, node: null })}>{t("Cancel")}</button>
                <button type="button" onClick={(e) => onValidateEdit(e.currentTarget.form)} disabled={editValidating}>
                  {editValidating ? t("Validating...") : t("Validate")}
                </button>
                <button type="submit">{t("Save")}</button>
              </div>
            </form>
          </div>
        </div>
      )}

      {addOpen && (
        <div className="modal overlay-modal">
          <div className="modal-content wide">
            <h3>{t("Add Node")}</h3>
            <form className="form-grid" onSubmit={onCreate} autoComplete="off">
              <select
                name="node_kind"
                value={form.kind}
                onChange={(e) => setForm({ ...form, kind: e.target.value })}
              >
                <option value="PANEL">{t("Panel node")}</option>
                <option value="HOST">{t("Host node")}</option>
                <option value="BOT">{t("Bot node")}</option>
              </select>
              {form.kind === "BOT" && (
                <div className="hint">{t("Bot node hint")}</div>
              )}
              <input name="node_name" autoComplete="off" placeholder={t("Name")} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
              <input name="node_tags" autoComplete="off" placeholder={t("Tags (comma)")} value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} />
              {form.kind === "PANEL" && (
                <>
                  <input name="node_base_url" autoComplete="off" placeholder={t("Base URL")} value={form.base_url} onChange={(e) => setForm({ ...form, base_url: e.target.value })} />
                  <input name="node_panel_user" autoComplete="off" placeholder={t("Panel Username")} value={form.panel_username} onChange={(e) => setForm({ ...form, panel_username: e.target.value })} />
                  <input name="node_panel_password" autoComplete="new-password" placeholder={t("Panel Password")} type="password" value={form.panel_password} onChange={(e) => setForm({ ...form, panel_password: e.target.value })} />
                </>
              )}
              <input name="node_ssh_host" autoComplete="off" placeholder={t("SSH Host")} value={form.ssh_host} onChange={(e) => setForm({ ...form, ssh_host: e.target.value })} />
              <input name="node_ssh_port" autoComplete="off" placeholder={t("SSH Port")} type="number" value={form.ssh_port} onChange={(e) => setForm({ ...form, ssh_port: Number(e.target.value) })} />
              <input name="node_ssh_user" autoComplete="off" placeholder={t("SSH User")} value={form.ssh_user} onChange={(e) => setForm({ ...form, ssh_user: e.target.value })} />
              <input name="node_key_passphrase" autoComplete="new-password" placeholder={t("Key Passphrase (optional)")} type="password" value={keyPassphrase} onChange={(e) => setKeyPassphrase(e.target.value)} />
              <label className="file-input">
                {t("Upload SSH Key (.ppk/.pem/.key)")}
                <input type="file" accept=".ppk,.pem,.key" onChange={onKeyUpload} />
              </label>
              <textarea name="node_ssh_key" autoComplete="off" placeholder={t("SSH Private Key")} rows="3" value={form.ssh_key} onChange={(e) => setForm({ ...form, ssh_key: e.target.value })} />
              <div className="hint">{t("Paste OpenSSH private key or upload .ppk")}</div>
              {keyFingerprint && <div className="hint">{t("Fingerprint: {fp}", { fp: keyFingerprint })}</div>}
              {form.kind === "PANEL" && (
                <label className="checkbox">
                  <input type="checkbox" checked={form.verify_tls} onChange={(e) => setForm({ ...form, verify_tls: e.target.checked })} />
                  {t("Verify TLS")}
                </label>
              )}
              <div className="actions">
                <button type="button" onClick={onValidateCreate} disabled={validating}>
                  {validating ? t("Validating...") : t("Validate")}
                </button>
                <button type="submit">{t("Create")}</button>
                <button type="button" onClick={() => setAddOpen(false)}>{t("Close")}</button>
              </div>
            </form>

            {validation && (
              <div className="validation-summary">
                {validation.error && <div className="error">{validation.error}</div>}
                <ValidationBadge
                  label={t("SSH")}
                  status={validation.ssh?.ok ? "ok" : "error"}
                  detail={validation.ssh?.ok ? validation.ssh.fingerprint : validation.ssh?.error}
                />
                {form.kind === "PANEL" && (
                  <>
                    <ValidationBadge
                      label={t("Base URL")}
                      status={validation.base_url?.ok ? "ok" : "error"}
                      detail={validation.base_url?.ok ? `HTTP ${validation.base_url.status_code}` : validation.base_url?.error}
                    />
                    <ValidationBadge
                      label={t("Panel")}
                      status={validation.panel_version && validation.panel_version !== "unknown" ? "ok" : "error"}
                      detail={validation.panel_version || t("unknown")}
                    />
                  </>
                )}
                <ValidationBadge
                  label={t("Xray")}
                  status={validation.xray_version && validation.xray_version !== "unknown" ? "ok" : "error"}
                  detail={validation.xray_version || t("unknown")}
                />
                {validation.ssh?.passphrase_required && (
                  <span className="muted small">{t("Passphrase required for SSH key")}</span>
                )}
              </div>
            )}
          </div>
        </div>
      )}

      {telegramOpen && (
        <div className="modal overlay-modal">
          <div className="modal-content">
            <h3>{t("Telegram alerts")}</h3>
            <div className="form-grid" autoComplete="off">
              <input
                placeholder={telegramTokenSet ? t("Bot token (leave blank to keep)") : t("Bot token")}
                type="password"
                name="telegram_bot_token"
                autoComplete="new-password"
                value={telegramForm.bot_token}
                onChange={(e) => setTelegramForm({ ...telegramForm, bot_token: e.target.value })}
              />
              <ListInput
                label={t("Admin chat IDs")}
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
                {t("Connection loss")}
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={telegramForm.alert_cpu}
                  onChange={(e) => setTelegramForm({ ...telegramForm, alert_cpu: e.target.checked })}
                />
                {t("High CPU")}
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={telegramForm.alert_memory}
                  onChange={(e) => setTelegramForm({ ...telegramForm, alert_memory: e.target.checked })}
                />
                {t("High memory")}
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={telegramForm.alert_disk}
                  onChange={(e) => setTelegramForm({ ...telegramForm, alert_disk: e.target.checked })}
                />
                {t("Low disk space")}
              </label>
            </div>
            <div className="audit-controls">
              <input
                name="telegram_test_message"
                autoComplete="off"
                placeholder={t("Test message (optional)")}
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
                {t("Send test")}
              </button>
            </div>
            {telegramSaved && <div className="hint">{t("Saved")}</div>}
            {telegramTestStatus && <div className="hint">{telegramTestStatus}</div>}
            {telegramTestResults.length > 0 && (
              <div className="table compact">
                <div className="table-row head">
                  <div>{t("Chat ID")}</div>
                  <div>{t("Status")}</div>
                  <div>{t("error")}</div>
                </div>
                {telegramTestResults.map((row) => (
                  <div className="table-row" key={row.chat_id}>
                    <div>{row.chat_id}</div>
                    <div>{row.ok ? t("ok") : t("error")}</div>
                    <div>{row.error || "-"}</div>
                  </div>
                ))}
              </div>
            )}
            <div className="actions">
              <button
                type="button"
                onClick={async () => {
                  setTelegramSaved(false);
                  try {
                    await saveTelegramSettings(telegramForm);
                    setTelegramForm({ ...telegramForm, bot_token: "" });
                    setTelegramSaved(true);
                  } catch (err) {
                    setError(err.message);
                  }
                }}
              >
                {t("Save")}
              </button>
              <button type="button" onClick={() => setTelegramOpen(false)}>{t("Close")}</button>
            </div>
          </div>
        </div>
      )}

      {totpOpen && (
        <div className="modal overlay-modal">
          <div className="modal-content">
            <h3>{t("2FA (TOTP)")}</h3>
            <div className="form-grid" autoComplete="off">
              <div className="hint">
                {t("Steps: Generate QR -> scan in Google Authenticator -> enter code -> enable.")}
              </div>
              <div className="hint">
                {totpStatus?.required ? t("Required for your role.") : t("Optional for your role.")}
              </div>
              <div className="hint">
                {t("Status: {status}", { status: totpStatus?.enabled ? t("connected") : t("disconnected") })}
              </div>
              {!totpStatus?.enabled && (
                <button type="button" className="btn-inline" onClick={setupTOTP}>
                  {t("Generate QR")}
                </button>
              )}
              {totpSetup && (
                <>
                  <img className="qr-img" src={totpSetup.qr_png} alt={t("TOTP QR")} />
                  <div className="hint">{t("Secret: {secret}", { secret: totpSetup.secret })}</div>
                  <input
                    name="totp_code"
                    autoComplete="one-time-code"
                    placeholder={t("Enter code")}
                    value={totpCode}
                    onChange={(e) => setTotpCode(e.target.value)}
                  />
                  <button type="button" className="btn-inline" onClick={verifyTOTP}>
                    {t("Enable 2FA")}
                  </button>
                </>
              )}
              {totpStatus?.enabled && (
                <>
                  <input
                    name="totp_disable_code"
                    autoComplete="one-time-code"
                    placeholder={t("Code to disable")}
                    value={totpDisableCode}
                    onChange={(e) => setTotpDisableCode(e.target.value)}
                  />
                  <input
                    name="totp_recovery_code"
                    autoComplete="off"
                    placeholder={t("Recovery code (optional)")}
                    value={totpRecoveryCode}
                    onChange={(e) => setTotpRecoveryCode(e.target.value)}
                  />
                  <button type="button" className="btn-inline" onClick={disableTOTP}>
                    {t("Disable 2FA")}
                  </button>
                </>
              )}
            </div>
            {totpMessage && <div className="hint">{totpMessage}</div>}
            <div className="actions">
              <button type="button" onClick={() => setTotpOpen(false)}>{t("Close")}</button>
            </div>
          </div>
        </div>
      )}

      {deployOpen && (
        <div className="modal overlay-modal">
          <div className="modal-content wide">
            <h3>{t("Deploy agent")}</h3>
            <div className="form-grid" autoComplete="off">
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={deployForm.all}
                  onChange={(e) => setDeployForm({ ...deployForm, all: e.target.checked })}
                />
                {t("All nodes")}
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={deployForm.sandbox_only}
                  onChange={(e) => setDeployForm({ ...deployForm, sandbox_only: e.target.checked })}
                />
                {t("Sandbox only")}
              </label>
              <input
                type="number"
                placeholder={t("Agent port")}
                value={deployForm.agent_port}
                onChange={(e) => setDeployForm({ ...deployForm, agent_port: Number(e.target.value) })}
              />
              <select
                value={deployForm.agent_token_mode}
                onChange={(e) => setDeployForm({ ...deployForm, agent_token_mode: e.target.value })}
              >
                <option value="per-node">{t("Token per node")}</option>
                <option value="shared">{t("Shared token")}</option>
              </select>
              {deployForm.agent_token_mode === "shared" && (
                <input
                  placeholder={t("Shared token")}
                  value={deployForm.shared_agent_token}
                  onChange={(e) => setDeployForm({ ...deployForm, shared_agent_token: e.target.value })}
                />
              )}
              <input
                placeholder={t("Allow CIDR")}
                value={deployForm.allow_cidr}
                onChange={(e) => setDeployForm({ ...deployForm, allow_cidr: e.target.value })}
              />
              <select
                value={deployForm.stats_mode}
                onChange={(e) => setDeployForm({ ...deployForm, stats_mode: e.target.value })}
              >
                <option value="log">log</option>
                <option value="xray_api">xray_api</option>
              </select>
              <input
                placeholder={t("Xray access log path")}
                value={deployForm.xray_access_log_path}
                onChange={(e) => setDeployForm({ ...deployForm, xray_access_log_path: e.target.value })}
              />
              <input
                type="number"
                placeholder={t("Rate limit (rps)")}
                value={deployForm.rate_limit_rps}
                onChange={(e) => setDeployForm({ ...deployForm, rate_limit_rps: Number(e.target.value) })}
              />
              <input
                type="number"
                placeholder={t("Parallelism")}
                value={deployForm.parallelism}
                onChange={(e) => setDeployForm({ ...deployForm, parallelism: Number(e.target.value) })}
              />
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={deployForm.enable_ufw}
                  onChange={(e) => setDeployForm({ ...deployForm, enable_ufw: e.target.checked })}
                />
                {t("Enable UFW")}
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={deployForm.health_check}
                  onChange={(e) => setDeployForm({ ...deployForm, health_check: e.target.checked })}
                />
                {t("Health check")}
              </label>
              {deployForm.all && (
                <input
                  placeholder={t("Type {token} to confirm", { token: "DEPLOY_AGENT" })}
                  value={deployForm.confirm}
                  onChange={(e) => setDeployForm({ ...deployForm, confirm: e.target.value })}
                />
              )}
              <div className="muted small">
                {t("Selected: {count}", { count: selectedNodeIDs.length })}
              </div>
            </div>
            {deployError && <div className="error">{deployError}</div>}
            <div className="actions">
              <button type="button" onClick={startDeployAgent}>{t("Start deploy")}</button>
              <button type="button" onClick={() => setDeployOpen(false)}>{t("Close")}</button>
            </div>
          </div>
        </div>
      )}

      {taskOpen && (
        <div className="modal overlay-modal">
          <div className="modal-content wide">
            <h3>{t("Bulk action")}</h3>
            <div className="form-grid" autoComplete="off">
              <select
                value={taskForm.type}
                onChange={(e) => setTaskForm({ ...taskForm, type: e.target.value })}
              >
                <option value="update_panel">{t("Update panels")}</option>
                <option value="reboot_node">{t("Reboot nodes")}</option>
                <option value="restart_service">{t("Restart service")}</option>
              </select>
              {taskForm.type === "restart_service" && (
                <select
                  value={taskForm.service}
                  onChange={(e) => setTaskForm({ ...taskForm, service: e.target.value })}
                >
                  <option value="3x-ui">3x-ui</option>
                  <option value="xray">xray</option>
                  <option value="sing-box">sing-box</option>
                  <option value="docker">docker</option>
                  <option value="adguard">adguard</option>
                  <option value="agent">agent</option>
                </select>
              )}
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={taskForm.all}
                  onChange={(e) => setTaskForm({ ...taskForm, all: e.target.checked })}
                />
                {t("All nodes")}
              </label>
              <input
                type="number"
                placeholder={t("Parallelism")}
                value={taskForm.parallelism}
                onChange={(e) => setTaskForm({ ...taskForm, parallelism: Number(e.target.value) })}
              />
              {taskForm.all && (
                <input
                  placeholder={t("Type {token} to confirm", { token: "REALLY_DO_IT" })}
                  value={taskForm.confirm}
                  onChange={(e) => setTaskForm({ ...taskForm, confirm: e.target.value })}
                />
              )}
              <div className="muted small">
                {t("Selected: {count}", { count: selectedNodeIDs.length })}
              </div>
            </div>
            {taskError && <div className="error">{taskError}</div>}
            <div className="actions">
              <button type="button" onClick={startTask}>{t("Start task")}</button>
              <button type="button" onClick={() => setTaskOpen(false)}>{t("Close")}</button>
            </div>
          </div>
        </div>
      )}

      {deployProgress.open && (
        <div className="modal overlay-modal">
          <div className="modal-content wide">
            <div className="deploy-header">
              <div>
                <h3>{t("Deploy agent progress")}</h3>
                <div className="muted small">{t("Job")}: {deployProgress.jobId}</div>
              </div>
              <button type="button" onClick={closeDeployProgress}>{t("Close")}</button>
            </div>
            {deployError && <div className="error">{deployError}</div>}
            <div className="deploy-status">
              {t("Status")}: <span className={`badge ${deployProgress.status || "queued"}`}>{deployProgress.status || "queued"}</span>
            </div>
            <div className="deploy-items">
              {deployItems.length === 0 && <div className="muted small">{t("No data")}</div>}
              {deployItems.map((item) => {
                const node = nodes.find((n) => n.id === item.node_id);
                return (
                  <div className="deploy-item" key={item.id}>
                  <div className="deploy-item-head">
                    <div className="deploy-item-title">{node?.name || item.node_id}</div>
                    <span className={`badge ${item.status || "queued"}`}>{item.status || "queued"}</span>
                  </div>
                  {item.stage && <div className="muted small">{t("Stage")}: {item.stage}</div>}
                  {item.error && <div className="error">{item.error}</div>}
                  {deployLogs[item.id] && <pre className="deploy-log">{deployLogs[item.id]}</pre>}
                </div>
              );
            })}
            </div>
          </div>
        </div>
      )}

      {taskProgress.open && (
        <div className="modal overlay-modal">
          <div className="modal-content wide">
            <div className="deploy-header">
              <div>
                <h3>{t("Task progress")}</h3>
                <div className="muted small">{t("Job")}: {taskProgress.jobId}</div>
              </div>
              <button type="button" onClick={closeTaskProgress}>{t("Close")}</button>
            </div>
            {taskError && <div className="error">{taskError}</div>}
            <div className="deploy-status">
              {t("Status")}: <span className={`badge ${taskProgress.status || "queued"}`}>{taskProgress.status || "queued"}</span>
            </div>
            <div className="deploy-items">
              {taskItems.length === 0 && <div className="muted small">{t("No data")}</div>}
              {taskItems.map((item) => {
                const node = nodes.find((n) => n.id === item.node_id);
                return (
                  <div className="deploy-item" key={item.id}>
                  <div className="deploy-item-head">
                    <div className="deploy-item-title">{node?.name || item.node_id}</div>
                    <span className={`badge ${item.status || "queued"}`}>{item.status || "queued"}</span>
                  </div>
                  {item.stage && <div className="muted small">{t("Stage")}: {item.stage}</div>}
                  {item.error && <div className="error">{item.error}</div>}
                  {taskLogs[item.id] && <pre className="deploy-log">{taskLogs[item.id]}</pre>}
                </div>
              );
            })}
            </div>
          </div>
        </div>
      )}

      {passkeysOpen && (
        <div className="modal overlay-modal">
          <div className="modal-content">
            <h3>{t("Passkeys")}</h3>
            <div className="form-grid" autoComplete="off">
              <div className="hint">{t("Use a passkey to log in without password.")}</div>
              {passkeysTotpRequired && (
                <input
                  name="passkey_otp"
                  autoComplete="one-time-code"
                  placeholder={t("2FA Code")}
                  value={passkeysOTP}
                  onChange={(e) => setPasskeysOTP(e.target.value)}
                />
              )}
              <button type="button" className="btn-inline" onClick={registerPasskey} disabled={passkeysBusy}>
                {passkeysBusy ? t("Loading...") : t("Enable Passkey")}
              </button>
            </div>
            {passkeysError && <div className="error">{passkeysError}</div>}
            <div className="table compact users-table">
              <div className="table-row head">
                <div>{t("Created")}</div>
                <div>{t("Last used")}</div>
                <div>{t("Transports")}</div>
                <div>{t("Actions")}</div>
              </div>
              {passkeysList.map((item) => (
                <div className="table-row" key={item.id}>
                  <div>{item.created_at ? formatTS(item.created_at) : "-"}</div>
                  <div>{item.last_used_at ? formatTS(item.last_used_at) : "-"}</div>
                  <div>{(item.transports || []).join(", ") || "-"}</div>
                  <div className="actions">
                    <button className="danger ghost" type="button" onClick={() => deletePasskey(item.id)}>
                      {t("Remove")}
                    </button>
                  </div>
                </div>
              ))}
              {passkeysList.length === 0 && (
                <div className="table-row">
                  <div className="muted small">{t("No passkeys yet")}</div>
                </div>
              )}
            </div>
            <div className="actions">
              <button type="button" onClick={() => setPasskeysOpen(false)}>{t("Close")}</button>
            </div>
          </div>
        </div>
      )}

      {usersOpen && (
        <div className="modal overlay-modal">
          <div className="modal-content">
            <h3>{t("Users & roles")}</h3>
            <div className="form-grid" autoComplete="off">
              <input
                name="user_name"
                autoComplete="off"
                placeholder={t("Username or email")}
                value={usersDraft.name}
                onChange={(e) => setUsersDraft({ ...usersDraft, name: e.target.value })}
              />
              <input
                name="user_password"
                autoComplete="new-password"
                type="password"
                placeholder={t("Password")}
                value={usersDraft.password}
                onChange={(e) => setUsersDraft({ ...usersDraft, password: e.target.value })}
              />
              <select
                name="user_role"
                value={usersDraft.role}
                onChange={(e) => setUsersDraft({ ...usersDraft, role: e.target.value })}
              >
                <option value="admin">{t("Administrator")}</option>
                <option value="operator">{t("Operator (no node delete)")}</option>
                <option value="viewer">{t("Viewer (status only)")}</option>
              </select>
            </div>
            <div className="actions">
              <button
                type="button"
                onClick={createUser}
                disabled={usersBusy}
              >
                {t("Add user")}
              </button>
              <button type="button" onClick={() => setUsersOpen(false)}>{t("Close")}</button>
            </div>
            <div className="table compact users-table">
              <div className="table-row head">
                <div>{t("Username")}</div>
                <div>{t("Role")}</div>
                <div>{t("Actions")}</div>
              </div>
              {usersList.map((user) => (
                <div className="table-row" key={user.id}>
                  <div>{user.username}</div>
                  <div>
                    <select
                      value={user.role}
                      onChange={(e) => setUsersList(usersList.map((u) => u.id === user.id ? { ...u, role: e.target.value } : u))}
                    >
                      <option value="admin">{t("Administrator")}</option>
                      <option value="operator">{t("Operator")}</option>
                      <option value="viewer">{t("Viewer")}</option>
                    </select>
                  </div>
                  <div className="actions">
                    <button type="button" onClick={() => updateUserRole(user)} disabled={usersBusy}>{t("Save")}</button>
                    <button className="danger ghost" type="button" onClick={() => deleteUser(user)} disabled={usersBusy}>
                      {t("Remove")}
                    </button>
                  </div>
                </div>
              ))}
              {usersList.length === 0 && (
                <div className="table-row">
                  <div className="muted small">{t("No users yet")}</div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {auditOpen && (
        <div className="modal overlay-modal">
          <div className="modal-content wide">
            <h3>{t("Audit log")}</h3>
            <div className="audit-controls">
              <input
                placeholder={t("Filter by node_id")}
                autoComplete="off"
                name="audit_node_id"
                value={auditNodeID}
                onChange={(e) => setAuditNodeID(e.target.value)}
              />
              <button type="button" onClick={() => loadAudit({ offset: 0, nodeID: auditNodeID })}>{t("Apply")}</button>
              <button type="button" onClick={() => { setAuditNodeID(""); loadAudit({ offset: 0, nodeID: "" }); }}>{t("Clear")}</button>
            </div>
            <div className="table compact audit-table">
              <div className="table-row head">
                <div>{t("Time")}</div>
                <div>{t("Actor")}</div>
                <div>{t("Action")}</div>
                <div>{t("Status")}</div>
                <div>{t("Node")}</div>
                <div>{t("Message")}</div>
              </div>
              {auditLogs.map((row) => (
                <div className="table-row" key={row.id}>
                <div data-label={t("Time")}>{formatTS(row.ts || row.created_at)}</div>
                <div data-label={t("Actor")}>{row.actor_user || row.actor}</div>
                <div data-label={t("Action")}>{row.action}</div>
                <div data-label={t("Status")}>{row.status}</div>
                <div data-label={t("Node")}>{row.node_id || "-"}</div>
                <div data-label={t("Message")}>{row.message || row.error || "-"}</div>
              </div>
            ))}
            </div>
            <div className="actions">
              <button type="button" onClick={() => loadAudit({ offset: Math.max(0, auditOffset - 100) })} disabled={auditOffset === 0}>{t("Prev")}</button>
              <button type="button" onClick={() => loadAudit({ offset: auditOffset + 100 })} disabled={auditLogs.length < 100}>{t("Next")}</button>
              <button type="button" onClick={() => setAuditOpen(false)}>{t("Close")}</button>
            </div>
          </div>
        </div>
      )}

      <NodeSSHModal
        open={sshModal.open}
        node={sshModal.node}
        onClose={() => {
          if (sshModal.confirmClose && !confirm(t("Are you sure?"))) return;
          setSshModal({ open: false, node: null, confirmClose: false });
        }}
      />

      {sshChoice.open && sshChoice.node && (
        <div className="modal overlay-modal">
          <div className="modal-content">
            <h3>{t("Open SSH")}</h3>
            <div className="hint">{t("Open here or in a new tab?")}</div>
            <div className="actions">
              <button
                type="button"
                onClick={() => {
                  setSshModal({ open: true, node: sshChoice.node, confirmClose: true });
                  setSshChoice({ open: false, node: null });
                }}
              >
                {t("Open here")}
              </button>
              <button
                type="button"
                onClick={() => {
                  window.open(`/nodes?ssh=${sshChoice.node.id}`, "_blank");
                  setSshChoice({ open: false, node: null });
                }}
              >
                {t("Open in new tab")}
              </button>
              <button type="button" onClick={() => setSshChoice({ open: false, node: null })}>{t("Cancel")}</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function DashboardPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [nodes, setNodes] = useState([]);
  const [activeUsers, setActiveUsers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [searchNodes, setSearchNodes] = useState("");
  const [searchUsers, setSearchUsers] = useState("");
  const [sandboxOnly, setSandboxOnly] = useState(false);
  const [wsStatus, setWsStatus] = useState("disconnected");
  const [now, setNow] = useState(Date.now());
  const wsRef = useRef(null);

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(timer);
  }, []);

  async function loadSummary() {
    setLoading(true);
    setError("");
    try {
      const data = await request("GET", "/dashboard/summary");
      setNodes(data.nodes || []);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }

  async function loadUsers() {
    try {
      const params = new URLSearchParams();
      params.set("limit", "200");
      if (searchUsers.trim()) {
        params.set("search", searchUsers.trim());
      }
      const data = await request("GET", `/dashboard/active-users?${params.toString()}`);
      setActiveUsers(data || []);
    } catch (err) {
      setError(err.message);
    }
  }

  useEffect(() => {
    loadSummary();
    loadUsers();
  }, []);

  useEffect(() => {
    const timer = setTimeout(() => {
      loadUsers();
    }, 300);
    return () => clearTimeout(timer);
  }, [searchUsers]);

  useEffect(() => {
    let stopped = false;
    let retries = 0;
    const connect = () => {
      const token = getToken();
      if (!token) return;
      setWsStatus("connecting");
      const protocol = window.location.protocol === "https:" ? "wss" : "ws";
      const wsUrl = `${protocol}://${window.location.host}${API_BASE}/dashboard/stream?token=${encodeURIComponent(token)}`;
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;
      ws.onopen = () => {
        retries = 0;
        setWsStatus("connected");
      };
      ws.onmessage = (event) => {
        if (!event.data) return;
        let payload = null;
        try {
          payload = JSON.parse(event.data);
        } catch {
          return;
        }
        if (!payload?.type) return;
        if (payload.type === "snapshot" && payload.data) {
          setNodes(payload.data.nodes || []);
          setActiveUsers(payload.data.active_users || []);
          return;
        }
        if (payload.type === "node_metrics_update" && payload.data) {
          const { node_id, metrics } = payload.data;
          if (!node_id || !metrics) return;
          setNodes((prev) => {
            const idx = prev.findIndex((n) => n.node_id === node_id);
            if (idx === -1) return prev;
            const next = [...prev];
            next[idx] = { ...next[idx], ...metrics, collected_at: metrics.collected_at || next[idx].collected_at };
            return next;
          });
        }
        if (payload.type === "active_users_update" && payload.data) {
          const { node_id, users, node_name, source } = payload.data;
          if (!node_id) return;
          setNodes((prev) => {
            const idx = prev.findIndex((n) => n.node_id === node_id);
            if (idx === -1) return prev;
            const next = [...prev];
            next[idx] = {
              ...next[idx],
              active_users_source: payload.data.source,
              active_users_source_detail: payload.data.source_detail,
              active_users_available: payload.data.available,
            };
            return next;
          });
          setActiveUsers((prev) => {
            const filtered = prev.filter((u) => u.node_id !== node_id);
            const mapped = Array.isArray(users)
              ? users.map((u) => ({
                  ...u,
                  node_id,
                  node_name: node_name || u.node_name,
                }))
              : [];
            return [...filtered, ...mapped].sort((a, b) => new Date(b.last_seen) - new Date(a.last_seen));
          });
        }
      };
      ws.onclose = () => {
        setWsStatus("disconnected");
        if (stopped) return;
        retries += 1;
        const delay = Math.min(10000, 1000 * retries);
        setTimeout(connect, delay);
      };
      ws.onerror = () => {
        ws.close();
      };
    };
    connect();
    return () => {
      stopped = true;
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, []);

  useEffect(() => {
    if (wsStatus === "connected") return;
    const timer = setInterval(() => {
      loadSummary();
      loadUsers();
    }, 15000);
    return () => clearInterval(timer);
  }, [wsStatus, searchUsers]);

  const staleMs = 20000;
  const nowTs = now;
  const nodesFiltered = useMemo(() => {
    const term = searchNodes.trim().toLowerCase();
    return nodes.filter((node) => {
      if (sandboxOnly && !node.is_sandbox) return false;
      if (!term) return true;
      return (node.name || "").toLowerCase().includes(term);
    });
  }, [nodes, searchNodes, sandboxOnly]);

  const usersFiltered = useMemo(() => {
    const term = searchUsers.trim().toLowerCase();
    if (!term) return activeUsers;
    return activeUsers.filter((u) => (u.client_email || "").toLowerCase().includes(term));
  }, [activeUsers, searchUsers]);

  const aggregate = useMemo(() => {
    let online = 0;
    let agentsOnline = 0;
    let agentsTotal = 0;
    let cpuSum = 0;
    let cpuCount = 0;
    let rx = 0;
    let tx = 0;
    nodesFiltered.forEach((node) => {
      if (node.agent_installed) {
        agentsTotal += 1;
      }
      if (node.agent_online) {
        online += 1;
        agentsOnline += 1;
      }
      if (node.cpu_pct != null) {
        cpuSum += node.cpu_pct;
        cpuCount += 1;
      }
      if (node.net_rx_bps != null) rx += node.net_rx_bps;
      if (node.net_tx_bps != null) tx += node.net_tx_bps;
    });
    return {
      nodesOnline: online,
      agentsOnline,
      agentsTotal,
      nodesTotal: nodesFiltered.length,
      avgCPU: cpuCount > 0 ? cpuSum / cpuCount : 0,
      totalRx: rx,
      totalTx: tx,
      activeUsers: activeUsers.length,
    };
  }, [nodesFiltered, activeUsers, nowTs]);

  const deriveNodeStatus = (node) => {
    if (!node.agent_installed) return "no_agent";
    if (node.agent_online) return "online";
    return "offline";
  };

  const formatSource = (node) => {
    const source = node.active_users_source || "unknown";
    if (source === "no_source") return t("No source");
    return source;
  };

  return (
    <div className="page page-wide dashboard-page">
      <header className="header">
        <div className="header-left">
          <button className="icon-button" onClick={() => navigate("/nodes")}>{"<"}</button>
          <h2>{t("Dashboard")}</h2>
          <span className={`badge ${wsStatus}`}>{t(wsStatus === "connected" ? "Connected" : wsStatus === "connecting" ? "Connecting" : "Disconnected")}</span>
        </div>
        <div className="header-right">
          <button onClick={() => { loadSummary(); loadUsers(); }}>{t("Refresh")}</button>
        </div>
      </header>

      {error && <div className="error">{error}</div>}

      <div className="dashboard-cards">
        <div className="dashboard-card">
          <div className="muted small">{t("Nodes online")}</div>
          <div className="dashboard-value">{aggregate.nodesOnline} / {aggregate.nodesTotal}</div>
        </div>
        <div className="dashboard-card">
          <div className="muted small">{t("Agents online")}</div>
          <div className="dashboard-value">{aggregate.agentsOnline} / {aggregate.agentsTotal}</div>
        </div>
        <div className="dashboard-card">
          <div className="muted small">{t("Total RX")}</div>
          <div className="dashboard-value">{formatBps(aggregate.totalRx)}</div>
        </div>
        <div className="dashboard-card">
          <div className="muted small">{t("Total TX")}</div>
          <div className="dashboard-value">{formatBps(aggregate.totalTx)}</div>
        </div>
        <div className="dashboard-card">
          <div className="muted small">{t("Avg CPU")}</div>
          <div className="dashboard-value">{formatPercent(aggregate.avgCPU)}</div>
        </div>
        <div className="dashboard-card">
          <div className="muted small">{t("Active users")}</div>
          <div className="dashboard-value">{aggregate.activeUsers}</div>
        </div>
      </div>

      <div className="dashboard-section">
        <div className="dashboard-section-header">
          <h3>{t("Nodes")}</h3>
          <div className="dashboard-filters">
            <input value={searchNodes} onChange={(e) => setSearchNodes(e.target.value)} placeholder={t("Search")} />
            <label className="checkbox">
              <input type="checkbox" checked={sandboxOnly} onChange={(e) => setSandboxOnly(e.target.checked)} />
              <span>{t("Sandbox only")}</span>
            </label>
          </div>
        </div>
        <div className="table dashboard-nodes">
          <div className="table-row head">
            <div>{t("Node")}</div>
            <div>{t("Status")}</div>
            <div>{t("Agent")}</div>
            <div>{t("CPU")}</div>
            <div>{t("RAM")}</div>
            <div>{t("Disk")}</div>
            <div>{t("RX")}</div>
            <div>{t("TX")}</div>
            <div>{t("Uptime")}</div>
            <div>{t("Last seen")}</div>
            <div>{t("Panel version")}</div>
          </div>
          {nodesFiltered.map((node) => {
            const status = deriveNodeStatus(node);
            const ram = node.ram_total_bytes ? `${formatBytes(node.ram_used_bytes || 0)} / ${formatBytes(node.ram_total_bytes)}` : "-";
            const disk = node.disk_total_bytes ? `${formatBytes(node.disk_used_bytes || 0)} / ${formatBytes(node.disk_total_bytes)}` : "-";
            const badgeClass = node.active_users_available ? "source-ok" : "source-bad";
            const agentLabel = node.agent_installed
              ? node.agent_online ? t("Agent online") : t("Agent offline")
              : t("No agent");
            const agentClass = node.agent_installed
              ? node.agent_online ? "badge online" : "badge offline"
              : "badge muted";
            return (
              <div className="table-row" key={node.node_id}>
                <div>
                  <div className="node-name">{node.name}</div>
                  <span className={`badge source ${badgeClass}`} title={node.active_users_source_detail || ""}>
                    {formatSource(node)}
                  </span>
                </div>
                <div><DashboardStatusBadge status={status} /></div>
                <div>
                  <span className={agentClass} title={node.agent_version ? `v${node.agent_version}` : ""}>
                    {agentLabel}
                  </span>
                </div>
                <div>{formatPercent(node.cpu_pct)}</div>
                <div>{ram}</div>
                <div>{disk}</div>
                <div>{formatBps(node.net_rx_bps)}</div>
                <div>{formatBps(node.net_tx_bps)}</div>
                <div>{formatDuration(node.uptime_sec)}</div>
                <div>{formatTS(node.collected_at)}</div>
                <div>{node.panel_version || "-"}</div>
              </div>
            );
          })}
          {nodesFiltered.length === 0 && (
            <div className="table-row">
              <div>{loading ? t("Loading...") : t("No data")}</div>
            </div>
          )}
        </div>
      </div>

      <div className="dashboard-section">
        <div className="dashboard-section-header">
          <h3>{t("Active users")}</h3>
          <div className="dashboard-filters">
            <input value={searchUsers} onChange={(e) => setSearchUsers(e.target.value)} placeholder={t("Search")} />
          </div>
        </div>
        <div className="table dashboard-users">
          <div className="table-row head">
            <div>{t("Client")}</div>
            <div>{t("Node")}</div>
            <div>{t("Inbound")}</div>
            <div>{t("IP")}</div>
            <div>{t("RX")}</div>
            <div>{t("TX")}</div>
            <div>{t("Total")}</div>
            <div>{t("Last seen")}</div>
          </div>
          {usersFiltered.map((user) => (
            <div className="table-row" key={user.id || `${user.node_id}-${user.client_email}-${user.ip || ""}`}>
              <div>{user.client_email}</div>
              <div>{user.node_name || "-"}</div>
              <div>{user.inbound_tag || "-"}</div>
              <div>{user.ip || "-"}</div>
              <div>{formatBps(user.rx_bps)}</div>
              <div>{formatBps(user.tx_bps)}</div>
              <div>{user.total_up_bytes || user.total_down_bytes ? `${formatBytes(user.total_up_bytes || 0)} / ${formatBytes(user.total_down_bytes || 0)}` : "-"}</div>
              <div>{formatTS(user.last_seen)}</div>
            </div>
          ))}
          {usersFiltered.length === 0 && (
            <div className="table-row">
              <div>{loading ? t("Loading...") : t("No data")}</div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function FilesPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [nodes, setNodes] = useState([]);
  const [nodeId, setNodeId] = useState("");
  const [roots, setRoots] = useState([]);
  const [currentPath, setCurrentPath] = useState("");
  const [entries, setEntries] = useState([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState({ key: "name", dir: "asc" });
  const [tree, setTree] = useState({});
  const [preview, setPreview] = useState({ open: false, entry: null, content: "", imageUrl: "", note: "" });
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef(null);

  useEffect(() => {
    let active = true;
    async function load() {
      try {
        const data = await request("GET", "/nodes");
        if (!active) return;
        setNodes(data);
        if (data.length > 0) {
          setNodeId(data[0].id);
        }
      } catch (err) {
        setError(err.message);
      }
    }
    load();
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!nodeId) return;
    let active = true;
    async function loadRoots() {
      setError("");
      setRoots([]);
      setEntries([]);
      setTree({});
      try {
        const data = await request("GET", `/nodes/${nodeId}/files/roots`);
        if (!active) return;
        const list = data?.roots || [];
        setRoots(list);
        if (list.length > 0) {
          setCurrentPath(list[0].path);
        }
      } catch (err) {
        setError(err.message);
      }
    }
    loadRoots();
    return () => {
      active = false;
    };
  }, [nodeId]);

  useEffect(() => {
    if (!nodeId || !currentPath) return;
    loadList(currentPath);
  }, [nodeId, currentPath]);

  async function loadList(pathValue) {
    setBusy(true);
    setError("");
    try {
      const data = await request("GET", `/nodes/${nodeId}/files/list?path=${encodeURIComponent(pathValue)}`);
      setEntries(data);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function loadTreeChildren(pathValue) {
    setTree((prev) => ({ ...prev, [pathValue]: { ...(prev[pathValue] || {}), loading: true } }));
    try {
      const data = await request("GET", `/nodes/${nodeId}/files/list?path=${encodeURIComponent(pathValue)}`);
      const children = (data || []).filter((item) => item.is_dir);
      setTree((prev) => ({
        ...prev,
        [pathValue]: { children, expanded: true, loading: false },
      }));
    } catch {
      setTree((prev) => ({ ...prev, [pathValue]: { ...(prev[pathValue] || {}), loading: false } }));
    }
  }

  function toggleTree(pathValue) {
    const node = tree[pathValue];
    if (node?.expanded) {
      setTree((prev) => ({ ...prev, [pathValue]: { ...(prev[pathValue] || {}), expanded: false } }));
      return;
    }
    if (node?.children) {
      setTree((prev) => ({ ...prev, [pathValue]: { ...(prev[pathValue] || {}), expanded: true } }));
      return;
    }
    loadTreeChildren(pathValue);
  }

  function joinPath(base, next) {
    if (base.endsWith("/")) return `${base}${next}`;
    return `${base}/${next}`;
  }

  function sortedEntries() {
    const filtered = entries.filter((item) => item.name.toLowerCase().includes(search.toLowerCase()));
    const dir = sort.dir === "asc" ? 1 : -1;
    const key = sort.key;
    return filtered.sort((a, b) => {
      const aVal = key === "modified" ? new Date(a.modified || 0).getTime() : a[key];
      const bVal = key === "modified" ? new Date(b.modified || 0).getTime() : b[key];
      if (aVal == null && bVal == null) return 0;
      if (aVal == null) return -1 * dir;
      if (bVal == null) return 1 * dir;
      if (typeof aVal === "string") {
        return aVal.localeCompare(bVal) * dir;
      }
      if (aVal > bVal) return 1 * dir;
      if (aVal < bVal) return -1 * dir;
      return 0;
    });
  }

  function setSortKey(key) {
    setSort((prev) => {
      if (prev.key === key) {
        return { key, dir: prev.dir === "asc" ? "desc" : "asc" };
      }
      return { key, dir: "asc" };
    });
  }

  function breadcrumbs() {
    if (!currentPath) return [];
    const parts = currentPath.split("/").filter(Boolean);
    const crumbs = [];
    let acc = "";
    for (const part of parts) {
      acc += `/${part}`;
      crumbs.push({ label: part, path: acc });
    }
    return crumbs;
  }

  async function downloadEntry(entry) {
    const token = getToken();
    const res = await fetch(`${API_BASE}/nodes/${nodeId}/files/download?path=${encodeURIComponent(entry.path)}`, {
      method: "GET",
      headers: {
        Authorization: token ? `Bearer ${token}` : "",
        "X-Requested-With": "XMLHttpRequest",
      },
      credentials: "include",
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || `Download failed: ${res.status}`);
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = entry.name;
    link.click();
    URL.revokeObjectURL(url);
  }

  async function deleteEntry(entry) {
    if (!confirm(t("Delete file {name}?", { name: entry.name }))) return;
    await request("POST", `/nodes/${nodeId}/files/delete`, { path: entry.path });
    await loadList(currentPath);
  }

  async function renameEntry(entry) {
    const next = prompt(t("New name"), entry.name);
    if (!next || next === entry.name) return;
    const parent = entry.path.split("/").slice(0, -1).join("/") || "/";
    const newPath = joinPath(parent, next);
    await request("POST", `/nodes/${nodeId}/files/rename`, { old_path: entry.path, new_path: newPath });
    await loadList(currentPath);
  }

  async function createFolder() {
    const name = prompt(t("Folder name"));
    if (!name) return;
    await request("POST", `/nodes/${nodeId}/files/mkdir`, { path: joinPath(currentPath, name) });
    await loadList(currentPath);
  }

  async function uploadFile(file) {
    if (!file) return;
    setUploading(true);
    setError("");
    try {
      const token = getToken();
      const form = new FormData();
      form.append("file", file);
      const res = await fetch(`${API_BASE}/nodes/${nodeId}/files/upload?path=${encodeURIComponent(currentPath)}`, {
        method: "POST",
        headers: {
          Authorization: token ? `Bearer ${token}` : "",
          "X-Requested-With": "XMLHttpRequest",
        },
        credentials: "include",
        body: form,
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || `Upload failed: ${res.status}`);
      }
      await loadList(currentPath);
    } catch (err) {
      setError(err.message);
    } finally {
      setUploading(false);
      if (fileInputRef.current) {
        fileInputRef.current.value = "";
      }
    }
  }

  async function openPreview(entry) {
    const name = entry.name.toLowerCase();
    const ext = name.includes(".") ? name.split(".").pop() : "";
    const textExts = ["log", "txt", "json", "yaml", "yml"];
    const imgExts = ["png", "jpg", "jpeg", "webp", "gif"];
    setPreview({ open: true, entry, content: "", imageUrl: "", note: "" });
    if (imgExts.includes(ext)) {
      try {
        const token = getToken();
        const res = await fetch(`${API_BASE}/nodes/${nodeId}/files/download?path=${encodeURIComponent(entry.path)}`, {
          method: "GET",
          headers: {
            Authorization: token ? `Bearer ${token}` : "",
            "X-Requested-With": "XMLHttpRequest",
          },
          credentials: "include",
        });
        if (!res.ok) {
          throw new Error(`Preview failed: ${res.status}`);
        }
        const blob = await res.blob();
        const url = URL.createObjectURL(blob);
        setPreview({ open: true, entry, content: "", imageUrl: url, note: "" });
      } catch (err) {
        setPreview({ open: true, entry, content: "", imageUrl: "", note: err.message });
      }
      return;
    }
    if (textExts.includes(ext)) {
      try {
        const data = await request("GET", `/nodes/${nodeId}/files/read?path=${encodeURIComponent(entry.path)}`);
        setPreview({ open: true, entry, content: data.data || "", imageUrl: "", note: "" });
      } catch (err) {
        const code = err?.data?.error?.code;
        if (code === "FILE_TOO_LARGE") {
          try {
            const data = await request("GET", `/nodes/${nodeId}/files/tail?path=${encodeURIComponent(entry.path)}`);
            setPreview({ open: true, entry, content: data.data || "", imageUrl: "", note: t("Showing tail") });
          } catch (tailErr) {
            setPreview({ open: true, entry, content: "", imageUrl: "", note: tailErr.message });
          }
          return;
        }
        setPreview({ open: true, entry, content: "", imageUrl: "", note: err.message });
      }
      return;
    }
    setPreview({ open: true, entry, content: "", imageUrl: "", note: t("No preview available") });
  }

  function renderTreeNode(rootPath, label) {
    const node = tree[rootPath] || {};
    const isExpanded = node.expanded;
    const children = node.children || [];
    return (
      <div className="tree-node" key={rootPath}>
        <button type="button" className={`tree-item ${currentPath === rootPath ? "active" : ""}`} onClick={() => setCurrentPath(rootPath)}>
          <span>{label}</span>
        </button>
        <button type="button" className="tree-toggle" onClick={() => toggleTree(rootPath)}>{isExpanded ? "-" : "+"}</button>
        {node.loading && <div className="muted small">Loading...</div>}
        {isExpanded && children.length > 0 && (
          <div className="tree-children">
            {children.map((child) => renderTreeNode(child.path, child.name))}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="page page-wide">
      <header className="header">
        <div className="header-left">
          <button className="icon-button" onClick={() => navigate("/nodes")}>{"<"}</button>
          <h2>{t("Files")}</h2>
        </div>
        <div className="header-right">
          <button className="secondary" onClick={() => loadList(currentPath)}>{t("Refresh")}</button>
        </div>
      </header>

      <div className="files-toolbar">
        <div className="files-select">
          <label>
            {t("Node")}
            <select value={nodeId} onChange={(e) => setNodeId(e.target.value)}>
              {nodes.map((node) => (
                <option key={node.id} value={node.id}>{node.name}</option>
              ))}
            </select>
          </label>
          <label>
            {t("Root")}
            <select value={currentPath} onChange={(e) => setCurrentPath(e.target.value)}>
              {roots.map((root) => (
                <option key={root.path} value={root.path}>{root.label}</option>
              ))}
            </select>
          </label>
        </div>
        <div className="files-actions">
          <input
            className="files-search"
            placeholder={t("Search")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <button type="button" className="secondary" onClick={createFolder}>{t("New folder")}</button>
          <button type="button" onClick={() => fileInputRef.current?.click()} disabled={uploading}>{uploading ? t("Loading...") : t("Upload")}</button>
          <input
            ref={fileInputRef}
            type="file"
            className="hidden-file"
            onChange={(e) => uploadFile(e.target.files?.[0])}
          />
        </div>
      </div>

      <div className="files-breadcrumbs">
        <span className="muted small">/</span>
        {breadcrumbs().map((crumb, idx) => (
          <button key={crumb.path} type="button" onClick={() => setCurrentPath(crumb.path)} className="breadcrumb">
            {crumb.label}{idx < breadcrumbs().length - 1 ? " /" : ""}
          </button>
        ))}
      </div>

      {error && <div className="error">{error}</div>}

      <div className="files-layout">
        <aside className="files-sidebar">
          {roots.map((root) => renderTreeNode(root.path, root.label))}
        </aside>
        <section className="files-list">
          <div className="table files">
            <div className="table-row head">
              <div onClick={() => setSortKey("name")}>{t("Name")}</div>
              <div onClick={() => setSortKey("size")}>{t("Size")}</div>
              <div onClick={() => setSortKey("modified")}>{t("Modified")}</div>
              <div onClick={() => setSortKey("type")}>{t("Type")}</div>
              <div>{t("Actions")}</div>
            </div>
            {sortedEntries().map((entry) => (
              <div className="table-row" key={entry.path}>
                <div className={`file-name ${entry.is_dir ? "is-dir" : ""}`} onClick={() => entry.is_dir ? setCurrentPath(entry.path) : openPreview(entry)}>
                  {entry.name}
                </div>
                <div>{entry.is_dir ? "-" : `${entry.size} B`}</div>
                <div>{entry.modified ? formatTS(entry.modified) : "-"}</div>
                <div>{entry.is_dir ? t("Folder") : entry.mime_guess || entry.type}</div>
                <div className="actions">
                  {!entry.is_dir && (
                    <button type="button" onClick={() => downloadEntry(entry)}>{t("Download")}</button>
                  )}
                  <button type="button" className="secondary" onClick={() => renameEntry(entry)}>{t("Rename")}</button>
                  <button type="button" className="danger" onClick={() => deleteEntry(entry)}>{t("Delete")}</button>
                </div>
              </div>
            ))}
            {!busy && entries.length === 0 && (
              <div className="table-row">
                <div className="muted small">{t("No files yet")}</div>
              </div>
            )}
            {busy && (
              <div className="table-row">
                <div className="muted small">{t("Loading...")}</div>
              </div>
            )}
          </div>
        </section>
      </div>

      {preview.open && (
        <div className="modal">
          <div className="modal-content wide">
            <div className="modal-header">
              <h3>{preview.entry?.name || t("Preview")}</h3>
              <button type="button" className="secondary" onClick={() => {
                if (preview.imageUrl) {
                  URL.revokeObjectURL(preview.imageUrl);
                }
                setPreview({ open: false, entry: null, content: "", imageUrl: "", note: "" });
              }}>
                {t("Close")}
              </button>
            </div>
            {preview.note && <div className="muted small">{preview.note}</div>}
            {preview.imageUrl && <img className="file-preview-image" src={preview.imageUrl} alt="preview" />}
            {preview.content && (
              <textarea readOnly rows={20} value={preview.content} />
            )}
            {!preview.imageUrl && !preview.content && (
              <div className="muted small">{t("No preview available")}</div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function InboundsPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const { t } = useI18n();
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
    if (!confirm(t("Delete inbound?"))) return;
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
        <h2>{t("Inbounds")}</h2>
        <div className="actions">
          <button onClick={() => navigate("/nodes")}>{t("Back")}</button>
          <button onClick={openAdd}>{t("Add")}</button>
        </div>
      </header>

      {error && <div className="error">{error}</div>}

      <div className="inbounds-table-desktop">
        <div className="table inbounds">
          <div className="table-row head">
            <div>ID</div>
            <div>{t("Remark")}</div>
            <div>{t("Protocol")}</div>
            <div>{t("Port")}</div>
            <div>{t("Actions")}</div>
          </div>
          {inbounds.map((inbound) => (
            <div className="table-row" key={inbound.id}>
              <div>{inbound.id}</div>
              <div>{inbound.remark}</div>
              <div>{inbound.protocol}</div>
              <div>{inbound.port}</div>
              <div className="actions">
                <button onClick={() => openEdit(inbound)}>{t("Edit")}</button>
                <button className="danger" onClick={() => onDelete(inbound.id)}>{t("Delete")}</button>
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
              <span className="field-label">{t("Remark")}</span>
              <span>{inbound.remark || "-"}</span>
            </div>
            <div className="inbound-card-row">
              <span className="field-label">{t("Protocol")}</span>
              <span>{inbound.protocol || "-"}</span>
            </div>
            <div className="inbound-card-row">
              <span className="field-label">{t("Port")}</span>
              <span>{inbound.port || "-"}</span>
            </div>
            <div className="actions">
              <button onClick={() => openEdit(inbound)}>{t("Edit")}</button>
              <button className="danger" onClick={() => onDelete(inbound.id)}>{t("Delete")}</button>
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
          path="/dashboard"
          element={
            <RequireAuth>
              <DashboardPage />
            </RequireAuth>
          }
        />
        <Route
          path="/files"
          element={
            <RequireAuth>
              <FilesPage />
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

