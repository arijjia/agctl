import { useCallback, useEffect, useState } from "react";
import { admin, api, type User } from "./api";
import { LogsPanel } from "./LogsPanel";

// AdminPanel — админ-панель супер-админа (карточка admin-panel-super-admin).
// Админ — не пользователь платформы: личного чата у него нет, вместо этого
// эта страница: сводка, пользователи, контейнеры, журнал запросов шлюза,
// траектории агентов и квоты. Всё read-базируется на requireAdmin-API.

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

type SessionRow = { workspace: string; id: string; sizeBytes: number; modified: string; hasTrace: boolean };

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

// ── вкладки ─────────────────────────────────────────────────────────────────

function OverviewTab() {
  const [data, setData] = useState<Overview | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    admin.overview().then(setData).catch((e) => setErr(e.message));
  }, []);
  if (err) return <Err msg={err} />;
  if (!data) return <p className="muted">…</p>;
  const mem = data.memoryBytes ? `${fmtBytes(data.memoryBytes)} суммарно` : "—";
  return (
    <div className="stat-grid">
      <Stat label="Пользователей" value={data.users.total} hint={Object.entries(data.users.byRole).map(([k, v]) => `${k}: ${v}`).join(" · ")} />
      <Stat label="Активных сессий" value={data.sessions} hint="cookie-сессии в базе" />
      <Stat label="Контейнеров" value={`${data.containers.running}/${data.containers.known}`} hint="живых / известных" />
      <Stat label="Память контейнеров" value={mem} hint="one-shot docker stats" />
      <Stat label="Запросов сегодня" value={data.requestsToday} hint="журнал шлюза /chat/" />
      <Stat label="Образ / лимит" value={data.image} hint={`TTL сна: ${data.chatTTLMinutes} мин · v${data.version}`} />
    </div>
  );
}

function Stat({ label, value, hint }: { label: string; value: any; hint?: string }) {
  return (
    <div className="stat-card">
      <div className="stat-label">{label}</div>
      <div className="stat-value">{String(value)}</div>
      {hint && <div className="muted small">{hint}</div>}
    </div>
  );
}

