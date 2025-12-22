const API_BASE = import.meta.env.VITE_API_BASE || "/api";

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

export function setAuth(token, role, username) {
  setToken(token);
  if (role) {
    localStorage.setItem("agg_role", role);
  }
  if (username) {
    localStorage.setItem("agg_user", username);
  }
}

export function clearAuth() {
  localStorage.removeItem("agg_token");
  localStorage.removeItem("agg_role");
  localStorage.removeItem("agg_user");
}

export function getRole() {
  return localStorage.getItem("agg_role") || "admin";
}

export async function request(method, path, body) {
  const headers = { "Content-Type": "application/json" };
  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers,
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
  const headers = {};
  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const res = await fetch(`${API_BASE}/utils/convert-ssh-key`, {
    method: "POST",
    headers,
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
  return request("GET", "/telegram/settings");
}

export async function saveTelegramSettings(payload) {
  return request("PUT", "/telegram/settings", payload);
}
