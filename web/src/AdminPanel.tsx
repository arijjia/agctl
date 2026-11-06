import { useCallback, useEffect, useMemo, useState } from "react";
import { admin, api, type User } from "./api";
import { LogsPanel } from "./LogsPanel";
import { AreaChart, BarChart, Donut, HBarList, bucketLabel, bucketTip } from "./charts";

// AdminPanel — админ-панель супер-админа (карточка admin-panel-super-admin).
// Админ — не пользователь платформы: личного чата у него нет, вместо этого
// эта страница: сводка с графиками, пользователи, контейнеры, журнал запросов
// шлюза, траектории агентов и квоты. На каждой вкладке — поиск и фильтры:
// таблицы быстро вырастают, а траектория может быть в сотни событий.

type Overview = {
  users: { total: number; byRole: Record<string, number> };
  sessions: number;
  requestsToday: number;
  containers: { known: number; running: number };
  memoryBytes: number;
  image: string;
  chatTTLMinutes: number;
  version: string;
  startedAt: string;
};

type UserRow = {
  id: number;
  login: string;
  role: string;
  createdAt: string;
  container?: { name: string; image: string; state: string; status: string };
  volumeBytes: number;
  quota?: { dailyTokens: number; monthlyTokens: number } | null;
};

type ContainerRow = {
  name: string;
  workspace: string;
  userId: number;
  image: string;
  state: string;
  status: string;
  memBytes: number;
  cpuPercent: number;
  ssh: boolean;
};

type RequestRow = {
  id: number;
  ts: string;
  login: string;
  method: string;
  path: string;
  status: number;
  ms: number;
  bytesOut: number;
};

type SessionRow = { workspace: string; id: string; sizeBytes: number; modified: string; hasTrace: boolean; userId?: number; login?: string };

type QuotaRow = {
  id: number;
  scope: string;
  subject: string;
  dailyTokens: number;
  monthlyTokens: number;
  note: string;
  updatedAt: string;
};

const TABS = ["Обзор", "Пользователи", "Контейнеры", "Запросы", "Траектории", "Квоты", "Аудит"] as const;
type Tab = (typeof TABS)[number];

