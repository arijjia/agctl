import { useCallback, useEffect, useRef, useState } from "react";
import { api, AuditEvent, AdminUser, User, Workspace, WsStatus } from "./api";

// ── статусы ──────────────────────────────────────────────────────────────────

const STATUS_LABEL: Record<WsStatus, string> = {
  provisioning: "создаётся…",
  running: "работает",
  stopped: "остановлен",
  stopping: "останавливается…",
  failed: "ошибка",
  removing: "удаляется…",
};

function StatusBadge({ status }: { status: WsStatus }) {
  return (
    <span className={`badge badge-${status}`}>
      {status === "provisioning" || status === "stopping" || status === "removing" ? (
        <span className="spinner" />
      ) : null}
      {STATUS_LABEL[status] ?? status}
    </span>
  );
}

// ── вход ─────────────────────────────────────────────────────────────────────

function Login({ onDone }: { onDone: (u: User) => void }) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      const res = mode === "login" ? await api.login(login, password) : await api.register(login, password);
      onDone(res.user);
    } catch (err: any) {
      setError(err.message || String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="center-wrap">
      <form className="card login-card" onSubmit={submit}>
        <h1>agctl</h1>
        <p className="muted">Рабочие места агентов: контейнер + репка одной кнопкой</p>
        <div className="tabs">
          <button type="button" className={mode === "login" ? "tab active" : "tab"} onClick={() => setMode("login")}>
            Вход
          </button>
          <button type="button" className={mode === "register" ? "tab active" : "tab"} onClick={() => setMode("register")}>
            Регистрация
          </button>
        </div>
        <label>
          Логин
          <input value={login} onChange={(e) => setLogin(e.target.value)} autoFocus minLength={3} required />
        </label>
        <label>
          Пароль
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} minLength={6} required />
        </label>
        {error && <div className="error">{error}</div>}
        <button className="primary" disabled={busy}>
          {busy ? "…" : mode === "login" ? "Войти" : "Создать аккаунт"}
        </button>
        <p className="muted small">Первый зарегистрированный пользователь становится администратором.</p>
      </form>
    </div>
  );
}

// ── карточка воркспейса ──────────────────────────────────────────────────────

function WorkspaceCard({
  ws,
  showOwner,
  onChanged,
}: {
  ws: Workspace;
  showOwner: boolean;
  onChanged: () => void;
}) {
  const [logs, setLogs] = useState<string | null>(null);
  const [error, setError] = useState("");

  const act = async (fn: () => Promise<unknown>) => {
    setError("");
    try {
      await fn();
      onChanged();
    } catch (err: any) {
      setError(err.message || String(err));
    }
  };

  const toggleLogs = async () => {
    if (logs !== null) {
      setLogs(null);
      return;
    }
    try {
      const res = await api.logs(ws.id);
      setLogs(res.logs || "(пусто)");
    } catch (err: any) {
      setLogs("(ошибка: " + (err.message || err) + ")");
    }
  };

  return (
    <div className="card ws-card">
      <div className="ws-head">
        <div>
          <span className="ws-name">{ws.name}</span>
          {showOwner && <span className="muted"> · {ws.owner}</span>}
          <div className="muted small mono">{ws.id}</div>
        </div>
        <StatusBadge status={ws.status} />
      </div>
      {ws.error && <div className="error">{ws.error}</div>}
      <div className="ws-actions">
        {ws.status === "running" && (
          <a className="button primary" href={ws.url} target="_blank" rel="noreferrer">
            Открыть
          </a>
        )}
        {ws.status === "running" && (
          <button onClick={() => act(() => api.stop(ws.id))}>Стоп</button>
        )}
        {ws.status === "stopped" && (
          <button className="primary" onClick={() => act(() => api.start(ws.id))}>
            Старт
          </button>
        )}
        {ws.status === "failed" && (
          <button className="primary" onClick={() => act(() => api.start(ws.id))}>
            Повторить
          </button>
        )}
        {(ws.status === "stopped" || ws.status === "failed") && (
          <button className="danger" onClick={() => act(() => api.remove(ws.id))}>
            Удалить
          </button>
        )}
        <button onClick={toggleLogs}>{logs === null ? "Логи" : "Скрыть логи"}</button>
      </div>
      {logs !== null && <pre className="logs">{logs}</pre>}
    </div>
  );
}

// ── личный кабинет ───────────────────────────────────────────────────────────

