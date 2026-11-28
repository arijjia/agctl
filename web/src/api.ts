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
  // source — откуда значение, если не из дома: profile — по умолчанию из профиля.
  source?: string;
};
// LocalImage — образ из каталога лаунчера: есть ли на машине и что внутри
// (метки io.agctl.* образа; у собранных до меток — пусто, outdated).
export type LocalImage = {
  ref: string;
  present: boolean;
  flavor?: string;
  dshVersion?: string;
  guestRevision?: string;
  guestApi?: number;
  variant?: string;
  built?: string;
  sizeMb?: number;
  outdated?: boolean;
  note?: string;
  // pullFrom — откуда «Скачать из реестра» тянет образ; пусто — реестр
  // выключен в профиле (images.registry: none).
  pullFrom?: string;
};
export type LocalUI = {
  profile: string;
  workdir: string;
  images: LocalImage[];
  current: string;
  secrets: LocalEntry[];
  storage: string;
  runtime: { state: string; image?: string };
  task: { state: string; image: string; log: string[]; error: string; restartNeeded: boolean };
  llmKeySet: boolean;
  runtimeKind?: string;
  // hostPaths — «пути как на хосте»: папки проектов видны в контейнере по
  // тем же путям, что на машине (config.yaml host_paths).
  hostPaths?: boolean;
  hostPathsAvailable?: boolean;
  runtimes?: RuntimeOption[];
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

// images — раздел «Образы»: каталог с местами, которые используют образ,
// план сборки (ступени, Dockerfile, аргументы, что качается из сети) и
// действия фоном (одна операция за раз, журнал — в task).
export type ImageUse = { id: string; name: string; running: boolean };
export type ImageView = LocalImage & { current: boolean; usedBy: ImageUse[] };
export type ImageTask = { state: string; image: string; log: string[]; error: string; cancelable?: boolean };
// ImageContext — файл или каталог контекста сборки ступени (кроме
// Dockerfile): откуда берётся (embedded — встроен в бинарь agctl, skel —
// скелет dsh-home, guest — guest-бинарь на диске, profile — файл профиля),
// где именно (path), размер, у каталога — число файлов.
export type ImageContext = {
  name: string;
  dir?: boolean;
  origin: string;
  from: string;
  path?: string;
  size: number;
  files?: number;
  missing?: boolean;
  modified?: string;
  note?: string;
};
// ImageFile — файл контекста (текст) или каталог (файлы с путями от корня контекста).
export type ImageFile = { path: string; size: number; dir?: boolean; files?: { path: string; size: number }[]; text?: string };
export type ImageStage = {
  ref: string;
  present: boolean;
  kind: string;
  title: string;
  source: string;
  dockerfile: string;
  args?: Record<string, string>;
  context?: ImageContext[];
  from?: string[];
  apt?: string[];
  npm?: string[];
  downloads?: string[];
};
export type ImagePlan = { ref: string; pull: string; pullRef?: string; stages: ImageStage[] };
// ImageAction — ensure: скачать или собрать недостающий; pull: скачать из
// реестра (без сборки); rebuild: собрать на этой машине; remove: удалить.
export type ImageAction = "ensure" | "pull" | "rebuild" | "remove";
export const imagesApi = {
  list: () => req<{ images: ImageView[]; task: ImageTask; actions: boolean }>("GET", "/api/local/ui/images"),
  plan: (ref: string) => req<ImagePlan>("GET", `/api/local/ui/images/plan?ref=${encodeURIComponent(ref)}`),
  // file — файл или каталог контекста сборки ступени stage (с 1, как в плане).
  file: (ref: string, stage: number, path: string) =>
    req<ImageFile>("GET", `/api/local/ui/images/file?ref=${encodeURIComponent(ref)}&stage=${stage}&path=${encodeURIComponent(path)}`),
  action: (action: ImageAction, ref: string) =>
    req<{ task: ImageTask }>("POST", `/api/local/ui/images/${action}`, { ref }),
  // cancel — оборвать идущее скачивание или сборку.
  cancel: () => req<{ status: string }>("POST", "/api/local/ui/images/cancel", {}),
};

// places — рабочие места локального режима (лаунчер v2). Изменения передают
// ревизию: устаревшая отклоняется сервером (409), интерфейс перечитывает.
export type PlaceMount = { host: string; name: string };
export type PlaceTask = { state: string; image: string; log: string[]; error: string; restartNeeded: boolean; cancelable?: boolean };
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
  // dsh-home в явной папке владельца — git-репозиторий конфигурации агента.
  dshHomeCustom?: boolean;
  dshHomeGit?: { repo: boolean; commits: boolean; remote?: string; dirty: boolean; branch?: string };
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
  create: (p: { name: string; id?: string; image?: string; mounts: PlaceMount[]; dshHome?: string }) =>
    req<Place>("POST", "/api/local/ui/places", p),
  update: (id: string, p: { name: string; image?: string; mounts: PlaceMount[]; revision: number; dshHome?: string }) =>
    req<Place>("PUT", `/api/local/ui/places/${encodeURIComponent(id)}`, p),
  // clone — новое место с теми же папками и образом и копией dsh-home
  // (сессии, скиллы, настройки); источник на время копии останавливается.
  clone: (id: string, p: { name?: string; start?: boolean }) =>
    req<Place>("POST", `/api/local/ui/places/${encodeURIComponent(id)}/clone`, p),
  remove: (id: string) => req<{ archived: string }>("DELETE", `/api/local/ui/places/${encodeURIComponent(id)}`),
  action: (id: string, action: "start" | "restart" | "stop" | "cancel") =>
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
  // missing — серверы пресета без нужных доступов: id сервера → имена
  // переменных («A|B» — хватит одной). В харнессе такие серверы отключены.
  missing?: Record<string, string[]>;
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

// ── единое веб-приложение: профиль, скачивание, пути ─────────────────────────

// raw — запрос с телом «как есть» (текст profile.yaml, PNG логотипа).
async function raw<T>(method: string, url: string, body: BodyInit, type: string): Promise<T> {
  const res = await fetch(url, { method, credentials: "same-origin", headers: { "Content-Type": type }, body });
  const text = await res.text();
  let data: any = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    /* не JSON */
  }
  if (!res.ok) throw new Error(data?.error || text || res.statusText);
  return data as T;
}

