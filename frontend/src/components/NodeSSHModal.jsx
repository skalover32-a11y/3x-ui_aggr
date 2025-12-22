import { useEffect, useRef, useState } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import { getToken } from "../api.js";

function buildWsUrl(path) {
  const base = import.meta.env.VITE_API_BASE || "/api";
  const baseUrl = new URL(base, window.location.origin);
  const wsProto = window.location.protocol === "https:" ? "wss:" : "ws:";
  baseUrl.protocol = wsProto;
  const basePath = baseUrl.pathname.replace(/\/$/, "");
  baseUrl.pathname = `${basePath}${path}`;
  return baseUrl;
}

export default function NodeSSHModal({ open, node, onClose }) {
  const containerRef = useRef(null);
  const termRef = useRef(null);
  const wsRef = useRef(null);
  const fitRef = useRef(null);
  const [status, setStatus] = useState("disconnected");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open || !node) return;
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      theme: {
        background: "#0b1220",
      },
    });
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(containerRef.current);
    fitAddon.fit();
    termRef.current = term;
    fitRef.current = fitAddon;
    setStatus("connecting");
    setError("");

    const token = getToken();
    const url = buildWsUrl(`/nodes/${node.id}/ssh`);
    if (token) {
      url.searchParams.set("token", token);
    }
    const ws = new WebSocket(url.toString());
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    const sendResize = () => {
      if (!ws || ws.readyState !== WebSocket.OPEN) return;
      const cols = term.cols || 80;
      const rows = term.rows || 24;
      ws.send(JSON.stringify({ type: "resize", cols, rows }));
    };

    ws.onopen = () => {
      setStatus("connected");
      sendResize();
    };
    ws.onmessage = (event) => {
      if (!termRef.current) return;
      if (event.data instanceof ArrayBuffer) {
        termRef.current.write(new Uint8Array(event.data));
      } else {
        termRef.current.write(event.data);
      }
    };
    ws.onerror = () => {
      setStatus("error");
      setError("SSH connection failed");
    };
    ws.onclose = (event) => {
      setStatus("disconnected");
      if (event.reason) {
        setError(event.reason);
      }
    };

    const onData = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(data);
      }
    });

    const onResize = () => {
      fitAddon.fit();
      sendResize();
    };

    window.addEventListener("resize", onResize);

    return () => {
      window.removeEventListener("resize", onResize);
      onData.dispose();
      ws.close();
      term.dispose();
      termRef.current = null;
      wsRef.current = null;
      fitRef.current = null;
    };
  }, [open, node]);

  if (!open || !node) return null;

  return (
    <div className="modal ssh-modal">
      <div className="modal-content wide ssh-modal-content">
        <div className="modal-header">
          <h3>SSH: {node.name}</h3>
          <div className="ssh-status">
            <span className={`badge ${status}`}>{status}</span>
          </div>
        </div>
        {error && <div className="error">{error}</div>}
        <div className="terminal-container" ref={containerRef} />
        <div className="actions">
          <button type="button" onClick={onClose}>Close</button>
        </div>
      </div>
    </div>
  );
}
