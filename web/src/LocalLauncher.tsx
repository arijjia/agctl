import { useCallback, useEffect, useState } from "react";
import { local, onLocalEvents, type LocalUI, type User } from "./api";
import PlacesView from "./PlacesView";
import PresetsView from "./PresetsView";
import AccessView from "./AccessView";
import { applyTheme, currentTheme, toggleTheme, type Theme } from "./theme";

// LocalLauncher — лаунчер локального режима (agctl dev), v2: рабочие места
// (несколько контейнеров со своими папками), модели и ключи, доступы.
// Писать состояние может только сервер; все открытые копии (вкладки
// браузера и табы правой панели dsh) получают изменения из потока событий и
// перечитывают данные сами.
//
// Встроенный режим /launcher/embed/<раздел>?ws=<id> — тот же интерфейс без
// шапки, для табов правой панели dsh: раздел places (текущее рабочее место),
// presets, secrets.

const brand = (window as any).__AGCTL_BRAND__?.name || "agctl";

type Tab = "places" | "presets" | "secrets";

export type EmbedTarget = { section: Tab; ws: string };

export function embedTarget(): EmbedTarget | null {
  const m = window.location.pathname.match(/^\/launcher\/embed\/(places|presets|secrets)\/?$/);
  if (!m) return null;
  return { section: m[1] as Tab, ws: new URLSearchParams(window.location.search).get("ws") || "" };
}

export default function LocalLauncher({ user, initial, embed }: { user: User; initial: LocalUI; embed?: EmbedTarget | null }) {
  const [ui, setUI] = useState<LocalUI>(initial);
  const [tab, setTab] = useState<Tab>(embed?.section || (initial.llmKeySet ? "places" : "secrets"));
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const [refreshKey, setRefreshKey] = useState(0);
  const [theme, setTheme] = useState<Theme>(currentTheme);

  // Тема применяется и во встроенном режиме: таб dsh наследует выбор из
  // localStorage (тот же origin), что и полная вкладка лаунчера.
  useEffect(() => applyTheme(theme), [theme]);

  const load = useCallback(() => {
    local.ui().then(setUI).catch((e) => setErr(e.message));
  }, []);
  // Живые обновления: изменение из другой вкладки, таба dsh или CLI.
  useEffect(() => {
    const off = onLocalEvents(() => {
      load();
      setRefreshKey((k) => k + 1);
    });
    return off;
  }, [load]);

  const note = (m: string, isErr?: boolean) => {
    if (isErr) {
      setErr(m);
      setMsg("");
    } else {
      setMsg(m);
      setErr("");
    }
    load();
    setRefreshKey((k) => k + 1);
  };

  const body = (
    <>
      {!ui.llmKeySet && tab !== "secrets" && (
        <div className="notice">
          Ключ модели не задан — агент не сможет отвечать.
          {!embed && (
            <button className="primary small" onClick={() => setTab("secrets")}>
              Задать во вкладке «Доступ»
            </button>
          )}
        </div>
      )}
      {msg && <p className="muted small">{msg}</p>}
      {err && <div className="error">{err}</div>}
      {tab === "places" && <PlacesView ui={ui} note={note} refreshKey={refreshKey} only={embed?.ws || undefined} />}
      {tab === "presets" && <PresetsView note={note} refreshKey={refreshKey} />}
      {tab === "secrets" && <AccessView ui={ui} note={note} refreshKey={refreshKey} />}
    </>
  );

  if (embed) {
    return (
      <div className="embed-shell">
        {body}
        <p className="muted small embed-foot">
          <a href="/launcher" target="_blank" rel="noreferrer">
            Открыть полный лаунчер →
          </a>
        </p>
      </div>
    );
  }

  return (
    <div className="admin-shell">
      <header className="admin-header">
        <div>
          <h1>{brand} — рабочие места на этой машине</h1>
          <p className="muted small">
            {user.login} · локальный режим · профиль {ui.profile}
          </p>
        </div>
        <button
          className="small"
          onClick={() => setTheme(toggleTheme(theme))}
          title="Переключить светлую/тёмную тему"
        >
          {theme === "dark" ? "☀️ Светлая" : "🌙 Тёмная"}
        </button>
      </header>
      <nav className="tabs admin-tabs">
        <button className={tab === "places" ? "tab active" : "tab"} onClick={() => setTab("places")}>
          Рабочие места
        </button>
        <button className={tab === "presets" ? "tab active" : "tab"} onClick={() => setTab("presets")}>
          Пресеты
        </button>
        <button className={tab === "secrets" ? "tab active" : "tab"} onClick={() => setTab("secrets")}>
          Доступ{!ui.llmKeySet && " ⚠"}
        </button>
      </nav>
      <main className="admin-main">{body}</main>
    </div>
  );
}