// profileApi — исходник профиля организации (profile.yaml в каталоге профиля).
export type ProfileSource = {
  id: string;
  dir: string;
  realDir?: string;
  writable: boolean;
  reason?: string;
  yaml: string;
  files: string[];
};
export const profileApi = {
  get: () => req<ProfileSource>("GET", "/api/profile/source"),
  check: (yaml: string) => raw<{ ok: boolean; error?: string }>("POST", "/api/profile/source/check", yaml, "text/yaml"),
  save: (yaml: string) => raw<{ saved: boolean; restartNeeded: boolean; path: string }>("PUT", "/api/profile/source", yaml, "text/yaml"),
  logo: (file: Blob) => raw<{ saved: boolean; path: string }>("PUT", "/api/profile/logo", file, "image/png"),
};

// downloads — страница «Скачать»: версия, репозитории и файлы с этого сервера.
export type Downloads = {
  version: string;
  profile: string;
  coreRepo: string;
  kitRepo: string;
  files: { name: string; size: number; url: string }[];
  downloadsDir: string;
  local: boolean;
};
export const downloadsApi = { get: () => req<Downloads>("GET", "/api/downloads") };

// paths — отчёт agctl dev paths (локальный режим): всё, что agctl держит на машине.
export type PathItem = { name: string; path: string; purpose: string; exists: boolean };
export type PathsReport = {
  root: { path: string; source: string; exists: boolean };
  binaries: PathItem[];
  executable: string;
  guest: { dir: string; files: PathItem[] };
  profile: { id?: string; dir?: string; source?: string; profilesDir: string; available: string[]; error?: string; [k: string]: any };
  home: { id: string; dir: string; source: string; initialized: boolean; files: PathItem[]; others: string[] };
  workdir: string;
  ui: { addr: string; url: string; running: boolean; pid?: number };
  bridge: { port: string; url: string; listen: string; tools: number; journal: string };
  image: { ref: string; source: string; previous?: string };
  docker: {
    containers: { name: string; purpose: string }[];
    egressNetwork: string;
    egressEnabled: boolean;
    mounts: { host: string; container: string; purpose: string }[];
  };
  secrets: { backend: string; service?: string; legacy?: string; file?: string; names: string[] };
};
export const pathsApi = { get: () => req<PathsReport>("GET", "/api/local/ui/paths") };

// runtimeApi — рантайм рабочего места локального режима: docker или sbx.
export type RuntimeOption = { id: string; title: string; available: boolean; detail?: string };
export const runtimeApi = {
  set: (runtime: string) => req<{ runtime: string; restartNeeded: boolean }>("PUT", "/api/local/ui/runtime", { runtime }),
  hostPaths: (enabled: boolean) => req<{ hostPaths: boolean }>("PUT", "/api/local/ui/hostpaths", { enabled }),
};
