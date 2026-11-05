// api.ts — тонкий клиент REST API agctl (только авторизация: весь остальной
// интерфейс платформы живёт внутри dsh).

export type Role = "user" | "developer" | "admin";
export type User = { login: string; role: Role };

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
  register: (login: string, password: string) => req<{ user: User }>("POST", "/api/register", { login, password }),
  login: (login: string, password: string) => req<{ user: User }>("POST", "/api/login", { login, password }),
  logout: () => req<void>("POST", "/api/logout"),
  me: () => req<{ user: User }>("GET", "/api/me"),
};

// admin — API админ-панели супер-админа (всё под requireAdmin на сервере).
export const admin = {
  overview: () => req<any>("GET", "/api/admin/overview"),
  usersDetailed: (withSizes: boolean) =>
    req<any>("GET", `/api/admin/users/details${withSizes ? "?withSizes=1" : ""}`),
  containers: () => req<any>("GET", "/api/admin/containers"),
  containerOp: (name: string, op: "stop" | "start" | "restart") =>
    req<any>("POST", `/api/admin/containers/${encodeURIComponent(name)}/${op}`),
  containerLogs: (name: string, tail: number) =>
    req<{ container: string; state: string; logs: string }>(
      "GET",
      `/api/admin/containers/${encodeURIComponent(name)}/logs?tail=${tail || 200}`,
    ),
  requests: (userId: number, limit: number) =>
    req<any>("GET", `/api/admin/requests?user_id=${userId || 0}&limit=${limit || 200}`),
  sessions: (userId: number) => req<any>("GET", `/api/admin/users/${userId}/sessions`),
  trace: (userId: number, workspace: string, session: string) =>
    req<any>("GET", `/api/admin/users/${userId}/sessions/${encodeURIComponent(workspace)}/${encodeURIComponent(session)}`),
  quotas: () => req<any>("GET", "/api/admin/quotas"),
  quotaPut: (q: { scope: string; subject: string; dailyTokens: number; monthlyTokens: number; note: string }) =>
    req<any>("PUT", "/api/admin/quotas", q),
  quotaDelete: (id: number) => req<any>("DELETE", `/api/admin/quotas/${id}`),
  audit: (limit: number) => req<any>("GET", `/api/admin/audit?limit=${limit || 200}`),
};

// launcher — API лаунчера developer'а (роль user/developer; админ отклоняется).
export const launcher = {
  images: () => req<{ images: string[]; default: string; current: string }>("GET", "/api/launcher/images"),
  pickImage: (image: string) => req<{ image: string }>("PUT", "/api/me/image", { image }),
  start: () => req<{ status: string }>("POST", "/api/me/runtime/start"),
  stop: () => req<{ status: string }>("POST", "/api/me/runtime/stop"),
  containers: () => req<{ containers: any[] }>("GET", "/api/me/containers"),
  logs: (container: string, tail: number) =>
    req<{ container: string; state: string; logs: string }>(
      "GET",
      `/api/me/runtime/logs?tail=${tail || 200}${container ? `&container=${encodeURIComponent(container)}` : ""}`,
    ),
};