function fmtBytes(n: number): string {
  if (!n) return "0";
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} КБ`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} МиБ`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} ГиБ`;
}

function Err({ msg }: { msg: string }) {
  return msg ? <div className="error">{msg}</div> : null;
}

// ── поиск и фильтры ──────────────────────────────────────────────────────────

// matches — регистронезависимая проверка «вхождение подстроки»; q пустой = всегда да.
function matches(hay: string | number | undefined | null, q: string): boolean {
  if (!q) return true;
  return String(hay ?? "").toLowerCase().includes(q.toLowerCase());
}

// useTableFilter — поиск + опциональные селекты для таблицы вкладки.
// Паттерн: предикат собирается в вызывающей вкладке, сюда вынесен UI.
function TableToolbar({ search, onSearch, placeholder = "поиск…", shown, total, children }: {
  search: string;
  onSearch: (v: string) => void;
  placeholder?: string;
  shown: number;
  total: number;
  children?: React.ReactNode;
}) {
  return (
    <div className="toolbar">
      <input
        className="filter-search"
        placeholder={placeholder}
        value={search}
        onChange={(e) => onSearch(e.target.value)}
        style={{ width: 220 }}
      />
      {children}
      <span className="muted small">
        {shown === total ? `${total} зап.` : `показано ${shown} из ${total}`}
      </span>
      <button className="ghost small" onClick={() => onSearch("")} disabled={!search}>сброс</button>
    </div>
  );
}

// FilterSelect — селект с опциями (значение = значение опции).
function FilterSelect({ value, onChange, options, label }: {
  value: string;
  onChange: (v: string) => void;
  options: { value: string; label: string }[];
  label?: string;
}) {
  return (
    <select value={value} onChange={(e) => onChange(e.target.value)} aria-label={label} style={{ width: "auto" }}>
      {options.map((o) => (
        <option key={o.value} value={o.value}>{o.label}</option>
      ))}
    </select>
  );
}

type ActivityData = {
  hourly: { t: string; requests: number; users: number; errors: number; avgMs: number }[];
  daily: { t: string; requests: number; users: number; errors: number; avgMs: number }[];
  topUsers: { login: string; requests: number; errors: number; avgMs: number; bytesOut: number }[];
  statuses: { ok: number; redirect: number; clientError: number; serverError: number };
  registrations: { t: string; count: number }[];
};

// ── вкладка «Обзор»: сводка + графики ────────────────────────────────────────

function OverviewTab() {
  const [data, setData] = useState<Overview | null>(null);
  const [act, setAct] = useState<ActivityData | null>(null);
  const [err, setErr] = useState("");
  const load = useCallback(() => {
    admin.overview().then(setData).catch((e) => setErr(e.message));
    admin.activity().then(setAct).catch(() => setAct(null));
  }, []);
  useEffect(() => {
    load();
    const t = setInterval(load, 60000); // график живёт: обновление раз в минуту
    return () => clearInterval(t);
  }, [load]);
  if (err) return <Err msg={err} />;
  if (!data) return <p className="muted">…</p>;
  const mem = data.memoryBytes ? `${fmtBytes(data.memoryBytes)} суммарно` : "—";
  const st = act?.statuses;
  return (
    <div>
      <div className="stat-grid">
        <Stat label="Пользователей" value={data.users.total} hint={Object.entries(data.users.byRole).map(([k, v]) => `${k}: ${v}`).join(" · ")} />
        <Stat label="Активных сессий" value={data.sessions} hint="cookie-сессии в базе" />
        <Stat label="Контейнеров" value={`${data.containers.running}/${data.containers.known}`} hint="живых / известных" />
        <Stat label="Память контейнеров" value={mem} hint="one-shot docker stats" />
        <Stat label="Запросов сегодня" value={data.requestsToday} hint="журнал шлюза /chat/" />
        <Stat label="Образ / лимит" value={data.image} hint={`TTL сна: ${data.chatTTLMinutes} мин · v${data.version}`} />
      </div>
      {act && (
        <div className="chart-grid">
          <AreaChart
            title="Запросы шлюза — 24 часа"
            hint="кто-то прямо сейчас работает: линия растёт"
            data={act.hourly.map((p) => ({ label: bucketLabel(p.t, "hour"), value: p.requests, extra: bucketTip(p.t) }))}
            second={act.hourly.map((p) => ({ label: bucketLabel(p.t, "hour"), value: p.avgMs }))}
            secondName="средняя задержка, мс"
          />
          <BarChart
            title="Запросы по дням — 14 дней"
            hint="дневная активность платформы"
            color="#58a6ff"
            valueName="запросов"
            data={act.daily.map((p) => ({
              label: bucketLabel(p.t, "day"),
              value: p.requests,
              extra: `${bucketTip(p.t)} · ${p.users} польз. · ошибок: ${p.errors}`,
            }))}
          />
          <Donut
            title="Статусы ответов — 7 дней"
            hint="здоровье шлюза: 4xx/5xx — повод посмотреть журнал"
            segments={[
              { key: "ok", label: "2xx успешно", value: st?.ok || 0 },
              { key: "redirect", label: "3xx редиректы", value: st?.redirect || 0 },
              { key: "clientError", label: "4xx ошибки клиента", value: st?.clientError || 0 },
              { key: "serverError", label: "5xx ошибки сервера", value: st?.serverError || 0 },
            ]}
          />
          <HBarList
            title="Самые активные — 7 дней"
            hint="кто нагружает шлюз; в тултипе — задержка и трафик"
            data={(act.topUsers || []).map((u) => ({
              label: u.login,
              value: u.requests,
              extra: `${u.login}: ср. ${Math.round(u.avgMs)} мс · исходящий ${fmtBytes(u.bytesOut)} · ошибок ${u.errors}`,
            }))}
          />
          <BarChart
            title="Регистрации — 14 дней"
            hint="новые учётки платформы"
            color="#a371f7"
            valueName="регистраций"
            data={(act.registrations || []).map((r) => ({ label: bucketLabel(r.t, "day"), value: r.count, extra: bucketTip(r.t) }))}
          />
        </div>
      )}
    </div>
  );
}

function Stat({ label, value, hint }: { label: string; value: any; hint?: string }) {
  const text = String(value);
  return (
    <div className="stat-card">
      <div className="stat-label">{label}</div>
      {/* Длинные значения (тег образа) — мельче, чтобы влезали в одну строку */}
      <div className={text.length > 14 ? "stat-value long" : "stat-value"}>{text}</div>
      {hint && <div className="muted small">{hint}</div>}
    </div>
  );
}

// ── вкладка «Пользователи» ───────────────────────────────────────────────────

function UsersTab() {
  const [rows, setRows] = useState<UserRow[]>([]);
  const [withSizes, setWithSizes] = useState(false);
  const [err, setErr] = useState("");
  const [search, setSearch] = useState("");
  const [role, setRole] = useState("");
  const load = useCallback(() => {
    admin.usersDetailed(withSizes).then((r) => setRows(r.users || [])).catch((e) => setErr(e.message));
  }, [withSizes]);
  useEffect(load, [load]);
  const filtered = rows.filter(
    (u) => (matches(u.login, search) || matches(u.id, search) || matches(u.role, search)) && (!role || u.role === role),
  );
  return (
    <div>
      <TableToolbar search={search} onSearch={setSearch} placeholder="логин или id…" shown={filtered.length} total={rows.length}>
        <label className="row-hint">
          <input type="checkbox" checked={withSizes} onChange={(e) => setWithSizes(e.target.checked)} /> размер тома (медленнее)
        </label>
        <FilterSelect
          value={role}
          onChange={setRole}
          label="Роль"
          options={[{ value: "", label: "все роли" }, { value: "user", label: "user" }, { value: "developer", label: "developer" }, { value: "admin", label: "admin" }]}
        />
        <button className="ghost small" onClick={load}>Обновить</button>
      </TableToolbar>
      <Err msg={err} />
      <table className="admin-table">
        <thead>
          <tr><th>ID</th><th>Логин</th><th>Роль</th><th>Создан</th><th>Контейнер</th><th>Том</th><th>Квота (токены/день)</th></tr>
        </thead>
        <tbody>
          {filtered.map((u) => (
            <tr key={u.id}>
              <td>{u.id}</td>
              <td>{u.login}</td>
              <td><span className={`badge role-${u.role}`}>{u.role}</span></td>
              <td className="muted">{u.createdAt}</td>
              <td>
                {u.container ? (
                  <span>
                    <span className={`badge state-${u.container.state}`}>{u.container.state}</span>{" "}
                    <span className="muted small">{u.container.image}</span>
                  </span>
                ) : (
                  <span className="muted">спит / не было</span>
                )}
              </td>
              <td>{u.volumeBytes ? fmtBytes(u.volumeBytes) : "—"}</td>
              <td>{u.quota ? u.quota.dailyTokens || "—" : "—"}</td>
            </tr>
          ))}
          {!filtered.length && <tr><td colSpan={7} className="muted">ничего не найдено</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

// ── вкладка «Контейнеры» ─────────────────────────────────────────────────────

const CONTAINER_STATES = ["", "running", "exited", "created", "paused", "dead", "removed"];

function ContainersTab() {
  const [rows, setRows] = useState<ContainerRow[]>([]);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [logsFor, setLogsFor] = useState("");
  const [search, setSearch] = useState("");
  const [state, setState] = useState("");
  const load = useCallback(() => {
    admin.containers().then((r) => setRows(r.containers || [])).catch((e) => setErr(e.message));
  }, []);
  useEffect(load, [load]);
  const op = async (name: string, action: "stop" | "start" | "restart") => {
    if (action !== "start" && !confirm(`${action === "stop" ? "Остановить" : "Пересоздать"} ${name}? Данные на томе сохраняются.`)) return;
    setMsg("");
    setErr("");
    try {
      await admin.containerOp(name, action);
      setMsg(`${name}: ${action} ок`);
      setTimeout(load, 800);
    } catch (e: any) {
      setErr(e.message);
    }
  };
  const fetchLogs = useCallback(() => admin.containerLogs(logsFor, 300), [logsFor]);
  const filtered = rows.filter(
    (c) => (matches(c.name, search) || matches(c.image, search) || matches(c.userId, search)) && (!state || c.state === state),
  );
  return (
    <div>
      <TableToolbar search={search} onSearch={setSearch} placeholder="имя, образ, юзер…" shown={filtered.length} total={rows.length}>
        <FilterSelect
          value={state}
          onChange={setState}
          label="Состояние"
          options={CONTAINER_STATES.map((s) => ({ value: s, label: s || "все состояния" }))}
        />
        <button className="ghost small" onClick={load}>Обновить</button>
        {msg && <span className="muted small">{msg}</span>}
      </TableToolbar>
      <Err msg={err} />
      <table className="admin-table">
        <thead>
          <tr><th>Имя</th><th>Юзер</th><th>Образ</th><th>Состояние</th><th>Статус</th><th>Память</th><th>CPU</th><th></th></tr>
        </thead>
        <tbody>
          {filtered.map((c) => (
            <tr key={c.name}>
              <td className="mono">{c.name}</td>
              <td>{c.userId || "—"}</td>
              <td className="muted small">{c.image}</td>
              <td><span className={`badge state-${c.state}`}>{c.state}</span></td>
              <td className="muted small">{c.status}</td>
              <td>{c.memBytes ? fmtBytes(c.memBytes) : "—"}</td>
              <td>{c.cpuPercent ? `${c.cpuPercent.toFixed(1)}%` : "—"}</td>
              <td className="actions">
                <button className="ghost small" onClick={() => setLogsFor(c.name)}>Логи</button>
                <button className="ghost small" onClick={() => op(c.name, "restart")}>Рестарт</button>
                {c.state === "running" ? (
                  <button className="ghost small danger" onClick={() => op(c.name, "stop")}>Стоп</button>
                ) : (
                  <button className="ghost small" onClick={() => op(c.name, "start")}>Старт</button>
                )}
              </td>
            </tr>
          ))}
          {!filtered.length && <tr><td colSpan={8} className="muted">ничего не найдено</td></tr>}
        </tbody>
      </table>
      {logsFor && (
        <LogsPanel fetchLogs={fetchLogs} onClose={() => setLogsFor("")} />
      )}
    </div>
  );
}

// ── вкладка «Запросы» ────────────────────────────────────────────────────────

function RequestsTab() {
  const [rows, setRows] = useState<RequestRow[]>([]);
  const [userId, setUserId] = useState("");
  const [limit, setLimit] = useState(200);
  const [err, setErr] = useState("");
  const [search, setSearch] = useState("");
  const [method, setMethod] = useState("");
  const [status, setStatus] = useState("");
  const load = () => {
    admin.requests(Number(userId) || 0, limit).then((r) => setRows(r.requests || [])).catch((e) => setErr(e.message));
  };
  useEffect(load, []);
  const methods = useMemo(() => [...new Set(rows.map((r) => r.method))].sort(), [rows]);
  const filtered = rows.filter(
    (r0) =>
      (matches(r0.login, search) || matches(r0.method, search) || matches(r0.path, search) || matches(r0.status, search)) &&
      (!method || r0.method === method) &&
      (!status || String(r0.status)[0] === status || String(r0.status) === status),
  );
  return (
    <div>
      <TableToolbar search={search} onSearch={setSearch} placeholder="логин, путь, статус…" shown={filtered.length} total={rows.length}>
        <FilterSelect value={method} onChange={setMethod} label="Метод" options={[{ value: "", label: "все методы" }, ...methods.map((m) => ({ value: m, label: m }))]} />
        <FilterSelect
          value={status}
          onChange={setStatus}
          label="Статус"
          options={[
            { value: "", label: "все статусы" },
            { value: "2", label: "2xx" },
            { value: "3", label: "3xx" },
            { value: "4", label: "4xx" },
            { value: "5", label: "5xx" },
          ]}
        />
        <input placeholder="user id" value={userId} onChange={(e) => setUserId(e.target.value)} style={{ width: 90 }} />
        <input placeholder="limit" value={limit} onChange={(e) => setLimit(Number(e.target.value) || 200)} style={{ width: 80 }} />
        <button className="ghost small" onClick={load}>Показать</button>
      </TableToolbar>
      <Err msg={err} />
      <table className="admin-table">
        <thead><tr><th>Время</th><th>Логин</th><th>Метод</th><th>Путь</th><th>Статус</th><th>мс</th><th>Байт</th></tr></thead>
        <tbody>
          {filtered.map((r0) => (
            <tr key={r0.id}>
              <td className="muted small">{r0.ts}</td>
              <td>{r0.login}</td>
              <td>{r0.method}</td>
              <td className="mono small">{r0.path}</td>
              <td>{r0.status}</td>
              <td>{r0.ms}</td>
              <td>{r0.bytesOut ? fmtBytes(r0.bytesOut) : "—"}</td>
            </tr>
          ))}
          {!filtered.length && <tr><td colSpan={7} className="muted">журнал пуст / фильтр ничего не нашёл</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

// ── вкладка «Траектории» ─────────────────────────────────────────────────────

// eventText — человеческий текст события для поиска и лога (без служебных полей).
function eventText(ev: any): string {
  const d = ev?.data || {};
  const msg = d.message || d;
  const content = msg?.content;
  if (Array.isArray(content)) {
    return content
      .map((c: any) => {
        if (c?.type === "tool-call") return `${c.name}(${c.arguments || ""})`;
        return c?.text || "";
      })
      .filter(Boolean)
      .join(" ");
  }
  if (typeof d.preset === "string") return d.preset;
  if (typeof d.mode === "string") return d.mode;
  if (typeof d.policy === "string") return d.policy;
  if (typeof d.turn === "number") return `turn ${d.turn}${d.step ? ` / step ${d.step}` : ""}`;
  return "";
}

// formatTraceLog — траектория как текстовый лог (для кнопки «Копировать как лог»).
// Формат: "#0007 2026-11-05T12:20:31.354Z [tool/call] grep {"pattern":"..."}"
export function formatTraceLog(events: any[]): string {
  const lines: string[] = [];
  for (let i = 0; i < events.length; i++) {
    const ev = events[i] || {};
    const ts = ev.time ? new Date(ev.time).toISOString() : "";
    const type = ev.type || "?";
    const seq = String(ev.seq ?? i).padStart(4, "0");
    const text = eventText(ev).replace(/\s+/g, " ").trim();
    let body = text;
    if (!body) {
      const d = ev.data || {};
      if (type === "turn/start" || type === "turn/end") body = `turn ${d.turn}`;
      else if (type === "step/start" || type === "step/end") body = `turn ${d.turn} / step ${d.step}`;
      else body = JSON.stringify(d);
    }
    if (body.length > 2000) body = body.slice(0, 2000) + "…[обрезано]";
    lines.push(`#${seq} ${ts} [${type}] ${body}`);
  }
  return lines.join("\n");
}

