import React, { useEffect, useMemo, useRef, useState } from "react";
import { Routes, Route, useNavigate, useLocation, useParams, Link } from "react-router-dom";
import { request, getToken, refreshAuth, convertSSHKey, getTelegramSettings, saveTelegramSettings, setAuth, clearAuth, getRole, getUser, getOrgId, setOrgId, API_BASE } from "./api.js";
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

function flagEmoji(code) {
  const clean = (code || "").trim().toUpperCase();
  if (!/^[A-Z]{2}$/.test(clean)) return "";
  const base = 127397;
  return String.fromCodePoint(...clean.split("").map((c) => c.charCodeAt(0) + base));
}

function formatLocation(node) {
  const region = (node?.region || "").trim();
  const provider = (node?.provider || "").trim();
  const parts = [];
  if (region) parts.push(region);
  if (provider) parts.push(provider);
  const code = region.length === 2 ? region : "";
  const flag = code ? flagEmoji(code) : "";
  return { text: parts.join(" / ") || "-", flag };
}

function MiniStatCard({ label, value, subvalue, progress, accent, onClick }) {
  const pct = Number.isFinite(progress) ? Math.max(0, Math.min(1, progress)) : 0;
  return (
    <div
      className={`mini-card ${accent || ""} ${onClick ? "clickable" : ""}`}
      style={{ "--progress": pct }}
      onClick={onClick}
      role={onClick ? "button" : undefined}
      tabIndex={onClick ? 0 : undefined}
      onKeyDown={onClick ? (e) => { if (e.key === "Enter" || e.key === " ") onClick(); } : undefined}
    >
      <div className="mini-ring" />
      <div className="mini-body">
        <div className="mini-value">{value}</div>
        {subvalue && <div className="mini-sub">{subvalue}</div>}
        <div className="mini-label">{label}</div>
      </div>
    </div>
  );
}

function StatusDot({ ok }) {
  return <span className={`status-dot ${ok ? "ok" : "bad"}`} />;
}

function UptimeBar({ percent }) {
  const pct = Number.isFinite(percent) ? Math.max(0, Math.min(100, percent)) : 0;
  return (
    <div className="uptime-bar">
      <div className="uptime-bar-fill" style={{ width: `${pct}%` }} />
    </div>
  );
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

function formatProblemMessage(problem, t) {
  if (!problem) return "-";
  const direct = problem.message || problem.error || problem.details;
  if (direct) return direct;
  const alertType = String(problem.alert_type || problem.type || "").toLowerCase();
  if (alertType === "tls") {
    const code = String(problem.check_type || "").trim();
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
      default:
        return t("TLS check failed");
    }
  }
  const fingerprint = problem.fingerprint || "";
  if (!fingerprint) return "-";
  const parts = fingerprint.split("|");
  if (parts[0] === "connection") {
    if (parts.includes("tls")) return t("TLS check failed");
    if (parts.includes("panel") && parts.includes("http")) return t("Panel HTTP is unavailable");
    if (parts.includes("panel") && parts.includes("ssh")) return t("Panel SSH is unavailable");
    if (parts.includes("ssh")) return t("SSH is unavailable");
    if (parts.includes("panel")) return t("Panel is unavailable");
    return t("Connection check failed");
  }
  return fingerprint;
}

function SidebarNav({ active }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const infraKeys = ["nodes", "panels", "hosts", "bots", "add"];
  const toolsKeys = ["files", "dbwork"];
  const securityKeys = ["alerts", "twofa", "passkeys", "settings", "audit"];
  const [infraOpen, setInfraOpen] = useState(false);
  const [toolsOpen, setToolsOpen] = useState(false);
  const [securityOpen, setSecurityOpen] = useState(false);

  useEffect(() => {
    if (infraKeys.includes(active)) setInfraOpen(true);
    if (toolsKeys.includes(active)) setToolsOpen(true);
    if (securityKeys.includes(active)) setSecurityOpen(true);
  }, [active]);

  const infraItems = [
    { key: "nodes", label: t("Nodes"), path: "/nodes" },
    { key: "panels", label: t("3x-ui Panels"), path: "/panels" },
    { key: "hosts", label: t("Hosts"), path: "/nodes?view=host" },
    { key: "bots", label: t("Bots"), path: "/nodes?view=bots" },
    { key: "add", label: t("Add"), path: "/nodes?add=1", addBadge: true },
  ];
  const toolsItems = [
    { key: "files", label: t("File Manager"), path: "/files" },
    { key: "dbwork", label: t("DB work"), path: "/db" },
  ];
  const securityItems = [
    { key: "alerts", label: t("Telegram alerts"), path: "/nodes?view=alerts" },
    { key: "twofa", label: t("2FA settings"), path: "/nodes?view=2fa" },
    { key: "passkeys", label: t("Passkeys"), path: "/nodes?view=passkeys" },
    { key: "settings", label: t("Users & roles"), path: "/nodes?view=settings" },
    { key: "audit", label: t("Audit Log"), path: "/nodes?view=audit" },
  ];
  return (
    <aside className="sidebar">
      <button type="button" className="sidebar-brand-button" onClick={() => navigate("/dashboard")}>
        <div className="sidebar-brand">
          <div className="brand-dot" />
          <div>
            <div className="brand-title">VLF Aggregator</div>
            <div className="brand-sub">{t("Fleet control")}</div>
          </div>
        </div>
      </button>
      <div className="sidebar-nav">
        <button
          type="button"
          className={`sidebar-item ${active === "dashboard" ? "active" : ""}`}
          onClick={() => navigate("/dashboard")}
        >
          <span>{t("Dashboard")}</span>
        </button>

        <button
          type="button"
          className="sidebar-section-toggle"
          onClick={() => setInfraOpen((prev) => !prev)}
        >
          <span>{t("Infrastructure")}</span>
        </button>
        {infraOpen &&
          infraItems.map((item) => (
            <button
              key={item.key}
              type="button"
              className={`sidebar-item ${active === item.key ? "active" : ""}`}
              onClick={() => navigate(item.path)}
            >
              <span>{item.label}</span>
              {item.addBadge && <span className="sidebar-add-icon">+</span>}
            </button>
          ))}

        <button
          type="button"
          className="sidebar-section-toggle"
          onClick={() => setToolsOpen((prev) => !prev)}
        >
          <span>{t("Tools")}</span>
        </button>
        {toolsOpen &&
          toolsItems.map((item) => (
            <button
              key={item.key}
              type="button"
              className={`sidebar-item ${active === item.key ? "active" : ""}`}
              onClick={() => navigate(item.path)}
            >
              <span>{item.label}</span>
            </button>
          ))}

        <button
          type="button"
          className="sidebar-section-toggle"
          onClick={() => setSecurityOpen((prev) => !prev)}
        >
          <span>{t("Access & Security")}</span>
        </button>
        {securityOpen &&
          securityItems.map((item) => (
            <button
              key={item.key}
              type="button"
              className={`sidebar-item ${active === item.key ? "active" : ""}`}
              onClick={() => navigate(item.path)}
            >
              <span>{item.label}</span>
            </button>
          ))}
      </div>
    </aside>
  );
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
  const [orgReady, setOrgReady] = useState(false);
  useEffect(() => {
    let active = true;
    async function ensureAuth() {
      if (getToken()) {
        try {
          await ensureOrg();
        } finally {
          if (active) {
            setOrgReady(true);
            setChecking(false);
          }
        }
        return;
      }
      try {
        const data = await refreshAuth();
        if (data?.token) {
          setAuth(data.token, data.role, data.username);
        }
        await ensureOrg();
      } catch {
        navigate("/login", { replace: true, state: { from: location.pathname } });
      } finally {
        if (active) {
          setOrgReady(true);
          setChecking(false);
        }
      }
    }
    async function ensureOrg() {
      try {
        const orgs = await request("GET", "/orgs");
        const stored = getOrgId();
        const found = Array.isArray(orgs) ? orgs.find((org) => org.id === stored) : null;
        if (found) {
          setOrgId(found.id);
          return;
        }
        if (Array.isArray(orgs) && orgs.length > 0) {
          setOrgId(orgs[0].id);
          return;
        }
        const created = await request("POST", "/orgs", { name: "Personal" });
        if (created?.id) {
          setOrgId(created.id);
        }
      } catch {
        // keep silent, org flows may be unavailable for admin-only sessions
      }
    }
    ensureAuth();
    return () => {
      active = false;
    };
  }, [navigate, location]);
  if (checking || !orgReady) {
    return <div className="page center"><div className="muted">Loading...</div></div>;
  }
  return children;
}

async function copyText(value) {
  if (!value) return;
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    const temp = document.createElement("textarea");
    temp.value = value;
    document.body.appendChild(temp);
    temp.select();
    document.execCommand("copy");
    document.body.removeChild(temp);
  }
}

