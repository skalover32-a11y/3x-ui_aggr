import React, { useEffect, useMemo, useState } from "react";
import { Routes, Route, useNavigate, useLocation, useParams, Link } from "react-router-dom";
import { request, setToken, getToken, convertSSHKey } from "./api.js";

function safeParseJSON(value, fallback = {}) {
  if (!value) return fallback;
  if (typeof value === "object") return value;
  try {
    return JSON.parse(value);
  } catch {
    return fallback;
  }
}

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

function StatusBadge({ status }) {
  const label = status || "unknown";
  return <span className={`badge ${label}`}>{label}</span>;
}

function Sparkline({ points }) {
  if (!points || points.length === 0) return <div className="sparkline empty">no data</div>;
  const width = 140;
  const height = 36;
  const maxLatency = Math.max(...points.map((p) => p.latency_ms || 0), 1);
  const step = points.length > 1 ? width / (points.length - 1) : width;
  const poly = points.map((p, idx) => {
    const x = idx * step;
    const y = height - (p.latency_ms || 0) / maxLatency * (height - 6) - 3;
    return `${x},${Math.max(3, Math.min(height - 3, y))}`;
  }).join(" ");

  return (
    <svg className="sparkline" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
      <polyline points={poly} fill="none" stroke="#60a5fa" strokeWidth="2" />
      {points.map((p, idx) => {
        const x = idx * step;
        const y = height - (p.latency_ms || 0) / maxLatency * (height - 6) - 3;
        const status = deriveStatus(p.panel_ok, p.ssh_ok);
        return (
          <circle key={`${p.ts}-${idx}`} cx={x} cy={Math.max(3, Math.min(height - 3, y))} r="2.5" className={`dot ${status}`}>
            <title>{`${formatTS(p.ts)} | latency ${p.latency_ms || 0}ms${p.error ? ` | ${p.error}` : ""}`}</title>
          </circle>
        );
      })}
    </svg>
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
        const statusNext = {};
        const uptimeNext = {};
        nodes.forEach((node, idx) => {
          statusNext[node.id] = statusEntries[idx];
          uptimeNext[node.id] = uptimeEntries[idx] || [];
        });
        setStatusMap(statusNext);
        setUptimeMap(uptimeNext);
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

      <div className="table nodes">
        <div className="table-row head">
          <div>Name</div>
          <div>Tags</div>
          <div>Base URL</div>
          <div>Status</div>
          <div>Availability</div>
          <div>Actions</div>
        </div>
        {nodes.map((node) => (
          <div className="table-row" key={node.id}>
            <div>{node.name}</div>
            <div>{(node.tags || []).join(", ")}</div>
            <div>{node.base_url}</div>
            <div>
              <StatusBadge status={statusMap[node.id]?.status} />
            </div>
            <div>
              <Sparkline points={uptimeMap[node.id]} />
            </div>
            <div className="actions">
              <button onClick={() => onTest(node.id)}>Test</button>
              <Link to={`/nodes/${node.id}/inbounds`} className="link-button">Inbounds</Link>
              <button onClick={() => openEdit(node)}>Edit</button>
              <button className="danger" onClick={() => onDelete(node)}>Delete</button>
              <button onClick={() => onRestart(node.id)}>Restart Xray</button>
              <button className="danger" onClick={() => onReboot(node.id)}>Reboot</button>
            </div>
          </div>
        ))}
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

      <InboundEditorModal
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

function InboundEditorModal({ open, mode, inbound, onClose, onSave }) {
  const [tab, setTab] = useState("basic");
  const [base, setBase] = useState({ remark: "", enable: true, port: 0, protocol: "vless" });
  const [clients, setClients] = useState([]);
  const [settingsRaw, setSettingsRaw] = useState({});
  const [streamRaw, setStreamRaw] = useState({});
  const [streamFields, setStreamFields] = useState({
    network: "tcp",
    security: "none",
    wsPath: "",
    wsHeadersText: "{}",
    grpcServiceName: "",
    tlsServerName: "",
    realityServerName: "",
    realityPublicKey: "",
    realityShortId: "",
    realitySpiderX: "",
  });
  const [advancedJson, setAdvancedJson] = useState("");
  const [advancedDirty, setAdvancedDirty] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    const rawInbound = inbound || {};
    const settingsObj = safeParseJSON(rawInbound.settings, {});
    const streamObj = safeParseJSON(rawInbound.streamSettings, {});
    setSettingsRaw(settingsObj);
    setStreamRaw(streamObj);
    setBase({
      remark: rawInbound.remark || "",
      enable: rawInbound.enable !== undefined ? rawInbound.enable : true,
      port: rawInbound.port || 0,
      protocol: rawInbound.protocol || "vless",
    });
    setClients(Array.isArray(settingsObj.clients) ? settingsObj.clients.map((c) => ({ ...c })) : []);
    setStreamFields({
      network: streamObj.network || "tcp",
      security: streamObj.security || "none",
      wsPath: streamObj.wsSettings?.path || "",
      wsHeadersText: JSON.stringify(streamObj.wsSettings?.headers || {}, null, 2),
      grpcServiceName: streamObj.grpcSettings?.serviceName || "",
      tlsServerName: streamObj.tlsSettings?.serverName || "",
      realityServerName: streamObj.realitySettings?.serverName || "",
      realityPublicKey: streamObj.realitySettings?.publicKey || "",
      realityShortId: streamObj.realitySettings?.shortId || "",
      realitySpiderX: streamObj.realitySettings?.spiderX || "",
    });
    setTab("basic");
    setError("");
    setAdvancedDirty(false);
  }, [open, inbound]);

  useEffect(() => {
    if (!open || advancedDirty) return;
    const patch = buildInboundPatch(base, clients, settingsRaw, streamRaw, streamFields);
    setAdvancedJson(JSON.stringify(patch, null, 2));
  }, [open, base, clients, settingsRaw, streamRaw, streamFields, advancedDirty]);

  function updateClient(idx, field, value) {
    setClients((prev) => prev.map((c, i) => i === idx ? { ...c, [field]: value } : c));
  }

  function addClient() {
    setClients((prev) => [...prev, { email: "", id: "", enable: true, expiryTime: 0, totalGB: 0, limitIp: 0 }]);
  }

  function removeClient(idx) {
    setClients((prev) => prev.filter((_, i) => i !== idx));
  }

  function handleSave() {
    setError("");
    if (advancedDirty) {
      try {
        const payload = JSON.parse(advancedJson || "{}");
        onSave(payload);
      } catch {
        setError("Invalid JSON in Advanced tab");
      }
      return;
    }
    const payload = buildInboundPatch(base, clients, settingsRaw, streamRaw, streamFields);
    if (!payload) {
      setError("Invalid stream settings");
      return;
    }
    onSave(payload);
  }

  if (!open) return null;

  return (
    <div className="modal">
      <div className="modal-content wide">
        <header className="modal-header">
          <h3>{mode === "add" ? "Add inbound" : "Edit inbound"}</h3>
          <div className="tabs">
            {["basic", "clients", "stream", "advanced"].map((t) => (
              <button key={t} className={tab === t ? "tab active" : "tab"} onClick={() => setTab(t)} type="button">
                {t === "basic" && "Basic"}
                {t === "clients" && "Clients"}
                {t === "stream" && "Transport"}
                {t === "advanced" && "Advanced JSON"}
              </button>
            ))}
          </div>
        </header>

        {error && <div className="error">{error}</div>}

        {tab === "basic" && (
          <div className="grid-2">
            <label>
              Remark
              <input value={base.remark} onChange={(e) => setBase({ ...base, remark: e.target.value })} />
            </label>
            <label className="checkbox">
              <input type="checkbox" checked={base.enable} onChange={(e) => setBase({ ...base, enable: e.target.checked })} />
              Enable
            </label>
            <label>
              Port
              <input type="number" value={base.port} onChange={(e) => setBase({ ...base, port: Number(e.target.value) })} />
            </label>
            <label>
              Protocol
              <select value={base.protocol} onChange={(e) => setBase({ ...base, protocol: e.target.value })} disabled={mode === "edit"}>
                <option value="vless">vless</option>
                <option value="vmess">vmess</option>
                <option value="trojan">trojan</option>
              </select>
            </label>
          </div>
        )}

        {tab === "clients" && (
          <div className="clients">
            <div className="actions">
              <button type="button" onClick={addClient}>Add client</button>
            </div>
            <div className="table compact">
              <div className="table-row head">
                <div>Email</div>
                <div>UUID</div>
                <div>Enable</div>
                <div>Expiry</div>
                <div>Total GB</div>
                <div>Limit IP</div>
                <div>Actions</div>
              </div>
              {clients.map((client, idx) => (
                <div className="table-row" key={`${client.email}-${idx}`}>
                  <div>
                    <input value={client.email || ""} onChange={(e) => updateClient(idx, "email", e.target.value)} />
                  </div>
                  <div>
                    <input value={client.id || ""} onChange={(e) => updateClient(idx, "id", e.target.value)} />
                  </div>
                  <div>
                    <input type="checkbox" checked={client.enable ?? true} onChange={(e) => updateClient(idx, "enable", e.target.checked)} />
                  </div>
                  <div>
                    <input type="number" value={client.expiryTime || 0} onChange={(e) => updateClient(idx, "expiryTime", Number(e.target.value))} />
                  </div>
                  <div>
                    <input type="number" value={client.totalGB || 0} onChange={(e) => updateClient(idx, "totalGB", Number(e.target.value))} />
                  </div>
                  <div>
                    <input type="number" value={client.limitIp || 0} onChange={(e) => updateClient(idx, "limitIp", Number(e.target.value))} />
                  </div>
                  <div>
                    <button className="danger" type="button" onClick={() => removeClient(idx)}>Remove</button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {tab === "stream" && (
          <div className="grid-2">
            <label>
              Network
              <select value={streamFields.network} onChange={(e) => setStreamFields({ ...streamFields, network: e.target.value })}>
                <option value="tcp">tcp</option>
                <option value="ws">ws</option>
                <option value="grpc">grpc</option>
              </select>
            </label>
            <label>
              Security
              <select value={streamFields.security} onChange={(e) => setStreamFields({ ...streamFields, security: e.target.value })}>
                <option value="none">none</option>
                <option value="tls">tls</option>
                <option value="reality">reality</option>
              </select>
            </label>
            <label>
              WS Path
              <input value={streamFields.wsPath} onChange={(e) => setStreamFields({ ...streamFields, wsPath: e.target.value })} />
            </label>
            <label>
              WS Headers (JSON)
              <textarea rows="3" value={streamFields.wsHeadersText} onChange={(e) => setStreamFields({ ...streamFields, wsHeadersText: e.target.value })} />
            </label>
            <label>
              gRPC Service Name
              <input value={streamFields.grpcServiceName} onChange={(e) => setStreamFields({ ...streamFields, grpcServiceName: e.target.value })} />
            </label>
            <label>
              TLS/Reality Server Name
              <input value={streamFields.tlsServerName} onChange={(e) => setStreamFields({ ...streamFields, tlsServerName: e.target.value })} />
            </label>
            <label>
              Reality Server Name
              <input value={streamFields.realityServerName} onChange={(e) => setStreamFields({ ...streamFields, realityServerName: e.target.value })} />
            </label>
            <label>
              Reality Public Key
              <input value={streamFields.realityPublicKey} onChange={(e) => setStreamFields({ ...streamFields, realityPublicKey: e.target.value })} />
            </label>
            <label>
              Reality Short ID
              <input value={streamFields.realityShortId} onChange={(e) => setStreamFields({ ...streamFields, realityShortId: e.target.value })} />
            </label>
            <label>
              Reality SpiderX
              <input value={streamFields.realitySpiderX} onChange={(e) => setStreamFields({ ...streamFields, realitySpiderX: e.target.value })} />
            </label>
          </div>
        )}

        {tab === "advanced" && (
          <div>
            <textarea
              rows="18"
              value={advancedJson}
              onChange={(e) => {
                setAdvancedDirty(true);
                setAdvancedJson(e.target.value);
              }}
            />
            <div className="hint">Advanced JSON is sent as patch and overrides the form.</div>
          </div>
        )}

        <div className="actions">
          <button type="button" onClick={onClose}>Cancel</button>
          <button type="button" onClick={handleSave}>Save</button>
        </div>
      </div>
    </div>
  );
}

function buildInboundPatch(base, clients, settingsRaw, streamRaw, streamFields) {
  const normalizedClients = (clients || []).map((c) => ({
    ...c,
    enable: c.enable !== undefined ? c.enable : true,
    expiryTime: Number(c.expiryTime || 0),
    totalGB: Number(c.totalGB || 0),
    limitIp: Number(c.limitIp || 0),
  }));

  let wsHeaders = {};
  if (streamFields.wsHeadersText && streamFields.wsHeadersText.trim() !== "") {
    try {
      wsHeaders = JSON.parse(streamFields.wsHeadersText);
    } catch {
      return null;
    }
  }

  const settingsNext = { ...settingsRaw, clients: normalizedClients };
  const streamNext = { ...streamRaw };
  streamNext.network = streamFields.network;
  streamNext.security = streamFields.security;
  streamNext.wsSettings = {
    ...(streamNext.wsSettings || {}),
    path: streamFields.wsPath || "",
    headers: wsHeaders,
  };
  streamNext.grpcSettings = {
    ...(streamNext.grpcSettings || {}),
    serviceName: streamFields.grpcServiceName || "",
  };
  streamNext.tlsSettings = {
    ...(streamNext.tlsSettings || {}),
    serverName: streamFields.tlsServerName || "",
  };
  streamNext.realitySettings = {
    ...(streamNext.realitySettings || {}),
    serverName: streamFields.realityServerName || "",
    publicKey: streamFields.realityPublicKey || "",
    shortId: streamFields.realityShortId || "",
    spiderX: streamFields.realitySpiderX || "",
  };

  return {
    remark: base.remark,
    enable: base.enable,
    port: Number(base.port || 0),
    protocol: base.protocol,
    settings: settingsNext,
    streamSettings: streamNext,
  };
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