async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    try {
      const ta = document.createElement("textarea");
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand("copy");
      ta.remove();
      return true;
    } catch {
      return false;
    }
  }
}

function TracesTab() {
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [events, setEvents] = useState<any[] | null>(null);
  const [cur, setCur] = useState("");
  const [err, setErr] = useState("");
  const [sessionSearch, setSessionSearch] = useState("");
  const [evType, setEvType] = useState("");
  const [evSearch, setEvSearch] = useState("");
  const [copied, setCopied] = useState(false);
  const [curUser, setCurUser] = useState(0); // владелец открытой траектории
  const loadSessions = useCallback(() => {
    setErr("");
    setEvents(null);
    admin.sessionsAll().then((r) => setSessions(r.sessions || [])).catch((e) => setErr(e.message));
  }, []);
  useEffect(loadSessions, [loadSessions]);
  const openTrace = (s: SessionRow) => {
    setErr("");
    setEvType("");
    setEvSearch("");
    admin.trace(Number(s.userId), s.workspace, s.id)
      .then((r) => { setEvents(r.events || []); setCur(`${s.userId}/${s.workspace}/${s.id}`); setCurUser(Number(s.userId)); })
      .catch((e) => setErr(e.message));
  };
  const filteredSessions = sessions.filter(
    (s) => matches(s.workspace, sessionSearch) || matches(s.id, sessionSearch) || matches(s.userId, sessionSearch) || matches(s.login, sessionSearch),
  );
  const evTypes = useMemo(() => [...new Set((events || []).map((e) => e.type || "?"))].sort(), [events]);
  const filteredEvents = useMemo(
    () =>
      (events || []).filter((ev) => {
        if (evType && (ev.type || "?") !== evType) return false;
        if (!evSearch) return true;
        const hay = `${eventText(ev)} ${JSON.stringify(ev.data ?? {})}`;
        return hay.toLowerCase().includes(evSearch.toLowerCase());
      }),
    [events, evType, evSearch],
  );
  const copyLog = async () => {
    if (await copyText(formatTraceLog(filteredEvents))) {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }
  };
  return (
    <div className="traces-wrap">
      <TableToolbar search={sessionSearch} onSearch={setSessionSearch} placeholder="пользователь, workspace, сессия…" shown={filteredSessions.length} total={sessions.length}>
        <button className="ghost small" onClick={loadSessions}>Обновить</button>
      </TableToolbar>
      <Err msg={err} />
      <table className="admin-table">
        <thead><tr><th>Пользователь</th><th>Проект (workspace)</th><th>Сессия</th><th>Размер</th><th>Изменена</th><th></th></tr></thead>
        <tbody>
          {filteredSessions.map((s) => (
            <tr key={s.userId + "/" + s.workspace + s.id}>
              <td>{s.login ? `${s.login} (#${s.userId})` : `#${s.userId}`}</td>
              <td className="mono small">{s.workspace}</td>
              <td className="mono small">{s.id}</td>
              <td>{fmtBytes(s.sizeBytes)}</td>
              <td className="muted small">{s.modified}</td>
              <td>{s.hasTrace && <button className="ghost small" onClick={() => openTrace(s)}>Траектория</button>}</td>
            </tr>
          ))}
          {!filteredSessions.length && <tr><td colSpan={6} className="muted">нет сессий (нажмите «Обновить»)</td></tr>}
        </tbody>
      </table>
      {events && (
        <div className="trace-view">
          <div className="toolbar">
            <h3 className="mono small" style={{ margin: 0 }}>{cur} — {filteredEvents.length} из {events.length} событий</h3>
            <span className="spacer" />
            <FilterSelect
              value={evType}
              onChange={setEvType}
              label="Тип события"
              options={[{ value: "", label: "все типы" }, ...evTypes.map((t) => ({ value: t, label: t }))]}
            />
            <button className="ghost small" onClick={copyLog}>Копировать как лог</button>
            <button className="ghost small" onClick={() => setEvents(null)}>Закрыть</button>
          </div>
          <div className="toolbar">
            <input
              className="filter-search"
              placeholder="поиск по событиям траектории (текст, JSON)…"
              value={evSearch}
              onChange={(e) => setEvSearch(e.target.value)}
              style={{ width: 340 }}
            />
            {copied && <span className="muted small">лог скопирован в буфер</span>}
          </div>
          <div className="trace-events">
            {filteredEvents.map((ev, i) => (
              <div key={i} className="trace-event">
                <span className="badge">{ev.type || "?"}</span>
                <span className="muted small mono"> #{String(ev.seq ?? i).padStart(4, "0")} {ev.time ? new Date(ev.time).toLocaleString("ru-RU") : ""}</span>
                {eventText(ev) && <div className="small" style={{ marginTop: 2, whiteSpace: "pre-wrap" }}>{eventText(ev).slice(0, 2000)}</div>}
                <details>
                  <summary className="muted small">JSON</summary>
                  <pre className="mono small">{JSON.stringify(ev, null, 1)}</pre>
                </details>
              </div>
            ))}
            {!filteredEvents.length && <p className="muted small">фильтр ничего не нашёл</p>}
          </div>
        </div>
      )}
    </div>
  );
}

