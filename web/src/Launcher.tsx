import { useCallback, useEffect, useState } from "react";
import { launcher, api, type User, type SlotInfo } from "./api";
import { LogsPanel } from "./LogsPanel";

// Launcher — лаунчер роли developer (карточка developer-launcher-images):
// после входа контейнер НЕ стартует автоматически; developer выбирает образ
// (каталог сервера, например Nexus), видит свои контейнеры и управляет ими.
// Мульти-рантайм (карточка developer-multi-containers): слоты 0..max-1 —
// параллельные контейнеры с разными образами; активный слот открывается по
// /chat/ и чей workspace видят проектные API шлюза. Оформление — то же, что
// у админ-панели (admin-shell).

type ImagesResp = { images: string[]; default: string; current: string };
type ContainerRow = {
  name: string;
  workspace: string;
  image: string;
  state: string;
  status: string;
  ssh: boolean;
  slot?: number;
};

export default function Launcher({ user }: { user: User }) {
  const [images, setImages] = useState<ImagesResp | null>(null);
  const [slots, setSlots] = useState<SlotInfo[]>([]);
  const [slotMax, setSlotMax] = useState(0);
  const [slotLive, setSlotLive] = useState(0);
  const [containers, setContainers] = useState<ContainerRow[]>([]);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [busySlot, setBusySlot] = useState(-1);
  const [logsFor, setLogsFor] = useState("");
  const [logsOpen, setLogsOpen] = useState(false);

  const load = useCallback(() => {
    launcher.images().then((r) => setImages(r)).catch((e) => setErr(e.message));
    launcher.slots().then((r) => {
      setSlots(r.slots || []);
      setSlotMax(r.max);
      setSlotLive(r.live);
    }).catch((e) => setErr(e.message));
    launcher.containers().then((r) => setContainers(r.containers || [])).catch(() => setContainers([]));
  }, []);
  useEffect(load, [load]);

  const slotOp = async (slot: number, action: "start" | "stop" | "activate", image?: string) => {
    setErr("");
    setMsg("");
    setBusySlot(slot);
    try {
      if (action === "start") {
        await launcher.slotStart(slot);
        setMsg(`Слот ${slot} запускается…`);
        setTimeout(load, 1500);
        setTimeout(load, 4000);
      } else if (action === "stop") {
        await launcher.slotStop(slot);
        setMsg(`Слот ${slot} остановлен, данные на томе сохранены`);
        setTimeout(load, 500);
      } else if (action === "activate") {
        await launcher.slotActivate(slot);
        setMsg(`Слот ${slot} активен — /chat/ откроет его`);
        load();
      } else if (image !== undefined) {
        await launcher.slotImage(slot, image);
        setMsg(`Образ слота ${slot}: ${image || "(дефолт сервера)"}`);
        load();
      }
    } catch (e: any) {
      setErr(e.message);
    } finally {
      setBusySlot(-1);
    }
  };

  const activeSlot = slots.find((s) => s.active);
  const fetchLogs = useCallback(() => launcher.logs(logsFor, 300), [logsFor]);

  return (
    <div className="admin-shell">
      <header className="admin-header">
        <div>
          <h1>{(window as any).__AGCTL_BRAND__?.name || "agctl"} — лаунчер разработчика</h1>
          <p className="muted small">
            {user.login} · роль developer · контейнер не запускается, пока вы не
            выбрали образ и не нажали «Запустить» · слотов: {slotLive}/{slotMax}
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
        <button className="tab active">Рантаймы</button>
      </nav>
      <main className="admin-main">
        <h2 className="small">Слоты (параллельные рантаймы с разными образами)</h2>
        <table className="admin-table">
          <thead><tr><th>Слот</th><th>Образ следующего старта</th><th>Состояние</th><th>Запущенный образ</th><th></th></tr></thead>
          <tbody>
            {slots.map((s) => (
              <tr key={s.slot} className={s.active ? "slot-active" : ""}>
                <td>
                  {s.slot} {s.active && <span className="badge">активный</span>}
                </td>
                <td>
                  <select
                    value={s.image || ""}
                    disabled={busySlot === s.slot}
                    onChange={(e) => slotOp(s.slot, "image", e.target.value)}
                  >
                    <option value="">(дефолт сервера: {images?.default || "—"})</option>
                    {(images?.images || []).map((img) => (
                      <option key={img} value={img}>{img}</option>
                    ))}
                  </select>
                </td>
                <td><span className={`badge state-${s.state}`}>{s.state}</span></td>
                <td className="muted small">{s.runningImage || "—"}</td>
                <td className="actions">
                  {s.state !== "running" && (
                    <button
                      className="primary small"
                      disabled={busySlot === s.slot}
                      onClick={() => slotOp(s.slot, "start")}
                    >
                      {busySlot === s.slot ? "…" : "Запустить"}
                    </button>
                  )}
                  {s.state === "running" && (
                    <button
                      className="ghost small"
                      disabled={busySlot === s.slot}
                      onClick={() => slotOp(s.slot, "stop")}
                    >
                      Остановить
                    </button>
                  )}
                  {!s.active && (
                    <button
                      className="ghost small"
                      disabled={busySlot === s.slot}
                      onClick={() => slotOp(s.slot, "activate")}
                      title="Этот слот будет открываться по /chat/"
                    >
                      Сделать активным
                    </button>
                  )}
                  {s.active && s.state === "running" && (
                    <a href="/chat/" className="ghost link">Открыть чат →</a>
                  )}
                </td>
              </tr>
            ))}
            {!slots.length && <tr><td colSpan={5} className="muted">слоты загружаются…</td></tr>}
          </tbody>
        </table>
        <p className="muted small">
          Активный слот открывается по /chat/; его workspace видят проектные API
          шлюза. Том у каждого слота свой: обмен файлами — через git.
          {" "}{activeSlot?.state === "running" && <a href="/chat/" className="link">Открыть чат активного слота →</a>}
        </p>
        {msg && <p className="muted small">{msg}</p>}
        {err && <div className="error">{err}</div>}

        <h2 className="small">Мои контейнеры</h2>
        <table className="admin-table">
          <thead><tr><th>Имя</th><th>Слот</th><th>Образ</th><th>Состояние</th><th>Статус</th><th>SSH</th><th></th></tr></thead>
          <tbody>
            {containers.map((c) => (
              <tr key={c.name}>
                <td className="mono small">{c.name}</td>
                <td className="muted small">{c.slot ?? 0}</td>
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
            {!containers.length && <tr><td colSpan={7} className="muted">контейнеров нет — запустите слот выше</td></tr>}
          </tbody>
        </table>

        {logsOpen && (
          <LogsPanel fetchLogs={fetchLogs} onClose={() => setLogsOpen(false)} />
        )}
      </main>
    </div>
  );
}
