import { useCallback, useEffect, useState } from "react";
import { launcher, local, type LocalEntry, type LocalUI, type User } from "./api";
import { LogsPanel } from "./LogsPanel";

// LocalLauncher — лаунчер разработчика в локальном режиме (agctl dev):
// образ рабочего места (каталог профиля, с отметкой «есть на этой машине»;
// недостающий скачивается или собирается здесь же, с прогрессом) и доступы —
// ключ модели, GitLab, Jira. Значения секретов сервер не возвращает: только
// «задан / не задан»; несекретное (адреса, логины) показывается как есть.

const brand = (window as any).__AGCTL_BRAND__?.name || "agctl";

const groupTitle: Record<string, string> = {
  llm: "Модель (LLM)",
  gitlab: "GitLab",
  jira: "Jira",
  other: "Другие переменные",
};

function flavorTitle(f?: string) {
  if (f === "1c") return "1С: Java 21 + BSL Language Server";
  if (f === "web") return "Веб: Playwright + Chromium";
  return "Базовый: dsh, git, glab, jira-cli, uv";
}

function SecretRow({ e, onSaved }: { e: LocalEntry; onSaved: (m: string, err?: boolean) => void }) {
  const [val, setVal] = useState("");
  const [busy, setBusy] = useState(false);
  const save = async () => {
    setBusy(true);
    try {
      await local.setSecret(e.name, val, !e.secret);
      setVal("");
      onSaved(`${e.title}: сохранено`);
    } catch (err: any) {
      onSaved(err.message, true);
    } finally {
      setBusy(false);
    }
  };
  const del = async () => {
    if (!confirm(`Удалить ${e.name}?`)) return;
    setBusy(true);
    try {
      await local.deleteSecret(e.name);
      onSaved(`${e.title}: удалено`);
    } catch (err: any) {
      onSaved(err.message, true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="secret-row">
      <div className="secret-head">
        <span>
          <b>{e.title}</b> <span className="mono muted small">{e.name}</span>
        </span>
        <span className={`badge ${e.set ? "state-running" : "state-exited"}`}>
          {e.set ? (e.secret ? "задан" : "задано") : "не задан"}
        </span>
      </div>
      {e.hint && <div className="muted small">{e.hint}</div>}
      {!e.secret && e.value && <div className="mono small">{e.value}</div>}
      <form
        className="secret-form"
        onSubmit={(ev) => {
          ev.preventDefault();
          if (val.trim()) save();
        }}
      >
        <input
          type={e.secret ? "password" : "text"}
          autoComplete="off"
          placeholder={e.secret ? (e.set ? "новое значение (заменит текущее)" : "вставьте значение") : e.value || "значение"}
          value={val}
          onChange={(ev) => setVal(ev.target.value)}
        />
        <button className="primary" disabled={busy || !val.trim()}>
          Сохранить
        </button>
        {e.set && (
          <button type="button" className="ghost danger" disabled={busy} onClick={del}>
            Удалить
          </button>
        )}
      </form>
    </div>
  );
}

export default function LocalLauncher({ user, initial }: { user: User; initial: LocalUI }) {
  const [ui, setUI] = useState<LocalUI>(initial);
  const [tab, setTab] = useState<"runtime" | "secrets">(initial.llmKeySet ? "runtime" : "secrets");
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const [containers, setContainers] = useState<any[]>([]);
  const [logsOpen, setLogsOpen] = useState(false);
  const [newName, setNewName] = useState("");
  const [newVal, setNewVal] = useState("");
  const [newPlain, setNewPlain] = useState(false);

  const load = useCallback(() => {
    local.ui().then(setUI).catch((e) => setErr(e.message));
    launcher.containers().then((r) => setContainers(r.containers || [])).catch(() => setContainers([]));
  }, []);
  const busy = ui.task.state === "building" || ui.task.state === "starting";
  useEffect(() => {
    load();
    const t = setInterval(load, busy ? 2000 : 8000);
    return () => clearInterval(t);
  }, [load, busy]);

  const note = (m: string, isErr?: boolean) => {
    if (isErr) {
      setErr(m);
      setMsg("");
    } else {
      setMsg(m);
      setErr("");
    }
    load();
  };

  const pick = async (ref: string) => {
    try {
      await local.setImage(ref);
      note(`Образ рабочего места: ${ref}`);
    } catch (e: any) {
      note(e.message, true);
    }
  };
  const run = async (what: "start" | "restart" | "stop") => {
    try {
      if (what === "start") await local.start();
      else if (what === "restart") await local.restart();
      else await launcher.stop();
      note(what === "stop" ? "Рабочее место остановлено, данные сохранены" : "Готовлю рабочее место…");
    } catch (e: any) {
      note(e.message, true);
    }
  };

  const running = ui.runtime.state === "running";
  const current = ui.images.find((i) => i.ref === ui.current);
  const groups = ["llm", "gitlab", "jira", "other"]
    .map((g) => ({ g, items: ui.secrets.filter((s) => s.group === g) }))
    .filter((x) => x.items.length);
  const fetchLogs = useCallback(() => launcher.logs("", 300), []);

  return (
    <div className="admin-shell">
      <header className="admin-header">
        <div>
          <h1>{brand} — рабочее место на этой машине</h1>
          <p className="muted small">
            {user.login} · локальный режим · папка проектов <span className="mono">{ui.workdir}</span> (в контейнере —{" "}
            <span className="mono">/workspace</span>)
          </p>
        </div>
        <a href="/chat/" className="button primary" aria-disabled={!running}>
          Открыть чат →
        </a>
      </header>
      <nav className="tabs admin-tabs">
        <button className={tab === "runtime" ? "tab active" : "tab"} onClick={() => setTab("runtime")}>
          Рабочее место
        </button>
        <button className={tab === "secrets" ? "tab active" : "tab"} onClick={() => setTab("secrets")}>
          Доступы{!ui.llmKeySet && " ⚠"}
        </button>
      </nav>
      <main className="admin-main">
        {!ui.llmKeySet && (
          <div className="notice">
            Ключ модели не задан — агент не сможет отвечать. Задайте его во вкладке «Доступы».
          </div>
        )}
        {ui.task.restartNeeded && (
          <div className="notice">
            Доступы или образ изменены — рабочее место работает на старых. {" "}
            <button className="primary small" disabled={busy} onClick={() => run("restart")}>
              Перезапустить
            </button>
          </div>
        )}
        {msg && <p className="muted small">{msg}</p>}
        {err && <div className="error">{err}</div>}

        {tab === "runtime" && (
          <>
            <h2 className="small">Образ рабочего места</h2>
            <div className="image-list">
              {ui.images.map((im) => (
                <label key={im.ref} className="image-option">
                  <input type="radio" name="image" checked={ui.current === im.ref} disabled={busy} onChange={() => pick(im.ref)} />
                  <span>
                    <span>{flavorTitle(im.flavor)}</span>
                    <br />
                    <span className="mono muted small">{im.ref}</span>{" "}
                    {im.present ? (
                      <span className="badge state-running">есть на машине</span>
                    ) : (
                      <span className="badge state-exited">скачается или соберётся при запуске (10–20 мин)</span>
                    )}
                  </span>
                </label>
              ))}
            </div>

            <div className="toolbar">
              {!running ? (
                <button className="primary" disabled={busy} onClick={() => run("start")}>
                  {busy ? "…" : current && !current.present ? "Подготовить и запустить" : "Запустить"}
                </button>
              ) : (
                <button className="primary" disabled={busy} onClick={() => run("restart")}>
                  Перезапустить
                </button>
              )}
              <button className="ghost" disabled={busy || !running} onClick={() => run("stop")}>
                Остановить
              </button>
              <button className="ghost" onClick={() => setLogsOpen(true)} disabled={!containers.length}>
                Логи контейнера
              </button>
              <span className="muted small">
                {busy
                  ? ui.task.state === "building"
                    ? "готовлю образ…"
                    : "запускаю…"
                  : running
                    ? `работает на ${ui.runtime.image}`
                    : ui.task.state === "error"
                      ? "ошибка — см. ниже"
                      : "остановлено"}
              </span>
            </div>
            {(busy || ui.task.state === "error" || ui.task.log.length > 0) && (
              <pre className="task-log">
                {ui.task.log.join("\n")}
                {ui.task.error && `\n\nОшибка: ${ui.task.error}`}
                {busy && "\n…"}
              </pre>
            )}
            {logsOpen && <LogsPanel fetchLogs={fetchLogs} onClose={() => setLogsOpen(false)} />}
          </>
        )}

        {tab === "secrets" && (
          <>
            <p className="muted small">
              Секреты хранятся в: {ui.storage}. Значения сюда не возвращаются — видно только, задан ли доступ.
              Изменения попадают в рабочее место при его перезапуске; ключ модели — тоже.
            </p>
            {groups.map(({ g, items }) => (
              <section key={g}>
                <h2 className="small">{groupTitle[g] || g}</h2>
                {items.map((e) => (
                  <SecretRow key={e.name} e={e} onSaved={note} />
                ))}
              </section>
            ))}
            <h2 className="small">Добавить переменную</h2>
            <form
              className="secret-form"
              onSubmit={async (ev) => {
                ev.preventDefault();
                try {
                  await local.setSecret(newName.trim().toUpperCase(), newVal, newPlain);
                  setNewName("");
                  setNewVal("");
                  note(`${newName.trim().toUpperCase()}: сохранено`);
                } catch (e: any) {
                  note(e.message, true);
                }
              }}
            >
              <input placeholder="ИМЯ_ПЕРЕМЕННОЙ" value={newName} onChange={(e) => setNewName(e.target.value)} />
              <input
                type={newPlain ? "text" : "password"}
                autoComplete="off"
                placeholder="значение"
                value={newVal}
                onChange={(e) => setNewVal(e.target.value)}
              />
              <label className="inline">
                <input type="checkbox" checked={newPlain} onChange={(e) => setNewPlain(e.target.checked)} /> не секрет
              </label>
              <button className="primary" disabled={!newName.trim() || !newVal.trim()}>
                Добавить
              </button>
            </form>
          </>
        )}
      </main>
    </div>
  );
}