// ── вкладка «Квоты» ──────────────────────────────────────────────────────────

function QuotasTab() {
  const [rows, setRows] = useState<QuotaRow[]>([]);
  const [scope, setScope] = useState("role");
  const [subject, setSubject] = useState("");
  const [daily, setDaily] = useState("");
  const [monthly, setMonthly] = useState("");
  const [note, setNote] = useState("");
  const [editID, setEditID] = useState<number | null>(null); // id редактируемой квоты
  const [err, setErr] = useState("");
  const [search, setSearch] = useState("");
  const [scopeFilter, setScopeFilter] = useState("");
  const load = useCallback(() => {
    admin.quotas().then((r) => setRows(r.quotas || [])).catch((e) => setErr(e.message));
  }, []);
  useEffect(load, [load]);

  const startEdit = (q: QuotaRow) => {
    setEditID(q.id);
    setScope(q.scope);
    setSubject(q.subject);
    setDaily(q.dailyTokens ? String(q.dailyTokens) : "");
    setMonthly(q.monthlyTokens ? String(q.monthlyTokens) : "");
    setNote(q.note || "");
  };
  const resetForm = () => {
    setEditID(null);
    setScope("role");
    setSubject("");
    setDaily("");
    setMonthly("");
    setNote("");
  };
  const save = async () => {
    setErr("");
    try {
      const r = await admin.quotaPut({
        scope, subject, dailyTokens: Number(daily) || 0, monthlyTokens: Number(monthly) || 0, note,
      });
      setRows(r.quotas || []);
      resetForm();
    } catch (e: any) {
      setErr(e.message);
    }
  };
  const del = async (id: number) => {
    try {
      const r = await admin.quotaDelete(id);
      setRows(r.quotas || []);
      if (editID === id) resetForm();
    } catch (e: any) {
      setErr(e.message);
    }
  };
  const filtered = rows.filter(
    (q) => (matches(q.subject, search) || matches(q.note, search)) && (!scopeFilter || q.scope === scopeFilter),
  );
  return (
    <div>
      <p className="muted small">
        LiteLLM-ключ на платформу один — бюджеты токенов назначает платформа.
        0 = без лимита на этом уровне; персональная квота перекрывает ролевую.
        «Изменить» заполняет форму значениями строки: сохранение обновляет квоту
        (PUT по паре область+кому), а не создаёт дубликат.
      </p>
      <TableToolbar search={search} onSearch={setSearch} placeholder="кому, примечание…" shown={filtered.length} total={rows.length}>
        <FilterSelect
          value={scopeFilter}
          onChange={setScopeFilter}
          label="Область"
          options={[{ value: "", label: "все области" }, { value: "role", label: "роль" }, { value: "user", label: "пользователь" }]}
        />
      </TableToolbar>
      <div className="toolbar">
        <select value={scope} onChange={(e) => setScope(e.target.value)} disabled={editID !== null}>
          <option value="role">роль</option>
          <option value="user">пользователь</option>
        </select>
        <input
          placeholder={scope === "role" ? "user | developer" : "логин"}
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
          style={{ width: 160 }}
          disabled={editID !== null}
        />
        <input placeholder="токенов/день" value={daily} onChange={(e) => setDaily(e.target.value)} style={{ width: 130 }} />
        <input placeholder="токенов/месяц" value={monthly} onChange={(e) => setMonthly(e.target.value)} style={{ width: 140 }} />
        <input placeholder="примечание" value={note} onChange={(e) => setNote(e.target.value)} style={{ width: 200 }} />
        <button className="primary small" onClick={save} disabled={!subject.trim()}>
          {editID !== null ? "Сохранить изменения" : "Сохранить"}
        </button>
        {editID !== null && <button className="ghost small" onClick={resetForm}>отменить правку</button>}
        {editID !== null && (
          <span className="muted small">правка квоты id {editID}: кому менять нельзя — удалите и создайте заново</span>
        )}
      </div>
      <Err msg={err} />
      <table className="admin-table">
        <thead><tr><th>Область</th><th>Кому</th><th>Токенов/день</th><th>Токенов/месяц</th><th>Примечание</th><th>Обновлено</th><th></th></tr></thead>
        <tbody>
          {filtered.map((q) => (
            <tr key={q.id} style={editID === q.id ? { outline: "1px solid var(--accent)" } : undefined}>
              <td>{q.scope}</td>
              <td>{q.subject}</td>
              <td>{q.dailyTokens || "—"}</td>
              <td>{q.monthlyTokens || "—"}</td>
              <td className="muted small">{q.note}</td>
              <td className="muted small">{q.updatedAt}</td>
              <td className="actions">
                <button className="ghost small" onClick={() => startEdit(q)}>Изменить</button>
                <button className="ghost small danger" onClick={() => del(q.id)}>Удалить</button>
              </td>
            </tr>
          ))}
          {!filtered.length && <tr><td colSpan={7} className="muted">квоты не заданы</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

// ── вкладка «Аудит» ──────────────────────────────────────────────────────────

function AuditTab() {
  const [rows, setRows] = useState<any[]>([]);
  const [err, setErr] = useState("");
  const [search, setSearch] = useState("");
  const [action, setAction] = useState("");
  const load = () => {
    admin.audit(200).then((r) => setRows(r.events || [])).catch((e) => setErr(e.message));
  };
  useEffect(load, []);
  const actions = useMemo(() => [...new Set(rows.map((e) => e.action))].filter(Boolean).sort(), [rows]);
  const filtered = rows.filter(
    (e) => (matches(e.actor, search) || matches(e.action, search) || matches(e.target, search) || matches(e.details, search)) && (!action || e.action === action),
  );
  return (
    <div>
      <TableToolbar search={search} onSearch={setSearch} placeholder="кто, объект, детали…" shown={filtered.length} total={rows.length}>
        <FilterSelect
          value={action}
          onChange={setAction}
          label="Действие"
          options={[{ value: "", label: "все действия" }, ...actions.map((a) => ({ value: a, label: a }))]}
        />
        <button className="ghost small" onClick={load}>Обновить</button>
      </TableToolbar>
      <Err msg={err} />
      <table className="admin-table">
        <thead><tr><th>Время</th><th>Кто</th><th>Действие</th><th>Объект</th><th>Детали</th></tr></thead>
        <tbody>
          {filtered.map((e, i) => (
            <tr key={i}>
              <td className="muted small">{e.ts}</td>
              <td>{e.actor}</td>
              <td className="mono small">{e.action}</td>
              <td className="mono small">{e.target}</td>
              <td className="muted small">{e.details}</td>
            </tr>
          ))}
          {!filtered.length && <tr><td colSpan={5} className="muted">ничего не найдено</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

// ── каркас ──────────────────────────────────────────────────────────────────

export default function AdminPanel({ user }: { user: User }) {
  const [tab, setTab] = useState<Tab>("Обзор");
  return (
    <div className="admin-shell">
      <header className="admin-header">
        <div>
          <h1>{(window as any).__AGCTL_BRAND__?.name || "agctl"} — панель управления</h1>
          <p className="muted small">
            {user.login} · роль admin · личный чат администратору не выдаётся
          </p>
        </div>
        <button
          className="ghost small"
          onClick={async () => { try { await api.logout(); } catch {} window.location.replace("/"); }}
        >
          Выйти
        </button>
      </header>
      <nav className="tabs admin-tabs">
        {TABS.map((t) => (
          <button key={t} className={t === tab ? "tab active" : "tab"} onClick={() => setTab(t)}>
            {t}
          </button>
        ))}
      </nav>
      <main className="admin-main">
        {tab === "Обзор" && <OverviewTab />}
        {tab === "Пользователи" && <UsersTab />}
        {tab === "Контейнеры" && <ContainersTab />}
        {tab === "Запросы" && <RequestsTab />}
        {tab === "Траектории" && <TracesTab />}
        {tab === "Квоты" && <QuotasTab />}
        {tab === "Аудит" && <AuditTab />}
      </main>
    </div>
  );
}
