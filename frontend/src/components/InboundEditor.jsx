import React, { useEffect, useMemo, useState } from "react";

const DEFAULT_CLIENT = {
  email: "",
  id: "",
  enable: true,
  flow: "xtls-rprx-vision",
  expiryTime: 0,
  totalGB: 0,
  limitIp: 0,
  subId: "",
  tgId: "",
};

function safeParseJSON(value, fallback = {}) {
  if (!value) return fallback;
  if (typeof value === "object") return value;
  try {
    return JSON.parse(value);
  } catch {
    return fallback;
  }
}

function toArray(value) {
  if (!value) return [];
  if (Array.isArray(value)) return value;
  return [value];
}

function formatDateTime(epochMs) {
  if (!epochMs) return "";
  const date = new Date(epochMs);
  const pad = (n) => `${n}`.padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function parseDateTime(value) {
  if (!value) return 0;
  const ts = Date.parse(value);
  if (Number.isNaN(ts)) return 0;
  return ts;
}

function bytesToGB(bytes) {
  if (!bytes) return 0;
  return Math.round((bytes / (1024 * 1024 * 1024)) * 100) / 100;
}

function gbToBytes(gb) {
  if (!gb) return 0;
  return Math.round(Number(gb) * 1024 * 1024 * 1024);
}

function generateUUID() {
  if (crypto?.randomUUID) return crypto.randomUUID();
  const buf = new Uint8Array(16);
  crypto.getRandomValues(buf);
  buf[6] = (buf[6] & 0x0f) | 0x40;
  buf[8] = (buf[8] & 0x3f) | 0x80;
  const hex = Array.from(buf).map((b) => b.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function ListEditor({ label, values, onChange, placeholder }) {
  const [value, setValue] = useState("");

  useEffect(() => {
    setValue("");
  }, [values]);

  return (
    <div className="list-editor">
      <div className="list-label">{label}</div>
      <div className="chips">
        {values.map((item, idx) => (
          <span className="chip" key={`${item}-${idx}`}>
            {item}
            <button type="button" onClick={() => onChange(values.filter((_, i) => i !== idx))}>
              ×
            </button>
          </span>
        ))}
      </div>
      <div className="list-input">
        <input autoComplete="off" value={value} placeholder={placeholder} onChange={(e) => setValue(e.target.value)} />
        <button type="button" onClick={() => {
          if (!value.trim()) return;
          onChange([...values, value.trim()]);
          setValue("");
        }}>Add</button>
      </div>
    </div>
  );
}

export default function InboundEditor({ open, mode, inbound, onClose, onSave }) {
  const [tab, setTab] = useState("basic");
  const [base, setBase] = useState({ remark: "", enable: true, port: 0, protocol: "vless" });
  const [clients, setClients] = useState([]);
  const [clientSearch, setClientSearch] = useState("");
  const [clientPage, setClientPage] = useState(1);
  const [clientPageSize, setClientPageSize] = useState(10);
  const [settingsRaw, setSettingsRaw] = useState({});
  const [streamRaw, setStreamRaw] = useState({});
  const [sniffing, setSniffing] = useState({ enabled: false, destOverride: [] });
  const [transport, setTransport] = useState({
    network: "tcp",
    security: "none",
    tcpHeaderType: "none",
    wsPath: "",
    wsHeadersText: "{}",
    grpcServiceName: "",
  });
  const [security, setSecurity] = useState({
    tlsServerName: "",
    tlsALPN: [],
    tlsAllowInsecure: false,
    realityDest: "",
    realityXver: 0,
    realityServerNames: [],
    realityPrivateKey: "",
    realityShortIds: [],
    realitySpiderX: "",
    realityFingerprint: "",
    realityALPN: [],
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
    setClients(Array.isArray(settingsObj.clients) ? settingsObj.clients.map((c) => ({ ...DEFAULT_CLIENT, ...c, _localId: c._localId || generateUUID() })) : []);
    setSniffing({
      enabled: settingsObj.sniffing?.enabled || false,
      destOverride: toArray(settingsObj.sniffing?.destOverride || []),
    });
    setTransport({
      network: streamObj.network || "tcp",
      security: streamObj.security || "none",
      tcpHeaderType: streamObj.tcpSettings?.header?.type || "none",
      wsPath: streamObj.wsSettings?.path || "",
      wsHeadersText: JSON.stringify(streamObj.wsSettings?.headers || {}, null, 2),
      grpcServiceName: streamObj.grpcSettings?.serviceName || "",
    });
    setSecurity({
      tlsServerName: streamObj.tlsSettings?.serverName || "",
      tlsALPN: toArray(streamObj.tlsSettings?.alpn || []),
      tlsAllowInsecure: streamObj.tlsSettings?.allowInsecure || false,
      realityDest: streamObj.realitySettings?.dest || "",
      realityXver: streamObj.realitySettings?.xver || 0,
      realityServerNames: toArray(streamObj.realitySettings?.serverNames || []),
      realityPrivateKey: streamObj.realitySettings?.privateKey || "",
      realityShortIds: toArray(streamObj.realitySettings?.shortIds || []),
      realitySpiderX: streamObj.realitySettings?.spiderX || "",
      realityFingerprint: streamObj.realitySettings?.fingerprint || "",
      realityALPN: toArray(streamObj.realitySettings?.alpn || []),
    });
    setAdvancedDirty(false);
    setError("");
    setTab("basic");
    setClientSearch("");
    setClientPage(1);
  }, [open, inbound]);

  const builtPatch = useMemo(() => buildInboundPatch(base, clients, settingsRaw, streamRaw, sniffing, transport, security), [
    base,
    clients,
    settingsRaw,
    streamRaw,
    sniffing,
    transport,
    security,
  ]);

  useEffect(() => {
    if (!open || advancedDirty) return;
    setAdvancedJson(JSON.stringify(builtPatch, null, 2));
  }, [open, advancedDirty, builtPatch]);

  const filteredClients = useMemo(() => {
    const term = clientSearch.trim().toLowerCase();
    if (!term) return clients;
    return clients.filter((c) => {
      const email = (c.email || "").toLowerCase();
      const id = (c.id || "").toLowerCase();
      return email.includes(term) || id.includes(term);
    });
  }, [clients, clientSearch]);

  const totalPages = Math.max(1, Math.ceil(filteredClients.length / clientPageSize));

  useEffect(() => {
    if (clientPage > totalPages) {
      setClientPage(totalPages);
    }
  }, [clientPage, totalPages]);

  const paginatedClients = useMemo(() => {
    const start = (clientPage - 1) * clientPageSize;
    return filteredClients.slice(start, start + clientPageSize);
  }, [filteredClients, clientPage, clientPageSize]);

  function updateClient(idx, field, value) {
    setClients((prev) => prev.map((c, i) => i === idx ? { ...c, [field]: value } : c));
  }

  function addClient() {
    setClients((prev) => [...prev, { ...DEFAULT_CLIENT, id: generateUUID(), _localId: generateUUID() }]);
    setClientPage(1);
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
    if (!builtPatch) {
      setError("Invalid stream settings");
      return;
    }
    onSave(builtPatch);
  }

  function handleReparse() {
    try {
      const payload = JSON.parse(advancedJson || "{}");
      const settingsObj = safeParseJSON(payload.settings, settingsRaw);
      const streamObj = safeParseJSON(payload.streamSettings, streamRaw);
      setSettingsRaw(settingsObj);
      setStreamRaw(streamObj);
      setBase({
        remark: payload.remark ?? base.remark,
        enable: payload.enable ?? base.enable,
        port: payload.port ?? base.port,
        protocol: payload.protocol ?? base.protocol,
      });
      setClients(Array.isArray(settingsObj.clients) ? settingsObj.clients.map((c) => ({ ...DEFAULT_CLIENT, ...c })) : []);
      setSniffing({
        enabled: settingsObj.sniffing?.enabled || false,
        destOverride: toArray(settingsObj.sniffing?.destOverride || []),
      });
      setTransport({
        network: streamObj.network || "tcp",
        security: streamObj.security || "none",
        tcpHeaderType: streamObj.tcpSettings?.header?.type || "none",
        wsPath: streamObj.wsSettings?.path || "",
        wsHeadersText: JSON.stringify(streamObj.wsSettings?.headers || {}, null, 2),
        grpcServiceName: streamObj.grpcSettings?.serviceName || "",
      });
      setSecurity({
        tlsServerName: streamObj.tlsSettings?.serverName || "",
        tlsALPN: toArray(streamObj.tlsSettings?.alpn || []),
        tlsAllowInsecure: streamObj.tlsSettings?.allowInsecure || false,
        realityDest: streamObj.realitySettings?.dest || "",
        realityXver: streamObj.realitySettings?.xver || 0,
        realityServerNames: toArray(streamObj.realitySettings?.serverNames || []),
        realityPrivateKey: streamObj.realitySettings?.privateKey || "",
        realityShortIds: toArray(streamObj.realitySettings?.shortIds || []),
        realitySpiderX: streamObj.realitySettings?.spiderX || "",
        realityFingerprint: streamObj.realitySettings?.fingerprint || "",
        realityALPN: toArray(streamObj.realitySettings?.alpn || []),
      });
      setAdvancedDirty(false);
    } catch {
      setError("Invalid JSON to re-parse");
    }
  }

  if (!open) return null;

  return (
    <div className="modal">
      <div className="modal-content wide">
        <header className="modal-header">
          <h3>{mode === "add" ? "Add inbound" : "Edit inbound"}</h3>
          <div className="tabs">
            {["basic", "clients", "transport", "security", "sniffing", "advanced"].map((t) => (
              <button key={t} className={tab === t ? "tab active" : "tab"} onClick={() => setTab(t)} type="button">
                {t === "basic" && "Basic"}
                {t === "clients" && "Clients"}
                {t === "transport" && "Transport"}
                {t === "security" && "Security"}
                {t === "sniffing" && "Sniffing"}
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
              <input autoComplete="off" value={base.remark} onChange={(e) => setBase({ ...base, remark: e.target.value })} />
            </label>
            <label className="checkbox">
              <input autoComplete="off" type="checkbox" checked={base.enable} onChange={(e) => setBase({ ...base, enable: e.target.checked })} />
              Enable
            </label>
            <label>
              Port
              <input autoComplete="off" type="number" value={base.port} onChange={(e) => setBase({ ...base, port: Number(e.target.value) })} />
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
            <div className="clients-toolbar">
              <input
                placeholder="Search by email or UUID"
                value={clientSearch}
                onChange={(e) => { setClientSearch(e.target.value); setClientPage(1); }}
              />
              <div className="pagination">
                <button type="button" disabled={clientPage <= 1} onClick={() => setClientPage((p) => Math.max(1, p - 1))}>Prev</button>
                <span>Page {clientPage} / {totalPages}</span>
                <button type="button" disabled={clientPage >= totalPages} onClick={() => setClientPage((p) => Math.min(totalPages, p + 1))}>Next</button>
                <select value={clientPageSize} onChange={(e) => { setClientPageSize(Number(e.target.value)); setClientPage(1); }}>
                  {[5, 10, 20, 50].map((n) => <option key={n} value={n}>{n}/page</option>)}
                </select>
              </div>
            </div>
            <div className="clients-table-desktop">
              <div className="table compact clients-table">
                <div className="table-row head">
                  <div>Email</div>
                  <div>UUID</div>
                  <div>Enable</div>
                  <div>Flow</div>
                  <div>Expiry</div>
                  <div>Total (GB)</div>
                  <div>Limit IP</div>
                  <div>Actions</div>
                </div>
                {paginatedClients.map((client, idx) => {
                  const globalIdx = (clientPage - 1) * clientPageSize + idx;
                  return (
                    <div className="table-row" key={client._localId || `${client.email}-${globalIdx}`}>
                      <div data-label="Email">
                        <input autoComplete="off" value={client.email || ""} onChange={(e) => updateClient(globalIdx, "email", e.target.value)} />
                        <div className="hint">subId/tgId are kept if present</div>
                      </div>
                      <div data-label="UUID">
                        <input autoComplete="off" value={client.id || ""} onChange={(e) => updateClient(globalIdx, "id", e.target.value)} />
                        <button type="button" onClick={() => updateClient(globalIdx, "id", generateUUID())}>Gen</button>
                      </div>
                      <div data-label="Enable">
                        <input autoComplete="off" type="checkbox" checked={client.enable ?? true} onChange={(e) => updateClient(globalIdx, "enable", e.target.checked)} />
                      </div>
                      <div data-label="Flow">
                        <input autoComplete="off" value={client.flow || ""} onChange={(e) => updateClient(globalIdx, "flow", e.target.value)} />
                      </div>
                      <div data-label="Expiry">
                        <input autoComplete="off" type="datetime-local" value={formatDateTime(client.expiryTime)} onChange={(e) => updateClient(globalIdx, "expiryTime", parseDateTime(e.target.value))} />
                      </div>
                      <div data-label="Total (GB)">
                        <input autoComplete="off" type="number" value={bytesToGB(client.totalGB)} onChange={(e) => updateClient(globalIdx, "totalGB", gbToBytes(e.target.value))} />
                      </div>
                      <div data-label="Limit IP">
                        <input autoComplete="off" type="number" value={client.limitIp || 0} onChange={(e) => updateClient(globalIdx, "limitIp", Number(e.target.value))} />
                      </div>
                      <div data-label="Actions">
                        <button className="danger" type="button" onClick={() => removeClient(globalIdx)}>Remove</button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
            <div className="clients-cards-mobile">
              {paginatedClients.map((client, idx) => {
                const globalIdx = (clientPage - 1) * clientPageSize + idx;
                return (
                  <div className="client-card" key={client._localId || `${client.email}-${globalIdx}`}>
                    <label>
                      <span className="field-label">Email</span>
                      <input autoComplete="off" value={client.email || ""} onChange={(e) => updateClient(globalIdx, "email", e.target.value)} />
                      <div className="hint">subId/tgId are kept if present</div>
                    </label>
                    <label>
                      <span className="field-label">UUID</span>
                      <div className="row">
                        <input autoComplete="off" value={client.id || ""} onChange={(e) => updateClient(globalIdx, "id", e.target.value)} />
                        <button type="button" onClick={() => updateClient(globalIdx, "id", generateUUID())}>Gen</button>
                      </div>
                    </label>
                    <label className="checkbox">
                      <input autoComplete="off" type="checkbox" checked={client.enable ?? true} onChange={(e) => updateClient(globalIdx, "enable", e.target.checked)} />
                      Enable
                    </label>
                    <label>
                      <span className="field-label">Flow</span>
                      <input autoComplete="off" value={client.flow || ""} onChange={(e) => updateClient(globalIdx, "flow", e.target.value)} />
                    </label>
                    <label>
                      <span className="field-label">Expiry</span>
                      <input autoComplete="off" type="datetime-local" value={formatDateTime(client.expiryTime)} onChange={(e) => updateClient(globalIdx, "expiryTime", parseDateTime(e.target.value))} />
                    </label>
                    <label>
                      <span className="field-label">Total (GB)</span>
                      <input autoComplete="off" type="number" value={bytesToGB(client.totalGB)} onChange={(e) => updateClient(globalIdx, "totalGB", gbToBytes(e.target.value))} />
                    </label>
                    <label>
                      <span className="field-label">Limit IP</span>
                      <input autoComplete="off" type="number" value={client.limitIp || 0} onChange={(e) => updateClient(globalIdx, "limitIp", Number(e.target.value))} />
                    </label>
                    <div className="actions">
                      <button className="danger" type="button" onClick={() => removeClient(globalIdx)}>Remove</button>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )}

        {tab === "transport" && (
          <div className="grid-2">
            <label>
              Network
              <select value={transport.network} onChange={(e) => setTransport({ ...transport, network: e.target.value })}>
                <option value="tcp">tcp</option>
                <option value="ws">ws</option>
                <option value="grpc">grpc</option>
              </select>
            </label>
            <label>
              TCP Header Type
              <select value={transport.tcpHeaderType} onChange={(e) => setTransport({ ...transport, tcpHeaderType: e.target.value })}>
                <option value="none">none</option>
                <option value="http">http</option>
              </select>
            </label>
            <label>
              WS Path
              <input autoComplete="off" value={transport.wsPath} onChange={(e) => setTransport({ ...transport, wsPath: e.target.value })} />
            </label>
            <label>
              WS Headers (JSON)
              <textarea autoComplete="off" rows="3" value={transport.wsHeadersText} onChange={(e) => setTransport({ ...transport, wsHeadersText: e.target.value })} />
            </label>
            <label>
              gRPC Service Name
              <input autoComplete="off" value={transport.grpcServiceName} onChange={(e) => setTransport({ ...transport, grpcServiceName: e.target.value })} />
            </label>
          </div>
        )}

        {tab === "security" && (
          <div className="grid-2">
            <label>
              Security
              <select value={transport.security} onChange={(e) => setTransport({ ...transport, security: e.target.value })}>
                <option value="none">none</option>
                <option value="tls">tls</option>
                <option value="reality">reality</option>
              </select>
            </label>
            <label>
              TLS Server Name
              <input autoComplete="off" value={security.tlsServerName} onChange={(e) => setSecurity({ ...security, tlsServerName: e.target.value })} />
            </label>
            <ListEditor
              label="TLS ALPN"
              values={security.tlsALPN}
              placeholder="h2"
              onChange={(values) => setSecurity({ ...security, tlsALPN: values })}
            />
            <label className="checkbox">
              <input autoComplete="off" type="checkbox" checked={security.tlsAllowInsecure} onChange={(e) => setSecurity({ ...security, tlsAllowInsecure: e.target.checked })} />
              TLS Allow Insecure
            </label>
            <label>
              Reality Dest (host:port)
              <input autoComplete="off" value={security.realityDest} onChange={(e) => setSecurity({ ...security, realityDest: e.target.value })} />
            </label>
            <label>
              Reality Xver
              <input autoComplete="off" type="number" value={security.realityXver} onChange={(e) => setSecurity({ ...security, realityXver: Number(e.target.value) })} />
            </label>
            <ListEditor
              label="Reality Server Names"
              values={security.realityServerNames}
              placeholder="example.com"
              onChange={(values) => setSecurity({ ...security, realityServerNames: values })}
            />
            <label>
              Reality Private Key
              <input autoComplete="off" value={security.realityPrivateKey} onChange={(e) => setSecurity({ ...security, realityPrivateKey: e.target.value })} />
            </label>
            <ListEditor
              label="Reality Short IDs"
              values={security.realityShortIds}
              placeholder="a1b2c3"
              onChange={(values) => setSecurity({ ...security, realityShortIds: values })}
            />
            <label>
              Reality SpiderX
              <input autoComplete="off" value={security.realitySpiderX} onChange={(e) => setSecurity({ ...security, realitySpiderX: e.target.value })} />
            </label>
            <label>
              Reality Fingerprint
              <input autoComplete="off" value={security.realityFingerprint} onChange={(e) => setSecurity({ ...security, realityFingerprint: e.target.value })} />
            </label>
            <ListEditor
              label="Reality ALPN"
              values={security.realityALPN}
              placeholder="h2"
              onChange={(values) => setSecurity({ ...security, realityALPN: values })}
            />
          </div>
        )}

        {tab === "sniffing" && (
          <div className="grid-2">
            <label className="checkbox">
              <input autoComplete="off" type="checkbox" checked={sniffing.enabled} onChange={(e) => setSniffing({ ...sniffing, enabled: e.target.checked })} />
              Sniffing enabled
            </label>
            <ListEditor
              label="Dest Override"
              values={sniffing.destOverride}
              placeholder="http"
              onChange={(values) => setSniffing({ ...sniffing, destOverride: values })}
            />
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
            <div className="actions">
              <button type="button" onClick={handleReparse}>Re-parse to form</button>
            </div>
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

function buildInboundPatch(base, clients, settingsRaw, streamRaw, sniffing, transport, security) {
  let wsHeaders = {};
  if (transport.wsHeadersText && transport.wsHeadersText.trim() !== "") {
    try {
      wsHeaders = JSON.parse(transport.wsHeadersText);
    } catch {
      return null;
    }
  }

  const normalizedClients = (clients || []).map((c) => ({
    ...c,
    enable: c.enable !== undefined ? c.enable : true,
    expiryTime: Number(c.expiryTime || 0),
    totalGB: Number(c.totalGB || 0),
    limitIp: Number(c.limitIp || 0),
  }));

  const settingsNext = {
    ...settingsRaw,
    clients: normalizedClients,
    sniffing: {
      ...(settingsRaw.sniffing || {}),
      enabled: sniffing.enabled,
      destOverride: sniffing.destOverride,
    },
  };

  const streamNext = { ...streamRaw };
  streamNext.network = transport.network;
  streamNext.security = transport.security;
  streamNext.tcpSettings = {
    ...(streamNext.tcpSettings || {}),
    header: {
      ...((streamNext.tcpSettings || {}).header || {}),
      type: transport.tcpHeaderType,
    },
  };
  streamNext.wsSettings = {
    ...(streamNext.wsSettings || {}),
    path: transport.wsPath || "",
    headers: wsHeaders,
  };
  streamNext.grpcSettings = {
    ...(streamNext.grpcSettings || {}),
    serviceName: transport.grpcServiceName || "",
  };
  streamNext.tlsSettings = {
    ...(streamNext.tlsSettings || {}),
    serverName: security.tlsServerName || "",
    alpn: security.tlsALPN,
    allowInsecure: security.tlsAllowInsecure,
  };
  streamNext.realitySettings = {
    ...(streamNext.realitySettings || {}),
    dest: security.realityDest || "",
    xver: Number(security.realityXver || 0),
    serverNames: security.realityServerNames,
    privateKey: security.realityPrivateKey || "",
    shortIds: security.realityShortIds,
    spiderX: security.realitySpiderX || "",
    fingerprint: security.realityFingerprint || "",
    alpn: security.realityALPN,
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