function maskSecret(value) {
  if (!value) return "";
  if (value.length <= 8) return "••••••";
  return `${value.slice(0, 4)}••••••${value.slice(-4)}`;
}

function LoginPage() {
  const { t } = useI18n();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [otp, setOtp] = useState("");
  const [recoveryCode, setRecoveryCode] = useState("");
  const [recoveryStatus, setRecoveryStatus] = useState("");
  const [error, setError] = useState("");
  const [signupOpen, setSignupOpen] = useState(false);
  const [signupError, setSignupError] = useState("");
  const [signupForm, setSignupForm] = useState({ invite_code: "", username: "", password: "" });
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
      navigate("/dashboard");
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
      navigate("/dashboard");
    } catch (err) {
      setError(err.message);
    } finally {
      setPasskeyBusy(false);
    }
  }

  function mapSignupError(err) {
    const code = err?.data?.error?.code;
    if (code === "INVITE_INVALID" || code === "INVITE_EXPIRED" || code === "INVITE_USED") {
      return t("Invite invalid or expired");
    }
    if (code === "USER_EXISTS") {
      return t("User already exists");
    }
    if (code === "RATE_LIMIT") {
      return t("Too many attempts, try later");
    }
    return err?.message || t("Request failed");
  }

  async function onSignupSubmit(e) {
    e.preventDefault();
    setSignupError("");
    try {
      const data = await request("POST", "/signup", signupForm);
      setAuth(data.token, data.role, data.username);
      setSignupOpen(false);
      navigate("/panels");
    } catch (err) {
      setSignupError(mapSignupError(err));
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
        <button type="button" className="ghost" onClick={() => setSignupOpen(true)}>
          {t("Create account by invite")}
        </button>
        {!webAuthnSupported && <div className="hint">{t("Passkeys are not supported in this browser.")}</div>}
      </form>
      {signupOpen && (
        <div className="modal-backdrop" onClick={() => setSignupOpen(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>{t("Invite-only signup")}</h3>
              <button className="icon-button" onClick={() => setSignupOpen(false)}>×</button>
            </div>
            <form className="modal-body" onSubmit={onSignupSubmit}>
              <label>
                {t("Invite code")}
                <input value={signupForm.invite_code} onChange={(e) => setSignupForm((prev) => ({ ...prev, invite_code: e.target.value }))} />
              </label>
              <label>
                {t("Username")}
                <input value={signupForm.username} onChange={(e) => setSignupForm((prev) => ({ ...prev, username: e.target.value }))} />
              </label>
              <label>
                {t("Password")}
                <input type="password" value={signupForm.password} onChange={(e) => setSignupForm((prev) => ({ ...prev, password: e.target.value }))} />
              </label>
              {signupError && <div className="error">{signupError}</div>}
              <div className="modal-actions">
                <button type="submit" className="primary">{t("Create account")}</button>
                <button type="button" className="secondary" onClick={() => setSignupOpen(false)}>{t("Close")}</button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

function PanelsSelfServicePage() {
  const { t, lang, setLang } = useI18n();
  const navigate = useNavigate();
  const user = getUser();
  const [orgId, setOrgIdState] = useState(getOrgId());
  const [nodes, setNodes] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [wizardOpen, setWizardOpen] = useState(false);
  const [wizardStep, setWizardStep] = useState(1);
  const [installCommand, setInstallCommand] = useState("");
  const [createdNodeId, setCreatedNodeId] = useState("");
  const [pollState, setPollState] = useState({ status: "idle", online: false });
  const pollRef = useRef(null);
  const [form, setForm] = useState({
    name: "",
    host: "",
    type: "PANEL",
    base_url: "",
    panel_username: "",
    panel_password: "",
  });

  const now = Date.now();
  const isOnline = (node) => {
    if (!node?.agent_last_seen_at) return false;
    const ts = new Date(node.agent_last_seen_at).getTime();
    if (!Number.isFinite(ts)) return false;
    return now - ts < 2 * 60 * 1000;
  };

  async function loadNodes() {
    if (!orgId) return;
    setLoading(true);
    setError("");
    try {
      const data = await request("GET", `/orgs/${orgId}/nodes`);
      setNodes(Array.isArray(data) ? data : []);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    const stored = getOrgId();
    if (stored && stored !== orgId) {
      setOrgIdState(stored);
    }
  }, [orgId]);

  useEffect(() => {
    loadNodes();
  }, [orgId]);

  useEffect(() => () => {
    if (pollRef.current) clearInterval(pollRef.current);
  }, []);

  function resetWizard() {
    setWizardStep(1);
    setInstallCommand("");
    setCreatedNodeId("");
    setPollState({ status: "idle", online: false });
    setForm({
      name: "",
      host: "",
      type: "PANEL",
      base_url: "",
      panel_username: "",
      panel_password: "",
    });
  }

  async function createNode() {
    if (!orgId) return;
    setError("");
    const payload = {
      name: form.name,
      kind: form.type,
      host: form.host,
      ssh_host: form.host,
      ssh_port: 22,
      ssh_user: "root",
      ssh_key: "",
      ssh_enabled: false,
    };
    if (form.type === "PANEL") {
      payload.base_url = form.base_url;
      payload.panel_username = form.panel_username;
      payload.panel_password = form.panel_password;
    }
    try {
      const created = await request("POST", `/orgs/${orgId}/nodes`, payload);
      setInstallCommand(created.install_command || "");
      setCreatedNodeId(created.node?.id || "");
      setWizardStep(2);
    } catch (err) {
      setError(err.message);
    }
  }

  function startPolling() {
    if (!orgId || !createdNodeId) return;
    setPollState({ status: "waiting", online: false });
    const start = Date.now();
    if (pollRef.current) clearInterval(pollRef.current);
    pollRef.current = setInterval(async () => {
      try {
        const data = await request("GET", `/orgs/${orgId}/nodes`);
        const node = Array.isArray(data) ? data.find((n) => n.id === createdNodeId) : null;
        if (node && node.agent_last_seen_at) {
          const ts = new Date(node.agent_last_seen_at).getTime();
          if (Number.isFinite(ts) && Date.now() - ts < 2 * 60 * 1000) {
            clearInterval(pollRef.current);
            pollRef.current = null;
            setPollState({ status: "online", online: true });
            setNodes(Array.isArray(data) ? data : []);
          }
        }
      } catch {
        // ignore while polling
      }
      if (Date.now() - start > 60000) {
        clearInterval(pollRef.current);
        pollRef.current = null;
        setPollState({ status: "timeout", online: false });
      }
    }, 2000);
  }

  async function revokeAgent(nodeId) {
    if (!orgId) return;
    setError("");
    try {
      await request("POST", `/orgs/${orgId}/nodes/${nodeId}/agent/revoke`, {});
      loadNodes();
    } catch (err) {
      setError(err.message);
    }
  }

  async function deleteNode(nodeId) {
    if (!orgId) return;
    if (!window.confirm(t("Delete node?"))) return;
    setError("");
    try {
      await request("DELETE", `/orgs/${orgId}/nodes/${nodeId}`);
      loadNodes();
    } catch (err) {
      setError(err.message);
    }
  }

  return (
    <div className="app-shell">
      <SidebarNav active="panels" />
      <div className="app-main">
        <header className="header">
          <div className="header-left" />
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
        <div className="page">
          <div className="page-header">
            <div>
              <h1>{t("3x-ui Panels")}</h1>
              <div className="muted small">{t("Self-service panels")}</div>
            </div>
            <div className="page-actions">
              <button className="primary" onClick={() => { resetWizard(); setWizardOpen(true); }}>{t("Add panel")}</button>
              <button className="secondary" onClick={loadNodes} disabled={loading}>{t("Refresh")}</button>
            </div>
          </div>

          {error && <div className="error">{error}</div>}

          <div className="data-table nodes-table">
            <div className="table-head">
              <div>{t("Name")}</div>
              <div>{t("Host")}</div>
              <div>{t("Created")}</div>
              <div>{t("Agent status")}</div>
              <div>{t("Last seen")}</div>
              <div>{t("Actions")}</div>
            </div>
            {nodes.map((node) => (
              <div key={node.id} className="table-row">
                <div>{node.name}</div>
                <div>{node.host || "-"}</div>
                <div>{formatTS(node.created_at)}</div>
                <div>
                  <StatusBadge status={isOnline(node) ? "online" : "offline"} />
                </div>
                <div>{formatTS(node.agent_last_seen_at)}</div>
                <div className="table-actions">
                  <button className="secondary" onClick={() => revokeAgent(node.id)}>{t("Revoke agent")}</button>
                  <button className="danger" onClick={() => deleteNode(node.id)}>{t("Delete")}</button>
                </div>
              </div>
            ))}
            {nodes.length === 0 && !loading && (
              <div className="table-empty">{t("No data")}</div>
            )}
          </div>

          {wizardOpen && (
            <div className="modal-backdrop" onClick={() => setWizardOpen(false)}>
              <div className="modal wide" onClick={(e) => e.stopPropagation()}>
                <div className="modal-header">
                  <h3>{t("Add panel")}</h3>
                  <button className="icon-button" onClick={() => setWizardOpen(false)}>×</button>
                </div>
                <div className="modal-body">
                  {wizardStep === 1 && (
                    <div className="form-grid">
                      <label>
                        {t("Name")}
                        <input value={form.name} onChange={(e) => setForm((prev) => ({ ...prev, name: e.target.value }))} />
                      </label>
                      <label>
                        {t("Host")}
                        <input value={form.host} onChange={(e) => setForm((prev) => ({ ...prev, host: e.target.value, base_url: prev.base_url || `https://${e.target.value}/x-ui/` }))} />
                      </label>
                      <label>
                        {t("Type")}
                        <select value={form.type} onChange={(e) => setForm((prev) => ({ ...prev, type: e.target.value }))}>
                          <option value="PANEL">{t("Panel")}</option>
                          <option value="HOST">{t("Host")}</option>
                        </select>
                      </label>
                      {form.type === "PANEL" && (
                        <>
                          <label>
                            {t("Base URL")}
                            <input value={form.base_url} onChange={(e) => setForm((prev) => ({ ...prev, base_url: e.target.value }))} />
                          </label>
                          <label>
                            {t("Panel username")}
                            <input value={form.panel_username} onChange={(e) => setForm((prev) => ({ ...prev, panel_username: e.target.value }))} />
                          </label>
                          <label>
                            {t("Panel password")}
                            <input type="password" value={form.panel_password} onChange={(e) => setForm((prev) => ({ ...prev, panel_password: e.target.value }))} />
                          </label>
                        </>
                      )}
                    </div>
                  )}
                  {wizardStep === 2 && (
                    <div className="stack">
                      <div className="muted">{t("Install command")}</div>
                      <pre className="code-block">{installCommand}</pre>
                      <button className="secondary" onClick={() => copyText(installCommand)}>{t("Copy")}</button>
                    </div>
                  )}
                  {wizardStep === 3 && (
                    <div className="stack">
                      {pollState.status === "waiting" && <div className="muted">{t("Waiting for agent...")}</div>}
                      {pollState.status === "timeout" && <div className="error">{t("Agent did not connect in time")}</div>}
                      {pollState.online && <div className="success">{t("Ready")}</div>}
                    </div>
                  )}
                </div>
                <div className="modal-actions">
                  {wizardStep === 1 && (
                    <button className="primary" onClick={createNode}>{t("Create")}</button>
                  )}
                  {wizardStep === 2 && (
                    <button className="primary" onClick={() => { setWizardStep(3); startPolling(); }}>{t("Next")}</button>
                  )}
                  {wizardStep === 3 && (
                    <button className="primary" onClick={() => { setWizardOpen(false); loadNodes(); }}>{t("Done")}</button>
                  )}
                  <button className="secondary" onClick={() => setWizardOpen(false)}>{t("Close")}</button>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
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
  const deployWsRef = useRef(null);
  const [auditOpen, setAuditOpen] = useState(false);
  const [auditLogs, setAuditLogs] = useState([]);
  const [auditNodeID, setAuditNodeID] = useState("");
  const [auditOffset, setAuditOffset] = useState(0);
  const [telegramOpen, setTelegramOpen] = useState(false);
  const [showAgentToken, setShowAgentToken] = useState(false);
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
  const [nodeAutoOpened, setNodeAutoOpened] = useState("");
  const [nodeDetails, setNodeDetails] = useState({ open: false, node: null });
  const [nodeTab, setNodeTab] = useState("overview");
  const [nodeTypeFilter, setNodeTypeFilter] = useState("PANEL");
  const [sidebarActive, setSidebarActive] = useState("nodes");
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
    install_docker: false,
    force_redeploy: true,
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
    agent_url: "",
    agent_token: "",
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
    if (nodes.length === 0) return;
    const params = new URLSearchParams(location.search);
    const sshId = params.get("ssh");
    if (sshId && sshAutoOpened !== sshId) {
      const node = nodes.find((n) => n.id === sshId);
      if (node) {
        if (!node.ssh_host) {
          setError(t("SSH not configured"));
        } else {
          setSshModal({ open: true, node, confirmClose: false });
          setSshAutoOpened(sshId);
        }
      }
    }
  }, [nodes, location.search, sshAutoOpened]);

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const view = params.get("view");
    const add = params.get("add");
    if (view === "panel") {
      setNodeTypeFilter("PANEL");
      setSidebarActive("panels");
    } else if (view === "host") {
      setNodeTypeFilter("HOST");
      setSidebarActive("hosts");
    } else if (view === "bots") {
      setNodeTypeFilter("BOT");
      setSidebarActive("bots");
    } else if (view === "alerts") {
      setSidebarActive("alerts");
    } else if (view === "audit") {
      setSidebarActive("audit");
    } else if (view === "2fa") {
      setSidebarActive("twofa");
    } else if (view === "settings") {
      setSidebarActive("settings");
    } else if (view === "passkeys") {
      setSidebarActive("passkeys");
    } else if (add === "1") {
      setSidebarActive("add");
    } else {
      setSidebarActive("nodes");
    }
    if (view === "alerts" && isAdmin) {
      setTelegramSaved(false);
      setTelegramTestMsg("");
      setTelegramTestStatus("");
      setTelegramTestResults([]);
      setTelegramOpen(true);
      getTelegramSettings()
        .then((data) => {
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
        })
        .catch((err) => setError(err.message));
    }
    if (view === "audit" && isAdmin) {
      openAudit();
    }
    if (view === "2fa" && !isViewer) {
      openTOTP();
    }
    if (view === "passkeys" && !isViewer) {
      openPasskeys();
    }
    if (view === "settings" && isAdmin) {
      setUsersOpen(true);
      loadUsers();
    }
    if (add === "1" && (isAdmin || isOperator)) {
      openAddForm();
    }
    if (!nodes.length) return;
    const nodeId = params.get("node");
    if (nodeId && nodeAutoOpened !== nodeId) {
      const node = nodes.find((n) => n.id === nodeId);
      if (node) {
        openNodeDetails(node);
        setNodeAutoOpened(nodeId);
      }
    }
  }, [location.search, nodes, nodeAutoOpened]);

  useEffect(() => {
    if (!nodeDetails.open) {
      setShowAgentToken(false);
      return;
    }
    setShowAgentToken(false);
  }, [nodeDetails.open, nodeDetails.node?.id]);

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

  async function openNodeDetails(node) {
    setNodeTab("overview");
    setServicesError("");
    setNodeDetails({ open: true, node });
    try {
      const fresh = await request("GET", `/nodes/${node.id}`);
      setNodeDetails({ open: true, node: fresh });
    } catch {
      // keep best-effort data
    }
  }

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
      setForm({
        kind: "PANEL",
        name: "",
        tags: "",
        base_url: "",
        panel_username: "",
        panel_password: "",
        agent_url: "",
        agent_token: "",
        ssh_host: "",
        ssh_port: 22,
        ssh_user: "",
        ssh_key: "",
        verify_tls: true,
      });
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
    const agentUrl = formEl.node_agent_url?.value?.trim();
    const agentToken = formEl.node_agent_token?.value?.trim();
    const sshKey = formEl.node_ssh_key.value;
    if (panelPass) payload.panel_password = panelPass;
    if (agentUrl) payload.agent_url = agentUrl;
    if (agentToken) payload.agent_token = agentToken;
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

  function extractAgentVersion(item, node, logs) {
    const candidates = [];
    if (logs && item?.id && logs[item.id]) {
      candidates.push(logs[item.id]);
    }
    if (item?.log) {
      candidates.push(item.log);
    }
    for (const text of candidates) {
      if (!text) continue;
      const match = text.match(/agent version matches \(([^)]+)\)/i);
      if (match && match[1]) return match[1].trim();
      const alt = text.match(/agent_version[:=\s]+([a-z0-9.\-_]+)/i);
      if (alt && alt[1]) return alt[1].trim();
    }
    if (node?.agent_version) return `v${node.agent_version}`;
    return "";
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
      install_docker: !!deployForm.install_docker,
      force_redeploy: !!deployForm.force_redeploy,
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
    const { success, total, percent } = computeUptime(uptimePoints);
    const latest = metrics && metrics.length > 0 ? metrics[metrics.length - 1] : {};
    const cpu = latest.cpu_pct != null ? formatPercent(latest.cpu_pct) : "-";
    const ram = latest.mem_total_bytes ? `${formatBytes(latest.mem_total_bytes - (latest.mem_available_bytes || 0))} / ${formatBytes(latest.mem_total_bytes)}` : "-";
    const disk = latest.disk_total_bytes ? `${formatBytes(latest.disk_used_bytes || 0)} / ${formatBytes(latest.disk_total_bytes)}` : "-";
    const rx = latest.net_rx_bps != null ? formatBps(latest.net_rx_bps) : null;
    const tx = latest.net_tx_bps != null ? formatBps(latest.net_tx_bps) : null;
    const tcpConn = latest.tcp_connections != null ? latest.tcp_connections : null;
    const udpConn = latest.udp_connections != null ? latest.udp_connections : null;
    return (
      <>
        <div className="details-grid">
          <div className="metric-card">
            <div className="metric-label">{t("CPU usage")}</div>
            <div className="metric-value">{cpu}</div>
            <div className="metric-sub">{t("Load")} {latest.load1 || "-"}</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">{t("RAM usage")}</div>
            <div className="metric-value">{ram}</div>
            <div className="metric-sub">{t("Availability")} {formatPercent(percent)}</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">{t("Disk usage")}</div>
            <div className="metric-value">{disk}</div>
            <div className="metric-sub">{t("Uptime")} {formatDuration(latest.uptime_sec || 0)}</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">{t("Traffic RX / TX")}</div>
            <div className="metric-value">{rx && tx ? `${rx} / ${tx}` : t("No data")}</div>
            <div className="metric-sub">{t("Net iface")} {latest.net_iface || "-"}</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">{t("Active connections")}</div>
            <div className="metric-value">
              {tcpConn != null && udpConn != null ? `${tcpConn} TCP / ${udpConn} UDP` : t("No data")}
            </div>
            <div className="metric-sub">{t("Last check")} {uptimePoints.length ? formatTS(uptimePoints[uptimePoints.length - 1]?.ts) : "-"}</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">{t("Agent health")}</div>
            <div className="metric-value">{node.agent_online ? t("Online") : t("Offline")}</div>
            <div className="metric-sub">{node.agent_version ? `v${node.agent_version}` : "-"}</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">{t("Panel health")}</div>
            <div className="metric-value">{node.panel_version ? t("Online") : t("Offline")}</div>
            <div className="metric-sub">{node.panel_version || "-"}</div>
          </div>
        </div>

        <div className="node-availability">
          <div className="availability-header">
            <div className="muted small">{t("Last {total} checks", { total: total || 0 })}</div>
            <div className="muted small">{t("{success}/{total} successful", { success, total: total || 0 })}</div>
          </div>
          <Sparkline points={uptimePoints} />
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
        <div className={`data-table nodes-table bots-table ${showNode ? "with-node" : ""}`}>
          <div className="data-row head">
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
            <div className="data-row" key={bot.id}>
              {showNode && <div title={nodeRef.name || ""}>{nodeRef.name || "-"}</div>}
              <div title={bot.name || ""}>{bot.name || "-"}</div>
              <div>{bot.kind || "-"}</div>
              <div title={botTargetLabel(bot)}>{botTargetLabel(bot)}</div>
              <div>{bot.is_enabled ? t("On") : t("Off")}</div>
              <div className="status-cell">
                <StatusBadge status={badgeStatus} />
                <span>{last?.status || "-"}</span>
                {last?.error && <span className="status-error" title={last.error}>{last.error}</span>}
              </div>
              <div>{last?.ts ? formatTS(last.ts) : "-"}</div>
              <div>{last?.latency_ms != null ? `${last.latency_ms}ms` : "-"}</div>
              <div className="actions">
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
          <div className="table-card">
            {renderBotsTable(bots, false)}
          </div>
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
          <div className="table-card">
            {renderBotsTable(bots, true)}
          </div>
        </>
      );
    }

  async function openAudit() {
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
    <div className="app-shell">
      <SidebarNav active={sidebarActive} />
      <div className="app-main">
        <header className="header">
          <div className="header-left" />
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

      <div className="nodes-layout">
        <div className="nodes-toolbar">
          <div>
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
        </div>

        {showingBots && <div className="bots-view">{renderBotsView()}</div>}

        {!showingBots && (
          <div className="table-card">
            <div className="data-table nodes-table selectable">
              <div className="data-row head">
                {(isAdmin || isOperator) && <div />}
                <div>{t("Status")}</div>
                <div>{t("Node Name")}</div>
                <div>{t("Location")}</div>
                <div>{t("Agent Status")}</div>
                <div>{t("Panel Status")}</div>
                <div>{t("Uptime")}</div>
                <div>{t("Last Check")}</div>
                <div>{t("Actions")}</div>
              </div>
              {filteredNodes.map((node) => {
                const uptimePoints = uptimeMap[node.id] || [];
                const { percent } = computeUptime(uptimePoints);
                const lastTs = uptimePoints[uptimePoints.length - 1]?.ts;
                const location = formatLocation(node);
                return (
                  <div
                    className="data-row"
                    key={node.id}
                    onClick={() => openNodeDetails(node)}
                  >
                    {(isAdmin || isOperator) && (
                      <div onClick={(e) => e.stopPropagation()}>
                        <input
                          type="checkbox"
                          checked={!!selectedNodes[node.id]}
                          onChange={() => toggleNodeSelection(node.id)}
                        />
                      </div>
                    )}
                    <div><StatusDot ok={node.online === true} /></div>
                    <div>
                      <div className="node-title">{node.name || t("Unnamed node")}</div>
                      <div className="muted small">{node.kind || "PANEL"}</div>
                    </div>
                    <div className="location-cell">
                      <span className="flag">{location.flag}</span>
                      <span>{location.text}</span>
                    </div>
                    <div>
                      <span className={`badge ${node.agent_online ? "online" : "offline"}`}>
                        {node.agent_online ? t("Online") : t("Offline")}
                      </span>
                      <span className="muted small">{node.agent_version ? `v${node.agent_version}` : "-"}</span>
                    </div>
                    <div>
                      <span className={`badge ${node.kind === "HOST" ? "muted" : node.panel_version ? "online" : "offline"}`}>
                        {node.kind === "HOST" ? t("N/A") : node.panel_version ? t("Online") : t("Offline")}
                      </span>
                      <span className="muted small">{node.panel_version || "-"}</span>
                    </div>
                    <div>
                      <UptimeBar percent={percent} />
                      <span className="muted small">{percent.toFixed(1)}%</span>
                    </div>
                    <div>{lastTs ? formatTS(lastTs) : "-"}</div>
                <div className="row-actions" onClick={(e) => e.stopPropagation()}>
                      <button
                        type="button"
                        className="ghost"
                        onClick={() => {
                          if (node.base_url) {
                            window.open(node.base_url, "_blank");
                          } else {
                            openNodeDetails(node);
                          }
                        }}
                      >
                        {t("Open Dashboard")}
                      </button>
                      <button type="button" className="ghost" disabled>{t("Restart Agent")}</button>
                      <button type="button" className="ghost" onClick={() => openSSH(node)}>{t("SSH")}</button>
                    </div>
                  </div>
                );
              })}
              {filteredNodes.length === 0 && (
                <div className="data-row">
                  <div>{t("No data")}</div>
                </div>
              )}
            </div>
          </div>
        )}
      </div>

      {nodeDetails.open && nodeDetails.node && (
        <div className="modal node-details-modal">
          <div className="modal-content node-details-content">
            <div className="node-details-header">
              <div>
                <div className="node-name">{nodeDetails.node.name || t("Unnamed node")}</div>
                <div className="muted small">{nodeDetails.node.kind || "PANEL"}</div>
                <div className="muted small">{nodeDetails.node.kind === "HOST" ? t("Base URL: not used") : (nodeDetails.node.base_url || t("No base URL"))}</div>
                <div className="node-id">
                  <span className="muted small">{t("Node ID")}: {nodeDetails.node.id}</span>
                  <button type="button" className="ghost small" onClick={() => copyText(nodeDetails.node.id)}>
                    {t("Copy")}
                  </button>
                </div>
                {isAdmin && nodeDetails.node.agent_token && (
                  <div className="node-id">
                    <span className="muted small">{t("Agent token")}: {showAgentToken ? nodeDetails.node.agent_token : maskSecret(nodeDetails.node.agent_token)}</span>
                    <button
                      type="button"
                      className="ghost small"
                      onClick={() => {
                        if (!showAgentToken) {
                          if (!confirm(t("Reveal agent token?"))) return;
                        }
                        setShowAgentToken((prev) => !prev);
                      }}
                    >
                      {showAgentToken ? t("Hide") : t("Show")}
                    </button>
                    <button type="button" className="ghost small" onClick={() => copyText(nodeDetails.node.agent_token)}>
                      {t("Copy")}
                    </button>
                  </div>
                )}
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
              <input name="node_agent_url" autoComplete="off" placeholder={t("Agent URL")} defaultValue={editModal.node.agent_url || ""} />
              <input name="node_agent_token" autoComplete="new-password" placeholder={t("Agent Token (leave blank to keep)")} type="password" />
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
                <input name="node_agent_url" autoComplete="off" placeholder={t("Agent URL")} value={form.agent_url} onChange={(e) => setForm({ ...form, agent_url: e.target.value })} />
                <input name="node_agent_token" autoComplete="new-password" placeholder={t("Agent Token")} type="password" value={form.agent_token} onChange={(e) => setForm({ ...form, agent_token: e.target.value })} />
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
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={deployForm.install_docker}
                  onChange={(e) => setDeployForm({ ...deployForm, install_docker: e.target.checked })}
                />
                {t("Install Docker")}
              </label>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={deployForm.force_redeploy}
                  onChange={(e) => setDeployForm({ ...deployForm, force_redeploy: e.target.checked })}
                />
                {t("Redeploy if version differs")}
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
                const agentVersion = extractAgentVersion(item, node, deployLogs);
                return (
                  <div className="deploy-item" key={item.id}>
                  <div className="deploy-item-head">
                    <div className="deploy-item-title">{node?.name || item.node_id}</div>
                    <span className={`badge ${item.status || "queued"}`}>{item.status || "queued"}</span>
                  </div>
                  {agentVersion && <div className="muted small">{t("Agent")} {agentVersion}</div>}
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
        <div className="modal overlay-modal" onClick={() => setAuditOpen(false)}>
          <div className="modal-content wide" onClick={(e) => e.stopPropagation()}>
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
  </div>
  );
}

function DashboardPage() {
  const { t, lang, setLang } = useI18n();
  const navigate = useNavigate();
  const user = getUser();
  const [nodes, setNodes] = useState([]);
  const [activeUsers, setActiveUsers] = useState([]);
  const [aggregate, setAggregate] = useState({
    nodes_online: 0,
    nodes_total: 0,
    agents_active: 0,
    agents_total: 0,
    panels_available: 0,
      avg_cpu: 0,
      avg_ping_ms: null,
      total_traffic_24h: null,
      total_traffic_7d: null,
      active_alerts: 0,
    });
  const [generatedAt, setGeneratedAt] = useState("");
  const [systemStatus, setSystemStatus] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [searchNodes, setSearchNodes] = useState("");
  const [searchUsers, setSearchUsers] = useState("");
  const [sandboxOnly, setSandboxOnly] = useState(false);
  const [wsStatus, setWsStatus] = useState("disconnected");
  const [now, setNow] = useState(Date.now());
  const [problemsOpen, setProblemsOpen] = useState(false);
  const [problemsList, setProblemsList] = useState([]);
  const [problemsError, setProblemsError] = useState("");
  const [problemsLoading, setProblemsLoading] = useState(false);
  const [problemsNode, setProblemsNode] = useState("");
  const [problemDetails, setProblemDetails] = useState(null);
  const wsRef = useRef(null);

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(timer);
  }, []);

  function dedupeActiveUsers(list) {
    const deduped = new Map();
    list.forEach((row) => {
      if (!row) return;
      const email = (row.client_email || "").trim().toLowerCase();
      if (!email) return;
      const prev = deduped.get(email);
      if (!prev) {
        deduped.set(email, row);
        return;
      }
      const prevTotal = (prev.total_up_bytes || 0) + (prev.total_down_bytes || 0);
      const rowTotal = (row.total_up_bytes || 0) + (row.total_down_bytes || 0);
      if (rowTotal > prevTotal) {
        deduped.set(email, row);
        return;
      }
      const prevSeen = prev.last_seen ? new Date(prev.last_seen).getTime() : 0;
      const rowSeen = row.last_seen ? new Date(row.last_seen).getTime() : 0;
      if (rowSeen > prevSeen) {
        deduped.set(email, row);
      }
    });
    return Array.from(deduped.values()).sort(
      (a, b) => new Date(b.last_seen).getTime() - new Date(a.last_seen).getTime()
    );
  }

  async function loadSummary() {
    setLoading(true);
    setError("");
    try {
      const data = await request("GET", "/dashboard/summary");
      setNodes(data.nodes || []);
      setAggregate(data.aggregate || {});
      setGeneratedAt(data.generated_at || "");
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }

  async function loadSystemStatus() {
    try {
      const data = await request("GET", "/system/status");
      setSystemStatus(data || null);
    } catch {
      setSystemStatus(null);
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

  async function loadProblems() {
    setProblemsLoading(true);
    setProblemsError("");
    try {
      const data = await request("GET", "/alerts?active=true&limit=200");
      setProblemsList(Array.isArray(data) ? data : []);
    } catch (err) {
      setProblemsError(err.message);
    } finally {
      setProblemsLoading(false);
    }
  }

  useEffect(() => {
    loadSummary();
    loadUsers();
    loadSystemStatus();
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
            const filtered = prev.filter((u) => u.node_id !== node_id && u.client_email);
            const mapped = Array.isArray(users)
              ? users.map((u) => {
                  if (typeof u === "string") {
                    return { client_email: u, node_id, node_name, last_seen: new Date().toISOString() };
                  }
                  const clientEmail = u.client_email || u.ClientEmail || "";
                  const inboundTag = u.inbound_tag ?? u.InboundTag ?? null;
                  const ip = u.ip || u.IP || "";
                  return {
                    ...u,
                    client_email: clientEmail,
                    inbound_tag: inboundTag,
                    ip,
                    node_id,
                    node_name: node_name || u.node_name,
                  };
                })
              : [];
            return dedupeActiveUsers([...filtered, ...mapped]);
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

  const nodeNameById = useMemo(() => {
    const map = {};
    nodes.forEach((node) => {
      const id = node.node_id || node.id;
      if (!id) return;
      map[id] = node.name || id;
    });
    return map;
  }, [nodes]);

  const problemsFiltered = useMemo(() => {
    if (!problemsNode) return problemsList;
    return problemsList.filter((row) => {
      const rowNode = row.node_id || row.nodeId || row.node || row.target_id;
      return rowNode === problemsNode;
    });
  }, [problemsList, problemsNode]);

  const usersFiltered = useMemo(() => {
    const term = searchUsers.trim().toLowerCase();
    if (!term) return activeUsers;
    return activeUsers.filter((u) => (u.client_email || "").toLowerCase().includes(term));
  }, [activeUsers, searchUsers]);

  const activeUsersSummary = useMemo(() => {
    if (nodes.length === 0) return t("No data");
    const availableCount = nodes.filter((n) => n.active_users_available).length;
    const sourceSet = new Set(nodes.map((n) => n.active_users_source).filter(Boolean));
    if (availableCount === 0) {
      return t("Active users source not available");
    }
    if (sourceSet.size === 1) {
      const source = Array.from(sourceSet)[0];
      return `${t("Source")}: ${source}`;
    }
    return t("Multiple sources");
  }, [nodes, t]);

  const aggregateSafe = useMemo(() => {
    const fallbackTotal = nodesFiltered.length;
    const fallbackOnline = nodesFiltered.filter((n) => n.agent_online).length;
    return {
      nodesOnline: aggregate.nodes_online ?? fallbackOnline,
      nodesTotal: aggregate.nodes_total ?? fallbackTotal,
      agentsActive: aggregate.agents_active ?? 0,
      agentsTotal: aggregate.agents_total ?? 0,
      panelsAvailable: aggregate.panels_available ?? 0,
        avgCPU: aggregate.avg_cpu ?? 0,
        avgPingMs: aggregate.avg_ping_ms,
        totalTraffic24h: aggregate.total_traffic_24h,
        totalTraffic7d: aggregate.total_traffic_7d,
        totalRxBps: aggregate.total_rx_bps ?? 0,
        totalTxBps: aggregate.total_tx_bps ?? 0,
      totalConnections: aggregate.total_connections,
      activeAlerts: aggregate.active_alerts ?? 0,
    };
  }, [aggregate, nodesFiltered, nowTs]);

  const deriveNodeStatus = (node) => {
    if (!node.agent_installed) return "no_agent";
    const collectedAt = node.collected_at ? new Date(node.collected_at).getTime() : 0;
    const recent = collectedAt > 0 && Math.abs(nowTs - collectedAt) < 90000;
    if (node.agent_online || recent) return "online";
    return "offline";
  };

  const formatSource = (node) => {
    const source = node.active_users_source || "unknown";
    if (source === "no_source") return t("No source");
    return source;
  };

  const activeIssues = useMemo(() => {
    return nodesFiltered.filter((node) => {
      const status = deriveNodeStatus(node);
      if (status !== "online") return true;
      if (node.kind !== "HOST" && !node.panel_version) return true;
      return false;
    }).length;
  }, [nodesFiltered, nowTs]);

  const totalNodes = aggregateSafe.nodesTotal || 0;
  const nodesOnline = aggregateSafe.nodesOnline || 0;
  const agentsActive = aggregateSafe.agentsActive || 0;
  const agentsTotal = aggregateSafe.agentsTotal || 0;
    const panelsAvailable = aggregateSafe.panelsAvailable || 0;
    const avgPing = aggregateSafe.avgPingMs;
    const traffic24h = aggregateSafe.totalTraffic24h;
    const traffic7d = aggregateSafe.totalTraffic7d;
    const rxBps = aggregateSafe.totalRxBps;
    const txBps = aggregateSafe.totalTxBps;

  return (
    <div className="app-shell">
      <SidebarNav active="dashboard" />
      <div className="app-main">
        <header className="header">
          <div className="header-left" />
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
        <div className="dashboard-sticky">
          <div className="topbar">
            <div>
              <div className="topbar-title">{t("Dashboard")}</div>
              <div className="topbar-sub">{t("Fleet overview")}</div>
            </div>
            <div className="topbar-actions">
              <span className={`badge ${wsStatus}`}>{t(wsStatus === "connected" ? "Connected" : wsStatus === "connecting" ? "Connecting" : "Disconnected")}</span>
              <button onClick={() => { loadSummary(); loadUsers(); loadSystemStatus(); }}>{t("Refresh")}</button>
            </div>
          </div>

          {error && <div className="error">{error}</div>}

          <section className="mini-grid">
            <MiniStatCard
              label={t("Nodes Online")}
              value={`${nodesOnline} / ${totalNodes}`}
              subvalue={t("Fleet availability")}
              progress={totalNodes > 0 ? nodesOnline / totalNodes : 0}
              accent="ok"
            />
            <MiniStatCard
              label={t("Agents Active")}
              value={`${agentsActive} / ${agentsTotal}`}
              subvalue={t("Agent heartbeat")}
              progress={agentsTotal > 0 ? agentsActive / agentsTotal : 0}
              accent="ok"
            />
            <MiniStatCard
              label={t("Panels Available")}
              value={`${panelsAvailable}`}
              subvalue={t("Panels healthy")}
              progress={totalNodes > 0 ? panelsAvailable / totalNodes : 0}
              accent="ok"
            />
            <MiniStatCard
              label={t("Average Ping")}
              value={avgPing != null ? `${avgPing.toFixed(0)} ms` : "-"}
              subvalue={t("Network latency")}
              progress={avgPing != null ? Math.max(0, 1 - avgPing / 200) : 0}
            />
            <MiniStatCard
              label={t("Active issues")}
              value={`${activeIssues}`}
              subvalue={t("Incidents")}
              progress={activeIssues === 0 ? 1 : Math.max(0, 1 - activeIssues / Math.max(1, totalNodes))}
              accent={activeIssues === 0 ? "ok" : "warn"}
              onClick={() => {
                setProblemsOpen(true);
                loadProblems();
              }}
            />
              <MiniStatCard
                label={t("Total Traffic (24h)")}
                value={traffic24h != null ? formatBytes(traffic24h) : `${formatBps(rxBps)} / ${formatBps(txBps)}`}
                subvalue={t("Fleet bandwidth")}
                progress={traffic24h != null ? 0.65 : 0.4}
              />
          </section>
        </div>

        <section className="service-card">
          <div>
            <div className="service-title">VLF Aggregator</div>
            <div className="service-meta">
              <div>{t("Status")}: <span className="status-pill ok">{systemStatus?.status || "running"}</span></div>
              <div>{t("Backend version")}: {systemStatus?.version || "unknown"}</div>
              <div>{t("Last sync")}: {systemStatus?.last_sync || generatedAt || "-"}</div>
            </div>
          </div>
          <div className="service-actions">
            <button type="button" className="secondary" onClick={() => navigate("/nodes")}>{t("Open Dashboard")}</button>
          </div>
        </section>

        <section className="table-card">
          <div className="section-head">
            <div>
              <h3>{t("Nodes")}</h3>
              <span className="muted small">{t("Realtime infrastructure status")}</span>
            </div>
            <div className="section-actions">
              <input value={searchNodes} onChange={(e) => setSearchNodes(e.target.value)} placeholder={t("Search nodes")} />
              <label className="checkbox">
                <input type="checkbox" checked={sandboxOnly} onChange={(e) => setSandboxOnly(e.target.checked)} />
                <span>{t("Sandbox only")}</span>
              </label>
            </div>
          </div>
          <div className="data-table nodes-table">
            <div className="data-row head">
              <div>{t("Status")}</div>
              <div>{t("Node Name")}</div>
              <div>{t("Location")}</div>
              <div>{t("Agent Status")}</div>
              <div>{t("Panel Status")}</div>
              <div>{t("Uptime")}</div>
              <div>{t("Last Check")}</div>
              <div>{t("Actions")}</div>
            </div>
            {nodesFiltered.map((node) => {
              const status = deriveNodeStatus(node);
              const location = formatLocation(node);
              const uptimePct = node.uptime_sec ? Math.min(100, (node.uptime_sec / 86400) * 100) : 0;
              return (
                <div
                  className="data-row"
                  key={node.node_id}
                  onClick={() => navigate(`/nodes?node=${node.node_id}`)}
                >
                  <div><StatusDot ok={status === "online"} /></div>
                  <div>
                    <div className="node-title">{node.name}</div>
                    <div className="muted small">{node.kind || "PANEL"}</div>
                  </div>
                  <div className="location-cell">
                    <span className="flag">{location.flag}</span>
                    <span>{location.text}</span>
                  </div>
                  <div>
                    <span className={`badge ${node.agent_online ? "online" : "offline"}`}>
                      {node.agent_online ? t("Online") : t("Offline")}
                    </span>
                    <span className="muted small">{node.agent_version ? `v${node.agent_version}` : "-"}</span>
                  </div>
                  <div>
                    <span className={`badge ${node.panel_running ? "online" : "offline"}`}>
                      {node.panel_running ? t("Online") : t("Offline")}
                    </span>
                    <span className="muted small">{node.panel_version || "-"}</span>
                  </div>
                  <div>
                    <div className="uptime-line">
                      <UptimeBar percent={uptimePct} />
                      <span className="muted small">{node.uptime_sec ? formatDuration(node.uptime_sec) : "-"}</span>
                    </div>
                  </div>
                  <div>{node.collected_at ? formatTS(node.collected_at) : "-"}</div>
                  <div className="row-actions" onClick={(e) => e.stopPropagation()}>
                    <button
                      type="button"
                      className="ghost"
                      onClick={() => navigate(`/nodes?node=${node.node_id}`)}
                    >
                      {t("Open Dashboard")}
                    </button>
                    <button type="button" className="ghost" disabled>{t("Restart Agent")}</button>
                    <button
                      type="button"
                      className="ghost"
                      onClick={() => navigate(`/nodes?ssh=${node.node_id}`)}
                    >
                      {t("SSH")}
                    </button>
                  </div>
                </div>
              );
            })}
            {nodesFiltered.length === 0 && (
              <div className="data-row">
                <div>{loading ? t("Loading...") : t("No data")}</div>
              </div>
            )}
          </div>
        </section>

        <section className="grid-bottom">
            <div className="bottom-card">
              <h4>{t("Total Traffic")}</h4>
              <div className="bottom-value">
                {traffic24h != null ? formatBytes(traffic24h) : `${formatBps(rxBps)} / ${formatBps(txBps)}`}
              </div>
              <div className="muted small">{t("Sent / Received")}</div>
              <div className="bottom-sub">
                {traffic24h != null && traffic7d != null
                  ? `${t("24h")}: ${formatBytes(traffic24h)} · ${t("7d")}: ${formatBytes(traffic7d)}`
                  : t("24h + 7d counters")}
              </div>
            </div>
          <div className="bottom-card">
            <h4>{t("Connections")}</h4>
            <div className="bottom-value">{aggregateSafe.totalConnections != null ? aggregateSafe.totalConnections : t("No data")}</div>
            <div className="muted small">{t("TCP / UDP")}</div>
            <div className="bottom-sub">{t("Realtime sockets")}</div>
          </div>
          <div className="bottom-card">
            <h4>{t("Agent Health")}</h4>
            <div className="bottom-value">{agentsActive} {t("Online")}</div>
            <div className="muted small">{agentsTotal - agentsActive} {t("Offline")}</div>
            <div className="bottom-sub">{t("Last agent errors")}</div>
          </div>
            <div className="bottom-card clickable" onClick={() => { setProblemsOpen(true); loadProblems(); }} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { setProblemsOpen(true); loadProblems(); } }}>
              <h4>{t("Active problems")}</h4>
              <div className="bottom-value">{activeIssues}</div>
              <div className="muted small">{t("Active issues now")}</div>
              <div className="bottom-sub">{t("Nodes with issues")}</div>
            </div>
        </section>

        <section className="table-card">
          <div className="section-head">
            <div>
              <h3>{t("Realtime active users")}</h3>
              <span className="muted small">{t("Across all nodes")}</span>
            </div>
            <div className="section-actions">
              <input value={searchUsers} onChange={(e) => setSearchUsers(e.target.value)} placeholder={t("Search users")} />
            </div>
          </div>
          <div className="data-table users-table">
            <div className="data-row head">
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
              <div className="data-row" key={user.id || `${user.node_id}-${user.client_email}-${user.ip || ""}`}>
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
              <div className="data-row">
                <div>{loading ? t("Loading...") : activeUsersSummary}</div>
              </div>
            )}
          </div>
        </section>

        {problemsOpen && (
          <div className="modal overlay-modal" onClick={() => setProblemsOpen(false)}>
            <div className="modal-content wide" onClick={(e) => e.stopPropagation()}>
              <h3>{t("Active problems")}</h3>
              <div className="audit-controls">
                <select value={problemsNode} onChange={(e) => setProblemsNode(e.target.value)}>
                  <option value="">{t("All nodes")}</option>
                  {nodes.map((node) => {
                    const id = node.node_id || node.id;
                    return (
                      <option key={id} value={id}>
                        {node.name || id}
                      </option>
                    );
                  })}
                </select>
                <button type="button" onClick={loadProblems}>{t("Refresh")}</button>
              </div>
              {problemsError && <div className="error">{problemsError}</div>}
              <div className="table compact audit-table">
                <div className="table-row head">
                  <div>{t("Node")}</div>
                  <div>{t("Type")}</div>
                  <div>{t("Status")}</div>
                  <div>{t("Last seen")}</div>
                  <div>{t("Message")}</div>
                  <div>{t("Actions")}</div>
                </div>
                {problemsFiltered.map((row) => {
                  const rowNode = row.node_id || row.nodeId || row.node || row.target_id;
                  const nodeName = nodeNameById[rowNode] || rowNode || "-";
                  const status = row.last_status || row.status || "fail";
                  const message = formatProblemMessage(row, t);
                  const occurrences = row.occurrences || row.count || 0;
                  return (
                    <div className="table-row" key={row.id || row.fingerprint}>
                      <div data-label={t("Node")}>
                        <span>{nodeName}</span>
                        {occurrences > 0 && <span className="badge problem-count">×{occurrences}</span>}
                      </div>
                      <div data-label={t("Type")}>{row.alert_type || row.type || row.check_type || "-"}</div>
                      <div data-label={t("Status")}>{status}</div>
                      <div data-label={t("Last seen")}>{formatTS(row.last_seen || row.updated_at || row.created_at)}</div>
                      <div data-label={t("Message")}>{message}</div>
                      <div className="actions">
                        <button type="button" onClick={() => setProblemDetails({ ...row, nodeName, message })}>{t("Open")}</button>
                      </div>
                    </div>
                  );
                })}
                {problemsFiltered.length === 0 && (
                  <div className="table-row">
                    <div>{problemsLoading ? t("Loading...") : t("No data")}</div>
                  </div>
                )}
              </div>
              <div className="actions">
                <button type="button" onClick={() => setProblemsOpen(false)}>{t("Close")}</button>
              </div>
            </div>
          </div>
        )}

        {problemDetails && (
          <div className="modal overlay-modal" onClick={() => setProblemDetails(null)}>
            <div className="modal-content" onClick={(e) => e.stopPropagation()}>
              <h3>{t("Problem details")}</h3>
              <div className="detail-grid">
                <div>
                  <div className="muted small">{t("Node")}</div>
                  <div>{problemDetails.nodeName || "-"}</div>
                </div>
                <div>
                  <div className="muted small">{t("Type")}</div>
                  <div>{problemDetails.alert_type || problemDetails.type || problemDetails.check_type || "-"}</div>
                </div>
                <div>
                  <div className="muted small">{t("Status")}</div>
                  <div>{problemDetails.last_status || problemDetails.status || "-"}</div>
                </div>
                <div>
                  <div className="muted small">{t("Last seen")}</div>
                  <div>{formatTS(problemDetails.last_seen || problemDetails.updated_at || problemDetails.created_at)}</div>
                </div>
              </div>
              <div className="muted small">{t("Message")}</div>
              <div className="detail-message">{problemDetails.message || formatProblemMessage(problemDetails, t)}</div>
              <div className="actions">
                <button type="button" onClick={() => {
                  const rowNode = problemDetails.node_id || problemDetails.nodeId || problemDetails.node || problemDetails.target_id;
                  if (rowNode) navigate(`/nodes?node=${rowNode}`);
                }}>{t("Open node")}</button>
                <button type="button" onClick={() => setProblemDetails(null)}>{t("Close")}</button>
              </div>
            </div>
          </div>
        )}
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
  const [preview, setPreview] = useState({ open: false, entry: null, content: "", imageUrl: "", note: "", editable: false });
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef(null);
  const role = getRole();
  const canWrite = role === "admin";

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
        let list = [];
        try {
          const data = await request("GET", `/nodes/${nodeId}/files/roots`);
          list = data?.roots || [];
        } catch {
          list = [];
        }
        if (!active) return;
        const rootList = [{ path: "/", label: "/" }, ...list].filter((item, idx, arr) => arr.findIndex((r) => r.path === item.path) === idx);
        setRoots(rootList);
        setCurrentPath("/");
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
      const data = await request("GET", `/nodes/${nodeId}/fs/list?path=${encodeURIComponent(pathValue)}`);
      const mapped = (data || []).map((item) => ({
        name: item.name,
        path: item.path,
        is_dir: item.type === "dir",
        size: item.size,
        modified: item.modified,
        type: item.type,
        mime_guess: "",
        mode: item.mode,
      }));
      setEntries(mapped);
    } catch (err) {
      const code = err?.data?.error?.code;
      if (code === "AGENT_UNSUPPORTED") {
        setError(t("Node agent is outdated. Redeploy agent to enable full filesystem."));
      } else {
        setError(err.message);
      }
    } finally {
      setBusy(false);
    }
  }

  async function loadTreeChildren(pathValue) {
    setTree((prev) => ({ ...prev, [pathValue]: { ...(prev[pathValue] || {}), loading: true } }));
    try {
      const data = await request("GET", `/nodes/${nodeId}/fs/list?path=${encodeURIComponent(pathValue)}`);
      const children = (data || []).filter((item) => item.type === "dir").map((item) => ({ name: item.name, path: item.path }));
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
    const res = await fetch(`${API_BASE}/nodes/${nodeId}/fs/download?path=${encodeURIComponent(entry.path)}`, {
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
    if (entry.is_dir) {
      const confirmRecursive = confirm(t("Delete folder {name}? This cannot be undone.", { name: entry.name }));
      if (!confirmRecursive) return;
      await request("POST", `/nodes/${nodeId}/fs/delete`, { path: entry.path, recursive: true });
    } else {
      if (!confirm(t("Delete file {name}?", { name: entry.name }))) return;
      await request("POST", `/nodes/${nodeId}/fs/delete`, { path: entry.path });
    }
    await loadList(currentPath);
  }

  async function renameEntry(entry) {
    const next = prompt(t("New name"), entry.name);
    if (!next || next === entry.name) return;
    const parent = entry.path.split("/").slice(0, -1).join("/") || "/";
    const newPath = joinPath(parent, next);
    await request("POST", `/nodes/${nodeId}/fs/rename`, { from: entry.path, to: newPath });
    await loadList(currentPath);
  }

  async function createFolder() {
    const name = prompt(t("Folder name"));
    if (!name) return;
    await request("POST", `/nodes/${nodeId}/fs/mkdir`, { path: joinPath(currentPath, name) });
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
      const res = await fetch(`${API_BASE}/nodes/${nodeId}/fs/upload?path=${encodeURIComponent(currentPath)}`, {
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
    setPreview({ open: true, entry, content: "", imageUrl: "", note: "", editable: false });
    try {
      const data = await request("GET", `/nodes/${nodeId}/fs/read?path=${encodeURIComponent(entry.path)}`);
      setPreview({ open: true, entry, content: data.content || "", imageUrl: "", note: "", editable: true });
    } catch (err) {
      const code = err?.data?.error?.code;
      if (code === "too_large") {
        setPreview({ open: true, entry, content: "", imageUrl: "", note: t("File too large to preview"), editable: false });
        return;
      }
      if (code === "not_text") {
        setPreview({ open: true, entry, content: "", imageUrl: "", note: t("File is not text"), editable: false });
        return;
      }
      setPreview({ open: true, entry, content: "", imageUrl: "", note: err.message, editable: false });
    }
  }

  async function savePreview() {
    if (!preview.entry || !canWrite) return;
    try {
      await request("PUT", `/nodes/${nodeId}/fs/write?path=${encodeURIComponent(preview.entry.path)}`, { content: preview.content });
      setPreview((prev) => ({ ...prev, note: t("File saved") }));
      await loadList(currentPath);
    } catch (err) {
      setPreview((prev) => ({ ...prev, note: err.message || t("Save failed") }));
    }
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
    <div className="app-shell">
      <SidebarNav active="files" />
      <div className="app-main">
        <div className="page page-wide">
      <header className="header">
        <div className="header-left">
          <button className="icon-button" onClick={() => navigate("/nodes")}>{"<"}</button>
          <h2>{t("File Manager")}</h2>
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
        </div>
        <div className="files-actions">
          <input
            className="files-search"
            placeholder={t("Search")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <button type="button" className="secondary" onClick={createFolder} disabled={!canWrite}>{t("New folder")}</button>
          <button type="button" onClick={() => fileInputRef.current?.click()} disabled={uploading || !canWrite}>{uploading ? t("Loading...") : t("Upload")}</button>
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
          <div className="files-shortcuts">
            <div className="muted small">{t("Quick roots")}</div>
            {roots.map((root) => (
              <button key={root.path} type="button" className={`tree-item ${currentPath === root.path ? "active" : ""}`} onClick={() => setCurrentPath(root.path)}>
                {root.label}
              </button>
            ))}
          </div>
          <div className="tree-divider" />
          {renderTreeNode("/", "/")}
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
                  <button type="button" className="secondary" onClick={() => renameEntry(entry)} disabled={!canWrite}>{t("Rename")}</button>
                  <button type="button" className="danger" onClick={() => deleteEntry(entry)} disabled={!canWrite}>{t("Delete")}</button>
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
                  setPreview({ open: false, entry: null, content: "", imageUrl: "", note: "", editable: false });
                }}>
                  {t("Close")}
                </button>
              </div>
              {preview.note && <div className="muted small">{preview.note}</div>}
              {preview.imageUrl && <img className="file-preview-image" src={preview.imageUrl} alt="preview" />}
              {preview.editable && (
                <textarea
                  rows={20}
                  value={preview.content}
                  onChange={(e) => setPreview((prev) => ({ ...prev, content: e.target.value }))}
                  readOnly={!canWrite}
                />
              )}
              {!preview.editable && preview.content && (
                <textarea readOnly rows={20} value={preview.content} />
              )}
              {!preview.imageUrl && !preview.content && (
                <div className="muted small">{t("No preview available")}</div>
              )}
              <div className="actions">
                {preview.entry && (
                  <button type="button" onClick={() => downloadEntry(preview.entry)}>{t("Download")}</button>
                )}
                {preview.editable && canWrite && (
                  <button type="button" onClick={savePreview}>{t("Save")}</button>
                )}
              </div>
            </div>
          </div>
        )}
        </div>
      </div>
    </div>
  );
}

function DbWorkPage() {
  const { t } = useI18n();
  const [nodes, setNodes] = useState([]);
  const [nodeId, setNodeId] = useState("");
  const [tab, setTab] = useState("sqlite");
  const [sqliteFiles, setSqliteFiles] = useState([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [sqliteUI, setSqliteUI] = useState("");
  const [adminerUI, setAdminerUI] = useState("");
  const [sqliteReadOnly, setSqliteReadOnly] = useState(true);

  useEffect(() => {
    let active = true;
    async function loadNodes() {
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
    loadNodes();
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    setSqliteFiles([]);
    setSqliteUI("");
    setAdminerUI("");
    setError("");
    if (!nodeId) return;
    if (tab === "sqlite") {
      loadSqliteList();
    }
  }, [nodeId, tab]);

  async function loadSqliteList() {
    setBusy(true);
    setError("");
    try {
      const data = await request("GET", `/nodes/${nodeId}/db/sqlite/list`);
      setSqliteFiles(data?.files || []);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function openSqlite(file) {
    setBusy(true);
    setError("");
    try {
      const data = await request("POST", `/nodes/${nodeId}/db/sqlite/start`, {
        path: file.path,
        read_only: sqliteReadOnly,
      });
      const token = getToken();
      const suffix = token ? `?token=${encodeURIComponent(token)}` : "";
      setSqliteUI(data?.proxy_path ? `${API_BASE}${data.proxy_path}${suffix}` : "");
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function openAdminer(engine) {
    setBusy(true);
    setError("");
    try {
      const data = await request("POST", `/nodes/${nodeId}/db/adminer/start`, { engine });
      const token = getToken();
      const suffix = token ? `?token=${encodeURIComponent(token)}` : "";
      setAdminerUI(data?.proxy_path ? `${API_BASE}${data.proxy_path}${suffix}` : "");
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="app-shell">
      <SidebarNav active="dbwork" />
      <div className="app-main">
        <div className="page page-wide">
          <header className="header">
            <div className="header-left">
              <h2>{t("DB work")}</h2>
            </div>
          </header>
          <div className="db-toolbar">
            <label>
              {t("Node")}
              <select value={nodeId} onChange={(e) => setNodeId(e.target.value)}>
                {nodes.map((node) => (
                  <option key={node.id} value={node.id}>{node.name}</option>
                ))}
              </select>
            </label>
            <div className="db-tabs">
              <button type="button" className={`tab ${tab === "sqlite" ? "active" : ""}`} onClick={() => setTab("sqlite")}>SQLite</button>
              <button type="button" className={`tab ${tab === "postgres" ? "active" : ""}`} onClick={() => setTab("postgres")}>Postgres</button>
              <button type="button" className={`tab ${tab === "mysql" ? "active" : ""}`} onClick={() => setTab("mysql")}>MySQL</button>
            </div>
          </div>
          {error && <div className="error">{error}</div>}
            {tab === "sqlite" && (
              <div className="card db-section">
                <div className="db-section-header">
                  <div className="muted small">{t("SQLite databases")}</div>
                  <div className="db-section-actions">
                    <label className="db-toggle">
                      <input
                        type="checkbox"
                        checked={sqliteReadOnly}
                        onChange={(e) => setSqliteReadOnly(e.target.checked)}
                      />
                      <span>{t("Read-only")}</span>
                    </label>
                    <button className="secondary" onClick={loadSqliteList} disabled={busy}>{t("Refresh")}</button>
                  </div>
                </div>
                {!sqliteReadOnly && (
                  <div className="db-warning">
                    {t("Write access is enabled. Changes are applied immediately.")}
                  </div>
                )}
              <div className="data-table db-table">
                <div className="data-row head">
                  <div>{t("Name")}</div>
                  <div>{t("Size")}</div>
                  <div>{t("Modified")}</div>
                  <div>{t("Actions")}</div>
                </div>
                {sqliteFiles.map((file) => (
                  <div className="data-row" key={file.path}>
                    <div>{file.name}</div>
                    <div>{formatBytes(file.size)}</div>
                    <div>{formatTS(file.mtime)}</div>
                    <div>
                      <button type="button" onClick={() => openSqlite(file)} disabled={busy}>{t("Open")}</button>
                    </div>
                  </div>
                ))}
                {sqliteFiles.length === 0 && (
                  <div className="data-row">
                    <div>{busy ? t("Loading...") : t("No databases found")}</div>
                  </div>
                )}
              </div>
              {sqliteUI && (
                <div className="card db-iframe">
                  <iframe title="SQLite Web" src={sqliteUI} />
                </div>
              )}
            </div>
          )}
          {tab !== "sqlite" && (
            <div className="card db-section">
              <div className="db-section-header">
                <div className="muted small">{t("Adminer")} ({tab})</div>
                <button onClick={() => openAdminer(tab)} disabled={busy}>{t("Open")}</button>
              </div>
              {adminerUI && (
                <div className="card db-iframe">
                  <iframe title="Adminer" src={adminerUI} />
                </div>
              )}
              {!adminerUI && <div className="muted small">{t("Adminer will open via node-agent proxy.")}</div>}
            </div>
          )}
        </div>
      </div>
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
          path="/panels"
          element={
            <RequireAuth>
              <PanelsSelfServicePage />
            </RequireAuth>
          }
        />
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
          path="/db"
          element={
            <RequireAuth>
              <DbWorkPage />
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



