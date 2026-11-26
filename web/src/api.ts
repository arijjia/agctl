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
  activity: () => req<any>("GET", "/api/admin/activity"),
  sessions: (userId: number) => req<any>("GET", `/api/admin/users/${userId}/sessions`),
  sessionsAll: () => req<any>("GET", "/api/admin/sessions"),
  trace: (userId: number, workspace: string, session: string) =>
    req<any>("GET", `/api/admin/users/${userId}/sessions/${encodeURIComponent(workspace)}/${encodeURIComponent(session)}`),
  quotas: () => req<any>("GET", "/api/admin/quotas"),
  quotaPut: (q: { scope: string; subject: string; dailyTokens: number; monthlyTokens: number; note: string }) =>
    req<any>("PUT", "/api/admin/quotas", q),
  quotaDelete: (id: number) => req<any>("DELETE", `/api/admin/quotas/${id}`),
  audit: (limit: number) => req<any>("GET", `/api/admin/audit?limit=${limit || 200}`),
};

// launcher — API лаунчера developer'а (роль user/developer; админ отклоняется).
export type SlotInfo = {
  slot: number;
  image: string;
  active: boolean;
  state: string; // running | exited | absent | …
  container?: string;
  status?: string;
  runningImage?: string;
};
export const launcher = {
  images: () => req<{ images: string[]; default: string; current: string }>("GET", "/api/launcher/images"),
  pickImage: (image: string) => req<{ image: string }>("PUT", "/api/me/image", { image }),
  start: () => req<{ status: string }>("POST", "/api/me/runtime/start"),
  stop: () => req<{ status: string }>("POST", "/api/me/runtime/stop"),
  containers: () => req<{ containers: any[] }>("GET", "/api/me/containers"),
  // Слоты мульти-рантайма (карточка developer-multi-containers).
  slots: () => req<{ slots: SlotInfo[]; max: number; live: number }>("GET", "/api/me/slots"),
  slotImage: (slot: number, image: string) => req<{ slot: number; image: string }>("PUT", `/api/me/slots/${slot}/image`, { image }),
  slotStart: (slot: number) => req<{ slot: number; status: string }>("POST", `/api/me/slots/${slot}/start`),
  slotStop: (slot: number) => req<{ slot: number; status: string }>("POST", `/api/me/slots/${slot}/stop`),
  slotActivate: (slot: number) => req<{ slot: number; active: boolean }>("POST", `/api/me/slots/${slot}/activate`),
  logs: (container: string, tail: number) =>
    req<{ container: string; state: string; logs: string }>(
      "GET",
      `/api/me/runtime/logs?tail=${tail || 200}${container ? `&container=${encodeURIComponent(container)}` : ""}`,
    ),
};

// local — лаунчер локального режима (agctl dev): образ и доступы на этой машине.
export type LocalEntry = {
  name: string;
  title: string;
  hint?: string;
  group: string;
  secret: boolean;
  set: boolean;
  value?: string;
  known: boolean;
};
export type LocalUI = {
  profile: string;
  workdir: string;
  images: { ref: string; present: boolean; flavor?: string }[];
  current: string;
  secrets: LocalEntry[];
  storage: string;
  runtime: { state: string; image?: string };
  task: { state: string; image: string; log: string[]; error: string; restartNeeded: boolean };
  llmKeySet: boolean;
};
export const local = {
  ui: () => req<LocalUI>("GET", "/api/local/ui"),
  setSecret: (name: string, value: string, plain: boolean) =>
    req<any>("PUT", `/api/local/ui/secrets/${encodeURIComponent(name)}`, { value, plain }),
  deleteSecret: (name: string) => req<any>("DELETE", `/api/local/ui/secrets/${encodeURIComponent(name)}`),
  setImage: (image: string) => req<any>("PUT", "/api/local/ui/image", { image }),
  setWorkdir: (workdir: string) => req<{ workdir: string }>("PUT", "/api/local/ui/workdir", { workdir }),
  start: () => req<any>("POST", "/api/local/ui/start"),
  restart: () => req<any>("POST", "/api/local/ui/restart"),
};