function Cabinet({ user, onLogout, onAdmin }: { user: User; onLogout: () => void; onAdmin: () => void }) {
  const [wss, setWss] = useState<Workspace[]>([]);
  const [name, setName] = useState("");
  const [baseURL, setBaseURL] = useState("https://litellm-dev.utmn.ru/v1");
  const [model, setModel] = useState("qwen-3.8-deep");
  const [apiKey, setApiKey] = useState("");
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const timer = useRef<number | null>(null);

  const refresh = useCallback(async () => {
    try {
      const res = await api.workspaces();
      setWss(res.workspaces);
      return res.workspaces;
    } catch {
      return [];
    }
  }, []);

  useEffect(() => {
    let stopped = false;
    const tick = async () => {
      const list = await refresh();
      if (stopped) return;
      const busy = list.some((w) => w.status === "provisioning" || w.status === "removing" || w.status === "stopping");
      timer.current = window.setTimeout(tick, busy ? 2500 : 6000);
    };
    tick();
    return () => {
      stopped = true;
      if (timer.current) window.clearTimeout(timer.current);
    };
  }, [refresh]);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setCreating(true);
    try {
      await api.create(name, baseURL, model, apiKey);
      setName("");
      setApiKey("");
      await refresh();
    } catch (err: any) {
      setError(err.message || String(err));
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="page">
      <header className="topbar">
        <span className="brand">agctl</span>
        <span className="spacer" />
        <span className="muted">
          {user.login} · {user.role}
        </span>
        {user.role === "admin" && (
          <button onClick={onAdmin}>Админка</button>
        )}
        <button onClick={onLogout}>Выйти</button>
      </header>

      <form className="card create-card" onSubmit={create}>
        <h2>Новое рабочее место</h2>
        <div className="grid">
          <label>
            Имя
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="моё-рабочее-место" required minLength={2} />
          </label>
          <label>
            Base URL провайдера
            <input value={baseURL} onChange={(e) => setBaseURL(e.target.value)} className="mono" required />
          </label>
          <label>
            Модель
            <input value={model} onChange={(e) => setModel(e.target.value)} className="mono" required />
          </label>
          <label>
            API-токен
            <input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="токен LLM-провайдера"
              required
            />
          </label>
        </div>
        {error && <div className="error">{error}</div>}
        <button className="primary" disabled={creating}>
          {creating ? "создаём…" : "Создать рабочее место"}
        </button>
        <p className="muted small">
          Токен хранится зашифрованным и передаётся только в ваш контейнер. В репку и логи он не попадает.
        </p>
      </form>

      <h2>Мои воркспейсы</h2>
      {wss.length === 0 && <p className="muted">Пока пусто — создайте первое рабочее место.</p>}
      {wss.map((w) => (
        <WorkspaceCard key={w.id} ws={w} showOwner={false} onChanged={refresh} />
      ))}
    </div>
  );
}

// ── админка ──────────────────────────────────────────────────────────────────

function Admin({ user, onBack }: { user: User; onBack: () => void }) {
  const [tab, setTab] = useState<"ws" | "users" | "audit">("ws");
  const [wss, setWss] = useState<Workspace[]>([]);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [events, setEvents] = useState<AuditEvent[]>([]);

  const refresh = useCallback(async () => {
    try {
      if (tab === "ws") setWss((await api.workspaces()).workspaces);
      if (tab === "users") setUsers((await api.adminUsers()).users);
      if (tab === "audit") setEvents((await api.adminAudit()).events);
    } catch {
      /* 401/403 — бэкенд сам разберётся */
    }
  }, [tab]);

  useEffect(() => {
    refresh();
    const t = window.setInterval(refresh, tab === "ws" ? 3000 : 10000);
    return () => window.clearInterval(t);
  }, [refresh, tab]);

  return (
    <div className="page">
      <header className="topbar">
        <span className="brand">agctl · админка</span>
        <span className="spacer" />
        <span className="muted">{user.login}</span>
        <button onClick={onBack}>← Кабинет</button>
      </header>

      <div className="tabs left">
        <button className={tab === "ws" ? "tab active" : "tab"} onClick={() => setTab("ws")}>
          Воркспейсы
        </button>
        <button className={tab === "users" ? "tab active" : "tab"} onClick={() => setTab("users")}>
          Пользователи
        </button>
        <button className={tab === "audit" ? "tab active" : "tab"} onClick={() => setTab("audit")}>
          Аудит
        </button>
      </div>

      {tab === "ws" &&
        (wss.length === 0 ? (
          <p className="muted">Воркспейсов нет.</p>
        ) : (
          wss.map((w) => <WorkspaceCard key={w.id} ws={w} showOwner onChanged={refresh} />)
        ))}

      {tab === "users" && (
        <table className="table">
          <thead>
            <tr>
              <th>Логин</th>
              <th>Роль</th>
              <th>Создан</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id}>
                <td>{u.login}</td>
                <td>{u.role}</td>
                <td className="mono small">{u.createdAt}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {tab === "audit" && (
        <table className="table">
          <thead>
            <tr>
              <th>Время</th>
              <th>Кто</th>
              <th>Действие</th>
              <th>Объект</th>
              <th>Детали</th>
            </tr>
          </thead>
          <tbody>
            {events.map((e, i) => (
              <tr key={i}>
                <td className="mono small">{e.ts}</td>
                <td>{e.actor}</td>
                <td className="mono small">{e.action}</td>
                <td className="mono small">{e.target}</td>
                <td className="small">{e.details}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

// ── корень ───────────────────────────────────────────────────────────────────

export default function App() {
  const [user, setUser] = useState<User | null>(null);
  const [view, setView] = useState<"main" | "admin">("main");
  const [checking, setChecking] = useState(true);

  useEffect(() => {
    api
      .me()
      .then((res) => setUser(res.user))
      .catch(() => setUser(null))
      .finally(() => setChecking(false));
  }, []);

  if (checking) {
    return (
      <div className="center-wrap">
        <span className="spinner big" />
      </div>
    );
  }
  if (!user) {
    return (
      <Login
        onDone={(u) => {
          setUser(u);
          setView("main");
        }}
      />
    );
  }
  const logout = async () => {
    await api.logout();
    setUser(null);
    setView("main");
  };
  if (view === "admin" && user.role === "admin") {
    return <Admin user={user} onBack={() => setView("main")} />;
  }
  return <Cabinet user={user} onLogout={logout} onAdmin={() => setView("admin")} />;
}
