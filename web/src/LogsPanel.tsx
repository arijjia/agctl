import { useCallback, useEffect, useRef, useState } from "react";

// LogsPanel — панель просмотра логов контейнера (лаунчер developer'а и
// админ-панель). Сама ничего не знает про API: fetchLogs отдаёт свежий хвост,
// панель умеет автообновление и копирование в буфер.

export type LogsData = { container: string; state: string; logs: string };

export function LogsPanel({ fetchLogs, onClose, autoMs = 5000 }: {
  fetchLogs: () => Promise<LogsData>;
  onClose: () => void;
  autoMs?: number;
}) {
  const [data, setData] = useState<LogsData | null>(null);
  const [err, setErr] = useState("");
  const [auto, setAuto] = useState(true);
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);
  const preRef = useRef<HTMLPreElement>(null);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const d = await fetchLogs();
      setData(d);
      setErr("");
      // Логи дописываются снизу: держим прокрутку на хвосте, если читатель
      // сам не прокрутил вверх (типичный журнал-просмотр).
      const pre = preRef.current;
      if (pre) pre.scrollTop = pre.scrollHeight;
    } catch (e: any) {
      setErr(e.message);
    } finally {
      setBusy(false);
    }
  }, [fetchLogs]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!auto) return undefined;
    const t = setInterval(() => { void load(); }, autoMs);
    return () => clearInterval(t);
  }, [auto, load, autoMs]);

  const copy = async () => {
    if (!data) return;
    const text = data.logs || "";
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // fallback для не-secure контекстов: выделение в скрытой textarea
      const ta = document.createElement("textarea");
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand("copy"); setCopied(true); } catch { /* нет прав */ }
      ta.remove();
      setTimeout(() => setCopied(false), 1500);
    }
  };

  return (
    <div className="logs-panel">
      <div className="toolbar">
        <strong className="mono small">{data?.container || "…"}</strong>
        {data?.state && <span className={`badge state-${data.state}`}>{data.state}</span>}
        <span className="spacer" />
        <label className="row-hint">
          <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} />
          авто {Math.round(autoMs / 1000)}с
        </label>
        <button className="ghost small" onClick={() => void load()} disabled={busy}>{busy ? "…" : "Обновить"}</button>
        <button className="ghost small" onClick={() => void copy()} disabled={!data}>Копировать</button>
        <button className="ghost small" onClick={onClose}>Закрыть</button>
      </div>
      {copied && <p className="muted small">скопировано в буфер</p>}
      {err && <div className="error">{err}</div>}
      <pre ref={preRef} className="logs">{data?.logs || (busy ? "…" : "(пусто)")}</pre>
    </div>
  );
}
