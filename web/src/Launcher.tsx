import { useCallback, useEffect, useState } from "react";
import { launcher, api, type User } from "./api";
import { LogsPanel } from "./LogsPanel";

// Launcher — лаунчер роли developer (карточка developer-launcher-images):
// после входа контейнер НЕ стартует автоматически; developer выбирает образ
// (каталог сервера, например Nexus), видит свои контейнеры и управляет ими.
// Оформление — то же, что у админ-панели (admin-shell): без рамки-карточки,
// тот же фон/шрифты/заголовок/ширина области.

type ImagesResp = { images: string[]; default: string; current: string };
type ContainerRow = {
  name: string;
  workspace: string;
  image: string;
  state: string;
  status: string;
  ssh: boolean;
};

export default function Launcher({ user }: { user: User }) {
  const [images, setImages] = useState<ImagesResp | null>(null);
  const [selected, setSelected] = useState("");
  const [containers, setContainers] = useState<ContainerRow[]>([]);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [logsFor, setLogsFor] = useState("");
  const [logsOpen, setLogsOpen] = useState(false);

  const load = useCallback(() => {
    launcher.images().then((r) => {
      setImages(r);
      setSelected(r.current || r.default);
    }).catch((e) => setErr(e.message));
    launcher.containers().then((r) => setContainers(r.containers || [])).catch(() => setContainers([]));
  }, []);
  useEffect(load, [load]);

  const pick = async (image: string) => {
    setErr("");
    setMsg("");
    try {
      await launcher.pickImage(image);
      setSelected(image);
      setMsg(`Образ для следующего старта: ${image || "(дефолт сервера)"}`);
    } catch (e: any) {
      setErr(e.message);
    }
  };

  const op = async (action: "start" | "stop") => {
    setErr("");
    setMsg("");
    setBusy(true);
    try {
      if (action === "start") {
        await launcher.start();
        setMsg("Рантайм запускается…");
        // прогрев ~2 c: пару раз дёрнем список
        setTimeout(load, 1500);
        setTimeout(load, 4000);
      } else {
        await launcher.stop();
        setMsg("Рантайм остановлен, данные на томе сохранены");
        setTimeout(load, 500);
      }
    } catch (e: any) {
      setErr(e.message);
    } finally {
      setBusy(false);
    }
  };

  const running = containers.some((c) => c.state === "running");
  const fetchLogs = useCallback(() => launcher.logs(logsFor, 300), [logsFor]);

  return (
    <div className="admin-shell">
      <header className="admin-header">
        <div>
          <h1>{(window as any).__AGCTL_BRAND__?.name || "agctl"} — лаунчер разработчика</h1>
          <p className="muted small">
            {user.login} · роль developer · контейнер не запускается, пока вы не
            выбрали образ и не нажали «Запустить»
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
        <button className="tab active">Рантайм</button>
      </nav>
      <main className="admin-main">
        <h2 className="small">Образ запуска</h2>
        <div className="image-list">
          {(images?.images || []).map((img) => (
            <label key={img} className="image-option">
              <input
                type="radio"
                name="image"
                checked={selected === img}
                onChange={() => pick(img)}
              />
              <span>
                <span className="mono">{img}</span>
                {img.includes("nossh") && (
                  <span className="muted small"> — без SSH-канала</span>
                )}
                {img === images?.default && <span className="muted small"> — дефолт</span>}
              </span>
            </label>
          ))}
          {(!images || !images.images.length) && <p className="muted small">каталог образов пуст</p>}
        </div>

        <div className="toolbar">
          <button className="primary" disabled={busy} onClick={() => op("start")}>
            {busy ? "…" : "Запустить"}
          </button>
          <button className="ghost" disabled={busy || !running} onClick={() => op("stop")}>
            Остановить
          </button>
          <a href="/chat/" className="ghost link" aria-disabled={!running}>
            Открыть чат →
          </a>
        </div>
        {msg && <p className="muted small">{msg}</p>}
        {err && <div className="error">{err}</div>}

        <h2 className="small">Мои контейнеры</h2>
        <table className="admin-table">
          <thead><tr><th>Имя</th><th>Образ</th><th>Состояние</th><th>Статус</th><th>SSH</th><th></th></tr></thead>
          <tbody>
            {containers.map((c) => (
              <tr key={c.name}>
                <td className="mono small">{c.name}</td>
                <td className="muted small">{c.image}</td>
                <td><span className={`badge state-${c.state}`}>{c.state}</span></td>
                <td className="muted small">{c.status}</td>
                <td>{c.ssh ? "2222 (loopback)" : "—"}</td>
                <td className="actions">
                  <button
                    className="ghost small"
                    onClick={() => { setLogsFor(c.name); setLogsOpen(true); }}
                  >
                    Логи
                  </button>
                </td>
              </tr>
            ))}
            {!containers.length && <tr><td colSpan={6} className="muted">контейнеров нет — нажмите «Запустить»</td></tr>}
          </tbody>
        </table>

        {logsOpen && (
          <LogsPanel fetchLogs={fetchLogs} onClose={() => setLogsOpen(false)} />
        )}
      </main>
    </div>
  );
}
