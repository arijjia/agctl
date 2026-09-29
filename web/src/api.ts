// api.ts — тонкий клиент REST API agctl.

export type Role = "user" | "admin";
export type User = { login: string; role: Role };
export type WsStatus = "provisioning" | "running" | "stopped" | "failed" | "removing" | "stopping";

export type RepoTemplate = { id: string; title: string; description: string };

export type Workspace = {
  id: string;
  name: string;
  template: string;
  status: WsStatus;
  owner: string;
  createdAt: string;
  updatedAt: string;
  url: string;
  port: number;
  error: string;
  model: string;
  baseURL: string;
};

export type AuditEvent = { ts: string; actor: string; action: string; target: string; details: string };
export type AdminUser = { id: number; login: string; role: Role; createdAt: string };

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data: any = {};
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = {};
    }
  }
  if (!res.ok) {
    throw new Error(data.error || `HTTP ${res.status}`);
  }
  return data as T;
}

export const api = {
  templates: () => req<{ templates: RepoTemplate[] }>("GET", "/api/templates"),
  register: (login: string, password: string) => req<{ user: User }>("POST", "/api/register", { login, password }),
  login: (login: string, password: string) => req<{ user: User }>("POST", "/api/login", { login, password }),
  logout: () => req<void>("POST", "/api/logout"),
  me: () => req<{ user: User }>("GET", "/api/me"),
  workspaces: () => req<{ workspaces: Workspace[] }>("GET", "/api/workspaces"),
  create: (name: string, template: string, baseURL: string, model: string, apiKey: string) =>
    req<{ workspace: Workspace }>("POST", "/api/workspaces", { name, template, baseURL, model, apiKey }),
  start: (id: string) => req<{ workspace: Workspace }>("POST", `/api/workspaces/${id}/start`),
  stop: (id: string) => req<{ workspace: Workspace }>("POST", `/api/workspaces/${id}/stop`),
  remove: (id: string) => req<void>("DELETE", `/api/workspaces/${id}`),
  logs: (id: string) => req<{ logs: string }>("GET", `/api/workspaces/${id}/logs?tail=150`),
  adminUsers: () => req<{ users: AdminUser[] }>("GET", "/api/admin/users"),
  adminAudit: () => req<{ events: AuditEvent[] }>("GET", "/api/admin/audit?limit=200"),
};
