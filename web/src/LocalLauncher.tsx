import { useCallback, useEffect, useState } from "react";
import { local, onLocalEvents, runtimeApi, type LocalUI, type User } from "./api";
import Shell, { Panel, useSection, type NavItem } from "./Shell";
import PathsPage from "./PathsPage";
import DownloadPage from "./DownloadPage";
import PlacesView from "./PlacesView";
import PresetsView from "./PresetsView";
import AccessView from "./AccessView";
import ImagesView from "./ImagesView";
import { applyTheme, currentTheme, type Theme } from "./theme";

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

type Tab = "places" | "images" | "presets" | "secrets";

const SUBTITLE: Record<string, string> = {
  places: "Контейнеры с агентом на этой машине: папка проектов, образ, запуск.",
  images: "Из чего собраны образы рабочих мест и чем отличаются; скачать, пересобрать, удалить.",
  presets: "Наборы инструментов агента: MCP-серверы и скиллы. Пресет выбирается в чате у поля ввода.",
  secrets: "Провайдеры моделей и доступы рабочего места. Значения хранятся в хранилище секретов.",
  paths: "Всё, что agctl держит на этой машине: файлы, порты, контейнеры.",
  download: "Установка agctl на компьютер разработчика.",
};

export type EmbedTarget = { section: Tab; ws: string };

export function embedTarget(): EmbedTarget | null {
  const m = window.location.pathname.match(/^\/launcher\/embed\/(places|presets|secrets)\/?$/);
  if (!m) return null;
  return { section: m[1] as Tab, ws: new URLSearchParams(window.location.search).get("ws") || "" };
}

export default function LocalLauncher({ user, initial, embed }: { user: User; initial: LocalUI; embed?: EmbedTarget | null }) {
  const [ui, setUI] = useState<LocalUI>(initial);
  const NAV: NavItem[] = [
    { id: "places", label: "Рабочие места" },
    { id: "images", label: "Образы" },
    { id: "presets", label: "Пресеты" },
    { id: "secrets", label: "Доступ", warn: !ui.llmKeySet },
    { id: "paths", label: "Где что лежит", group: "Система" },
    { id: "download", label: "Скачать", group: "Система" },
  ];
  const [section, go] = useSection(NAV, initial.llmKeySet ? "places" : "secrets");
  const tab: Tab = embed?.section || (section === "images" || section === "presets" || section === "secrets" ? section : "places");
  const setTab = (t: Tab) => go(t);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const [refreshKey, setRefreshKey] = useState(0);
  const [theme] = useState<Theme>(currentTheme);

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
      {tab === "images" && <ImagesView note={note} refreshKey={refreshKey} />}
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
    <Shell
      items={NAV}
      active={section}
      onSelect={go}
      user={user.login}
      pills={[
        { label: "локальный режим", tone: "data", title: "Платформа работает на этой машине" },
        ...(ui.runtimeKind ? [{ label: ui.runtimeKind === "sbx" ? "docker sandbox" : "docker", tone: "ok" as const, title: "Рантайм рабочего места" }] : []),
      ]}
      title={NAV.find((n) => n.id === section)?.label}
      subtitle={SUBTITLE[section]}
    >
      {section === "paths" && <PathsPage />}
      {section === "download" && <DownloadPage />}
      {(section === "places" || section === "images" || section === "presets" || section === "secrets") && (
        <>
          {section === "places" && <RuntimeSwitch ui={ui} note={note} />}
          {body}
        </>
      )}
    </Shell>
  );
}

// RuntimeSwitch — рантайм рабочего места: контейнер Docker или Docker
// Sandboxes (микро-ВМ). Смена действует со следующего перезапуска места.
function RuntimeSwitch({ ui, note }: { ui: LocalUI; note: (m: string, err?: boolean) => void }) {
  if (!ui.runtimes?.length) return null;
  return (
    <Panel label="Рантайм рабочего места">
      <div className="choice-row">
        {ui.runtimes.map((r) => (
          <button
            key={r.id}
            className={r.id === ui.runtimeKind ? "choice active" : "choice"}
            disabled={!r.available && r.id !== ui.runtimeKind}
            title={r.detail || ""}
            onClick={async () => {
              if (r.id === ui.runtimeKind) return;
              try {
                const res = await runtimeApi.set(r.id);
                note(`Рантайм: ${r.title}${res.restartNeeded ? " — перезапустите рабочее место" : ""}`);
              } catch (e: any) {
                note(e.message, true);
              }
            }}
          >
            <b>{r.title}</b>
            <span className="muted small">{r.available ? r.detail || "доступен" : r.detail || "недоступен на этой машине"}</span>
          </button>
        ))}
      </div>
      {ui.hostPathsAvailable && (
        <label className="inline" title="Папки проектов видны в контейнере по тем же путям, что на этой машине: пути в сессиях агента открываются во внешнем редакторе, его команды можно выполнить на хосте. /workspace остаётся. Применяется при перезапуске места.">
          <input
            type="checkbox"
            checked={!!ui.hostPaths}
            disabled={ui.runtimeKind === "sbx"}
            onChange={async (e) => {
              try {
                const on = e.target.checked;
                await runtimeApi.hostPaths(on);
                note(on ? "Пути как на хосте включены — перезапустите рабочие места" : "Пути как на хосте выключены — перезапустите рабочие места");
              } catch (err: any) {
                note(err.message, true);
              }
            }}
          />{" "}
          пути как на хосте — агент видит папки проектов по тем же путям, что и вы
          {ui.runtimeKind === "sbx" && <span className="muted small"> (только для рантайма docker)</span>}
        </label>
      )}
    </Panel>
  );
}