// places — рабочие места локального режима (лаунчер v2). Изменения передают
// ревизию: устаревшая отклоняется сервером (409), интерфейс перечитывает.
export type PlaceMount = { host: string; name: string };
export type PlaceTask = { state: string; image: string; log: string[]; error: string; restartNeeded: boolean };
export type Place = {
  id: string;
  slot: number; // слот лаунчера: 0 — основное место
  name: string;
  main: boolean;
  image: string;
  mounts: PlaceMount[];
  revision: number;
  url: string;
  dshHome: string;
  state: "running" | "sleeping" | "starting" | "building" | "error";
  task: PlaceTask;
  layout: { host: string; container: string }[];
};
export type FsListing = {
  path: string;
  parent: string;
  home: string;
  entries: { name: string; path: string; git: boolean }[];
  selectable: boolean;
  reason: string;
};
export const places = {
  list: () => req<{ places: Place[] }>("GET", "/api/local/ui/places"),
  create: (p: { name: string; id?: string; image?: string; mounts: PlaceMount[] }) =>
    req<Place>("POST", "/api/local/ui/places", p),
  update: (id: string, p: { name: string; image?: string; mounts: PlaceMount[]; revision: number }) =>
    req<Place>("PUT", `/api/local/ui/places/${encodeURIComponent(id)}`, p),
  remove: (id: string) => req<{ archived: string }>("DELETE", `/api/local/ui/places/${encodeURIComponent(id)}`),
  action: (id: string, action: "start" | "restart" | "stop") =>
    req<any>("POST", `/api/local/ui/places/${encodeURIComponent(id)}/${action}`),
  logs: (id: string, tail = 300) =>
    req<{ logs: string; running: boolean }>("GET", `/api/local/ui/places/${encodeURIComponent(id)}/logs?tail=${tail}`),
  fs: (path: string, hidden = false) =>
    req<FsListing>("GET", `/api/local/ui/fs?path=${encodeURIComponent(path)}${hidden ? "&hidden=1" : ""}`),
  fsDialog: () => req<{ cancelled: boolean; path?: string }>("POST", "/api/local/ui/fs/dialog", {}),
  reveal: (path: string, app: "finder" | "vscode") => req<any>("POST", "/api/local/ui/reveal", { path, app }),
};

// presets — пресеты возможностей: наборы MCP и скиллов контейнера, применяемые
// к рабочим местам. В dsh сессия создаётся с готовым пресетом; изменение
// передаёт ревизию (устаревшая — 409).
export type Preset = {
  id: string;
  name: string;
  description: string;
  mcp: string[];
  skills: string[];
  places: string[];
  revision: number;
};
export type PresetsCatalog = {
  presets: Preset[];
  mcp: { id: string; title: string; description: string; secretEnv: string }[];
  skills: { id: string; title: string; description: string }[];
  places: { id: string; name: string }[];
};
export const presets = {
  list: () => req<PresetsCatalog>("GET", "/api/local/ui/presets"),
  create: (p: { name: string; description?: string; mcp: string[]; skills: string[]; places: string[] }) =>
    req<Preset>("POST", "/api/local/ui/presets", p),
  update: (id: string, p: { name: string; description?: string; mcp: string[]; skills: string[]; places: string[]; revision: number }) =>
    req<Preset>("PUT", `/api/local/ui/presets/${encodeURIComponent(id)}`, p),
  remove: (id: string) => req<any>("DELETE", `/api/local/ui/presets/${encodeURIComponent(id)}`),
};

// onLocalEvents — подписка на изменения (другая вкладка, таб dsh, CLI):
// вызывает cb при каждом событии; возвращает отписку.
export function onLocalEvents(cb: (topic: string) => void): () => void {
  const es = new EventSource("/api/local/ui/events");
  es.addEventListener("change", (ev) => {
    try {
      cb(JSON.parse((ev as MessageEvent).data).topic);
    } catch {
      cb("");
    }
  });
  return () => es.close();
}

// providers — провайдеры моделей во вкладке «Доступ»: заданные профилем
// организации (source: profile — меняется только ключ) и личные (source: own).
// Значение ключа сервер не возвращает — только keySet.
export type Provider = {
  name: string;
  baseUrls: string[];
  models: string[];
  defaultModel: string;
  keyEnv: string;
  keySet: boolean;
  source: "profile" | "own";
  primary: boolean;
};
export type ProviderInput = { name: string; baseURL: string; models: string[]; defaultModel?: string; key?: string };
export const providers = {
  list: () => req<{ providers: Provider[]; canAdd: boolean }>("GET", "/api/local/ui/providers"),
  create: (p: ProviderInput) => req<{ providers: Provider[] }>("POST", "/api/local/ui/providers", p),
  update: (name: string, p: Omit<ProviderInput, "name">) =>
    req<{ providers: Provider[] }>("PUT", `/api/local/ui/providers/${encodeURIComponent(name)}`, p),
  remove: (name: string) => req<{ providers: Provider[] }>("DELETE", `/api/local/ui/providers/${encodeURIComponent(name)}`),
};
