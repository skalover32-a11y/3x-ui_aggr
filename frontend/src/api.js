export const API_BASE = import.meta.env.VITE_API_BASE || "/api";

export function getToken() {
  return localStorage.getItem("agg_token") || "";
}

export function setToken(token) {
  if (token) {
    localStorage.setItem("agg_token", token);
  } else {
    localStorage.removeItem("agg_token");
  }
}

export function setAuth(token, role, username, isGlobalAdmin) {
  const previousUser = getUser();
  const nextUser = (username || "").trim();
  if (previousUser && nextUser && previousUser !== nextUser) {
    // Prevent org leakage between different accounts in one browser session.
    localStorage.removeItem("agg_org_id");
    localStorage.removeItem("agg_org_role");
  }
  setToken(token);
  if (role) {
    localStorage.setItem("agg_role", role);
  }
  if (username) {
    localStorage.setItem("agg_user", username);
  }
  if (isGlobalAdmin != null) {
    localStorage.setItem("agg_is_global_admin", isGlobalAdmin ? "1" : "0");
  }
}

export function clearAuth() {
  localStorage.removeItem("agg_token");
  localStorage.removeItem("agg_role");
  localStorage.removeItem("agg_user");
  localStorage.removeItem("agg_is_global_admin");
  localStorage.removeItem("agg_org_id");
  localStorage.removeItem("agg_org_role");
}

export function getOrgId() {
  return localStorage.getItem("agg_org_id") || "";
}

export function setOrgId(orgId) {
  if (orgId) {
    localStorage.setItem("agg_org_id", orgId);
  } else {
    localStorage.removeItem("agg_org_id");
  }
}

export function getOrgRole() {
  return localStorage.getItem("agg_org_role") || "";
}

export function setOrgRole(role) {
  if (role) {
    localStorage.setItem("agg_org_role", role);
  } else {
    localStorage.removeItem("agg_org_role");
  }
}

export function getRole() {
  return localStorage.getItem("agg_role") || "viewer";
}

export function getIsGlobalAdmin() {
  return localStorage.getItem("agg_is_global_admin") === "1";
}

export function getUser() {
  return localStorage.getItem("agg_user") || "";
}

export async function refreshAuth() {
  const res = await fetch(`${API_BASE}/auth/refresh`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest" },
    credentials: "include",
  });
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const message = data?.error?.message || `Request failed: ${res.status}`;
    const err = new Error(message);
    err.data = data;
    throw err;
  }
  return data;
}

export async function request(method, path, body) {
  const headers = { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest" };
  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const orgId = getOrgId();
  if (orgId) {
    headers["X-Org-ID"] = orgId;
  }
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers,
    credentials: "include",
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const message = data?.error?.message || `Request failed: ${res.status}`;
    const err = new Error(message);
    err.data = data;
    throw err;
  }
  return data;
}

export async function convertSSHKey(file, passphrase) {
  const formData = new FormData();
  formData.append("file", file);
  if (passphrase) {
    formData.append("passphrase", passphrase);
  }
  const headers = { "X-Requested-With": "XMLHttpRequest" };
  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const res = await fetch(`${API_BASE}/utils/convert-ssh-key`, {
    method: "POST",
    headers,
    credentials: "include",
    body: formData,
  });
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const message = data?.error?.message || `Request failed: ${res.status}`;
    const err = new Error(message);
    err.data = data;
    throw err;
  }
  return data;
}

export async function getTelegramSettings() {
  const headers = { "X-Requested-With": "XMLHttpRequest" };
  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const orgId = getOrgId();
  if (orgId) {
    headers["X-Org-ID"] = orgId;
  }
  const res = await fetch(`${API_BASE}/telegram/settings`, {
    method: "GET",
    headers,
    credentials: "include",
  });
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const message = data?.error?.message || `Request failed: ${res.status}`;
    const err = new Error(message);
    err.data = data;
    throw err;
  }
  return data;
}

export async function saveTelegramSettings(payload) {
  const headers = { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest" };
  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const orgId = getOrgId();
  if (orgId) {
    headers["X-Org-ID"] = orgId;
  }
  const res = await fetch(`${API_BASE}/telegram/settings`, {
    method: "PUT",
    headers,
    credentials: "include",
    body: JSON.stringify(payload || {}),
  });
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const message = data?.error?.message || `Request failed: ${res.status}`;
    const err = new Error(message);
    err.data = data;
    throw err;
  }
  return data;
}

export async function sendTelegramTest(payload) {
  const headers = { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest" };
  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const orgId = getOrgId();
  if (orgId) {
    headers["X-Org-ID"] = orgId;
  }
  const res = await fetch(`${API_BASE}/telegram/test`, {
    method: "POST",
    headers,
    credentials: "include",
    body: JSON.stringify(payload || {}),
  });
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const message = data?.error?.message || `Request failed: ${res.status}`;
    const err = new Error(message);
    err.data = data;
    throw err;
  }
  return data;
}

export async function getPrometheusSettings() {
  return request("GET", "/prometheus/settings");
}

export async function savePrometheusSettings(payload) {
  return request("PUT", "/prometheus/settings", payload || {});
}

export async function testPrometheusConnection(payload) {
  return request("POST", "/prometheus/test", payload || {});
}

export async function queryPrometheus(payload) {
  return request("POST", "/prometheus/query", payload || {});
}
