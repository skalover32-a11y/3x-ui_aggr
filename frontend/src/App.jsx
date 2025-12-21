import React, { useEffect, useMemo, useState } from "react";
import { Routes, Route, useNavigate, useLocation, useParams, Link } from "react-router-dom";
import { request, setToken, getToken, convertSSHKey } from "./api.js";
import InboundEditor from "./components/InboundEditor.jsx";

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
  const [error, setError] = useState("");
  const navigate = useNavigate();

  async function onSubmit(e) {
    e.preventDefault();
    setError("");
    try {
      const data = await request("POST", "/auth/login", { username, password });
      setToken(data.token);
      navigate("/nodes");
    } catch (err) {
      setError(err.message);
    }
  }

  return (
    <div className="page center">
      <form className="card" onSubmit={onSubmit}>
        <h1>3x-ui Aggregator</h1>
        <label>
          Username
          <input value={username} onChange={(e) => setUsername(e.target.value)} />
        </label>
        <label>
          Password
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        {error && <div className="error">{error}</div>}
        <button type="submit">Login</button>
      </form>
    </div>
  );
}

function NodesPage() {
  const [nodes, setNodes] = useState([]);
  const [error, setError] = useState("");
  const [keyPassphrase, setKeyPassphrase] = useState("");
  const [keyFingerprint, setKeyFingerprint] = useState("");
  const [statusMap, setStatusMap] = useState({});
  const [uptimeMap, setUptimeMap] = useState({});
  const [metricsMap, setMetricsMap] = useState({});
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
  }, [nodes]);

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
    setError("");
    try {
      await request("POST", `/nodes/${id}/actions/restart-xray`, {});
      alert("Xray restart requested");
    } catch (err) {
      setError(err.message);
    }
  }

  async function onReboot(id) {
    const confirm = prompt('Type "REBOOT" to confirm reboot');
    if (confirm !== "REBOOT") {
      return;
    }
    setError("");
    try {
      await request("POST", `/nodes/${id}/actions/reboot`, { confirm: "REBOOT" });
      alert("Reboot requested");
    } catch (err) {
      setError(err.message);
    }
  }

  function openEdit(node) {
    setEditModal({ open: true, node });
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

  async function onDelete(node) {
    const confirm = prompt(`Type DELETE to remove node ${node.name}`);
    if (confirm !== "DELETE") return;
    setError("");
    try {
      await request("DELETE", `/nodes/${node.id}`, {});
      loadNodes();
    } catch (err) {
      setError(err.message);
    }
  }

  return (
    <div className="page">
      <header className="header">
        <h2>Nodes</h2>
        <button onClick={() => { setToken(""); window.location.href = "/login"; }}>Logout</button>
      </header>

      <form className="card form-grid" onSubmit={onCreate}>
        <h3>Add Node</h3>
        <input placeholder="Name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        <input placeholder="Tags (comma)" value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} />
        <input placeholder="Base URL" value={form.base_url} onChange={(e) => setForm({ ...form, base_url: e.target.value })} />
        <input placeholder="Panel Username" value={form.panel_username} onChange={(e) => setForm({ ...form, panel_username: e.target.value })} />
        <input placeholder="Panel Password" type="password" value={form.panel_password} onChange={(e) => setForm({ ...form, panel_password: e.target.value })} />
        <input placeholder="SSH Host" value={form.ssh_host} onChange={(e) => setForm({ ...form, ssh_host: e.target.value })} />
        <input placeholder="SSH Port" type="number" value={form.ssh_port} onChange={(e) => setForm({ ...form, ssh_port: Number(e.target.value) })} />
        <input placeholder="SSH User" value={form.ssh_user} onChange={(e) => setForm({ ...form, ssh_user: e.target.value })} />
        <input placeholder="Key Passphrase (optional)" type="password" value={keyPassphrase} onChange={(e) => setKeyPassphrase(e.target.value)} />
        <label className="file-input">
          Upload SSH Key (.ppk/.pem/.key)
          <input type="file" accept=".ppk,.pem,.key" onChange={onKeyUpload} />
        </label>
        <textarea placeholder="SSH Private Key" rows="3" value={form.ssh_key} onChange={(e) => setForm({ ...form, ssh_key: e.target.value })} />
        <div className="hint">Paste OpenSSH private key or upload .ppk</div>
        {keyFingerprint && <div className="hint">Fingerprint: {keyFingerprint}</div>}
        <label className="checkbox">
          <input type="checkbox" checked={form.verify_tls} onChange={(e) => setForm({ ...form, verify_tls: e.target.checked })} />
          Verify TLS
        </label>
        <button type="submit">Create</button>
      </form>

      {error && <div className="error">{error}</div>}

      <div className="nodes-cards">
        <div className="nodes-cards-head">
          <div>
            <h3>Nodes Manager</h3>
            <div className="muted">{nodes.length} servers configured</div>
          </div>
          <button onClick={() => window.scrollTo({ top: 0, behavior: "smooth" })}>Add Node</button>
        </div>

        {nodes.map((node) => {
          const uptimePoints = uptimeMap[node.id] || [];
          const { percent, success, total } = computeUptime(uptimePoints);
          const lastTs = uptimePoints[uptimePoints.length - 1]?.ts;

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
                  <div className="uptime-arrow">▾</div>
                </div>
              </div>

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
                  <div className="meta-value">{node.ssh_port || "—"}</div>
                </div>
                <div className="meta-box">
                  <div className="meta-label">Panel User</div>
                  <div className="meta-value">{node.panel_username || "—"}</div>
                </div>
              </div>

              <div className="node-actions">
                <button className="primary" onClick={() => onTest(node.id)}>Test</button>
                <Link to={`/nodes/${node.id}/inbounds`} className="link-button">Inbounds</Link>
                <button className="secondary" onClick={() => openEdit(node)}>Edit</button>
                <button className="warning" onClick={() => onRestart(node.id)}>Restart Xray</button>
                <button className="danger" onClick={() => onReboot(node.id)}>Reboot</button>
                <button className="danger ghost" onClick={() => onDelete(node)}>Delete</button>
              </div>
            </div>
          );
        })}
      </div>

      {editModal.open && editModal.node && (
        <div className="modal">
          <div className="modal-content">
            <h3>Edit Node</h3>
            <form className="form-grid" onSubmit={onUpdate}>
              <input name="name" placeholder="Name" defaultValue={editModal.node.name} />
              <input name="tags" placeholder="Tags (comma)" defaultValue={(editModal.node.tags || []).join(", ")} />
              <input name="base_url" placeholder="Base URL" defaultValue={editModal.node.base_url} />
              <input name="panel_username" placeholder="Panel Username" defaultValue={editModal.node.panel_username} />
              <input name="panel_password" placeholder="Panel Password (leave blank to keep)" type="password" />
              <input name="ssh_host" placeholder="SSH Host" defaultValue={editModal.node.ssh_host} />
              <input name="ssh_port" placeholder="SSH Port" type="number" defaultValue={editModal.node.ssh_port} />
              <input name="ssh_user" placeholder="SSH User" defaultValue={editModal.node.ssh_user} />
              <textarea name="ssh_key" placeholder="SSH Private Key (leave blank to keep)" rows="3" />
              <label className="checkbox">
                <input name="verify_tls" type="checkbox" defaultChecked={editModal.node.verify_tls} />
                Verify TLS
              </label>
              <div className="actions">
                <button type="button" onClick={() => setEditModal({ open: false, node: null })}>Cancel</button>
                <button type="submit">Save</button>
              </div>
            </form>
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