function UsersTab() {
  const [rows, setRows] = useState<UserRow[]>([]);
  const [withSizes, setWithSizes] = useState(false);
  const [err, setErr] = useState("");
  const load = useCallback(() => {
    admin.usersDetailed(withSizes).then((r) => setRows(r.users || [])).catch((e) => setErr(e.message));
  }, [withSizes]);
  useEffect(load, [load]);
  return (
    <div>
      <label className="row-hint">
        <input type="checkbox" checked={withSizes} onChange={(e) => setWithSizes(e.target.checked)} /> считать размер тома (медленнее)
      </label>
      <button className="ghost small" onClick={load}>Обновить</button>
      <Err msg={err} />
      <table className="admin-table">
        <thead>
          <tr><th>ID</th><th>Логин</th><th>Роль</th><th>Создан</th><th>Контейнер</th><th>Том</th><th>Квота (токены/день)</th></tr>
        </thead>
        <tbody>
          {rows.map((u) => (
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
        </tbody>
      </table>
    </div>
  );
}

function ContainersTab() {
  const [rows, setRows] = useState<ContainerRow[]>([]);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [logsFor, setLogsFor] = useState("");
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
  return (
    <div>
      <button className="ghost small" onClick={load}>Обновить</button>
      {msg && <p className="muted small">{msg}</p>}
      <Err msg={err} />
      <table className="admin-table">
        <thead>
          <tr><th>Имя</th><th>Юзер</th><th>Образ</th><th>Состояние</th><th>Статус</th><th>Память</th><th>CPU</th><th></th></tr>
        </thead>
        <tbody>
          {rows.map((c) => (
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
          {!rows.length && <tr><td colSpan={8} className="muted">контейнеров нет</td></tr>}
        </tbody>
      </table>
      {logsFor && (
        <LogsPanel fetchLogs={fetchLogs} onClose={() => setLogsFor("")} />
      )}
    </div>
  );
}

function RequestsTab() {
  const [rows, setRows] = useState<RequestRow[]>([]);
  const [userId, setUserId] = useState("");
  const [limit, setLimit] = useState(200);
  const [err, setErr] = useState("");
  const load = () => {
    admin.requests(Number(userId) || 0, limit).then((r) => setRows(r.requests || [])).catch((e) => setErr(e.message));
  };
  useEffect(load, []);
  return (
    <div>
      <div className="toolbar">
        <input placeholder="user id" value={userId} onChange={(e) => setUserId(e.target.value)} style={{ width: 90 }} />
        <input placeholder="limit" value={limit} onChange={(e) => setLimit(Number(e.target.value) || 200)} style={{ width: 80 }} />
        <button className="ghost small" onClick={load}>Показать</button>
      </div>
      <Err msg={err} />
      <table className="admin-table">
        <thead><tr><th>Время</th><th>Логин</th><th>Метод</th><th>Путь</th><th>Статус</th><th>мс</th><th>Байт</th></tr></thead>
        <tbody>
          {rows.map((r0) => (
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
          {!rows.length && <tr><td colSpan={7} className="muted">журнал пуст</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

function TracesTab() {
  const [userId, setUserId] = useState("");
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [events, setEvents] = useState<any[] | null>(null);
  const [cur, setCur] = useState("");
  const [err, setErr] = useState("");
  const loadSessions = () => {
    const uid = Number(userId);
    if (!uid) { setErr("укажите id пользователя"); return; }
    setErr("");
    setEvents(null);
    admin.sessions(uid).then((r) => setSessions(r.sessions || [])).catch((e) => setErr(e.message));
  };
  const openTrace = (s: SessionRow) => {
    setErr("");
    admin.trace(Number(userId), s.workspace, s.id)
      .then((r) => { setEvents(r.events || []); setCur(`${s.workspace}/${s.id}`); })
      .catch((e) => setErr(e.message));
  };
  return (
    <div className="traces-wrap">
      <div className="toolbar">
        <input placeholder="id пользователя" value={userId} onChange={(e) => setUserId(e.target.value)} style={{ width: 120 }} />
        <button className="ghost small" onClick={loadSessions}>Сессии</button>
      </div>
      <Err msg={err} />
      <table className="admin-table">
        <thead><tr><th>Проект (workspace)</th><th>Сессия</th><th>Размер</th><th>Изменена</th><th></th></tr></thead>
        <tbody>
          {sessions.map((s) => (
            <tr key={s.workspace + s.id}>
              <td className="mono small">{s.workspace}</td>
              <td className="mono small">{s.id}</td>
              <td>{fmtBytes(s.sizeBytes)}</td>
              <td className="muted small">{s.modified}</td>
              <td>{s.hasTrace && <button className="ghost small" onClick={() => openTrace(s)}>Траектория</button>}</td>
            </tr>
          ))}
          {!sessions.length && <tr><td colSpan={5} className="muted">нет сессий (укажите id и нажмите «Сессии»)</td></tr>}
        </tbody>
      </table>
      {events && (
        <div className="trace-view">
          <h3 className="mono small">{cur} — {events.length} событий</h3>
          <div className="trace-events">
            {events.map((ev, i) => (
              <div key={i} className="trace-event">
                <span className="badge">{ev.type || "?"}</span>
                <pre className="mono small">{JSON.stringify(ev, null, 1)}</pre>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function QuotasTab() {
  const [rows, setRows] = useState<QuotaRow[]>([]);
  const [scope, setScope] = useState("role");
  const [subject, setSubject] = useState("");
  const [daily, setDaily] = useState("");
  const [monthly, setMonthly] = useState("");
  const [note, setNote] = useState("");
  const [err, setErr] = useState("");
  const load = useCallback(() => {
    admin.quotas().then((r) => setRows(r.quotas || [])).catch((e) => setErr(e.message));
  }, []);
  useEffect(load, [load]);
  const save = async () => {
    setErr("");
    try {
      const r = await admin.quotaPut({
        scope, subject, dailyTokens: Number(daily) || 0, monthlyTokens: Number(monthly) || 0, note,
      });
      setRows(r.quotas || []);
      setSubject(""); setDaily(""); setMonthly(""); setNote("");
    } catch (e: any) {
      setErr(e.message);
    }
  };
  const del = async (id: number) => {
    try {
      const r = await admin.quotaDelete(id);
      setRows(r.quotas || []);
    } catch (e: any) {
      setErr(e.message);
    }
  };
  return (
    <div>
      <p className="muted small">
        LiteLLM-ключ на платформу один — бюджеты токенов назначает платформа.
        0 = без лимита на этом уровне; персональная квота перекрывает ролевую.
        Применение бюджетов к LiteLLM (виртуальные ключи) — следующая итерация.
      </p>
      <div className="toolbar">
        <select value={scope} onChange={(e) => setScope(e.target.value)}>
          <option value="role">роль</option>
          <option value="user">пользователь</option>
        </select>
        <input placeholder={scope === "role" ? "user | developer" : "логин"} value={subject} onChange={(e) => setSubject(e.target.value)} style={{ width: 160 }} />
        <input placeholder="токенов/день" value={daily} onChange={(e) => setDaily(e.target.value)} style={{ width: 130 }} />
        <input placeholder="токенов/месяц" value={monthly} onChange={(e) => setMonthly(e.target.value)} style={{ width: 140 }} />
        <input placeholder="примечание" value={note} onChange={(e) => setNote(e.target.value)} style={{ width: 200 }} />
        <button className="primary small" onClick={save} disabled={!subject.trim()}>Сохранить</button>
      </div>
      <Err msg={err} />
      <table className="admin-table">
        <thead><tr><th>Область</th><th>Кому</th><th>Токенов/день</th><th>Токенов/месяц</th><th>Примечание</th><th>Обновлено</th><th></th></tr></thead>
        <tbody>
          {rows.map((q) => (
            <tr key={q.id}>
              <td>{q.scope}</td>
              <td>{q.subject}</td>
              <td>{q.dailyTokens || "—"}</td>
              <td>{q.monthlyTokens || "—"}</td>
              <td className="muted small">{q.note}</td>
              <td className="muted small">{q.updatedAt}</td>
              <td><button className="ghost small danger" onClick={() => del(q.id)}>Удалить</button></td>
            </tr>
          ))}
          {!rows.length && <tr><td colSpan={7} className="muted">квоты не заданы</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

function AuditTab() {
  const [rows, setRows] = useState<any[]>([]);
  const [err, setErr] = useState("");
  const load = () => {
    admin.audit(200).then((r) => setRows(r.events || [])).catch((e) => setErr(e.message));
  };
  useEffect(load, []);
  return (
    <div>
      <button className="ghost small" onClick={load}>Обновить</button>
      <Err msg={err} />
      <table className="admin-table">
        <thead><tr><th>Время</th><th>Кто</th><th>Действие</th><th>Объект</th><th>Детали</th></tr></thead>
        <tbody>
          {rows.map((e, i) => (
            <tr key={i}>
              <td className="muted small">{e.ts}</td>
              <td>{e.actor}</td>
              <td className="mono small">{e.action}</td>
              <td className="mono small">{e.target}</td>
              <td className="muted small">{e.details}</td>
            </tr>
          ))}
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
