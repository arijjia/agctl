import { useCallback, useEffect, useState } from "react";
import { local, places, type ModelProvider } from "./api";

// ModelsView — провайдеры и модели платформы (профиль организации + свои
// ключи): какие модели доступны агенту, какая по умолчанию, задан ли ключ.
// Ключ хранится в доступах (хранилище ОС) и попадает в .credentials.yaml
// рабочих мест. Сравнение моделей по умности на уровнях усилий — следующий
// этап лаунчера.

function KeyForm({ p, onSaved }: { p: ModelProvider; onSaved: (m: string, err?: boolean) => void }) {
  const [val, setVal] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <form
      className="secret-form"
      onSubmit={async (ev) => {
        ev.preventDefault();
        setBusy(true);
        try {
          await local.setSecret(p.keyEnv, val.trim(), false);
          setVal("");
          onSaved(`Ключ ${p.id}: сохранён`);
        } catch (e: any) {
          onSaved(e.message, true);
        } finally {
          setBusy(false);
        }
      }}
    >
      <input
        type="password"
        autoComplete="off"
        placeholder={p.keySet ? "новый ключ (заменит текущий)" : "вставьте ключ провайдера"}
        value={val}
        onChange={(e) => setVal(e.target.value)}
      />
      <button className="primary" disabled={busy || !val.trim()}>
        Сохранить ключ
      </button>
    </form>
  );
}

export default function ModelsView({ note, refreshKey }: { note: (m: string, err?: boolean) => void; refreshKey: number }) {
  const [providers, setProviders] = useState<ModelProvider[] | null>(null);
  const [err, setErr] = useState("");
  const load = useCallback(() => {
    places
      .models()
      .then((r) => setProviders(r.providers))
      .catch((e) => setErr(e.message));
  }, []);
  useEffect(load, [load, refreshKey]);
  const saved = (m: string, isErr?: boolean) => {
    note(m, isErr);
    load();
  };
  if (err) return <div className="error">{err}</div>;
  if (!providers) return <span className="spinner" />;
  if (!providers.length) {
    return (
      <p className="muted">
        Провайдеры моделей не настроены: профиль организации их не задаёт. Ключ и адрес можно указать во вкладке
        «Доступы».
      </p>
    );
  }
  const total = providers.reduce((n, p) => n + p.models.length, 0);
  return (
    <>
      <p className="muted small">
        {providers.length} провайдер(а), {total} моделей. Агент выбирает модель в чате dsh; здесь — что доступно и
        чем оплачивается. Ключи хранятся в доступах этой машины и общие для всех рабочих мест.
      </p>
      <div className="model-grid">
        {providers.map((p) => (
          <section key={p.id} className="card model-card">
            {/* строки = поля, каждое на всю ширину, строго друг под другом */}
            <div className="model-line">
              <h3>
                {p.id} {p.primary && <span className="badge">основной</span>}
              </h3>
              <span className={`badge ${p.keySet ? "state-running" : "state-exited"}`}>
                {p.keySet ? "ключ задан" : "нет ключа"}
              </span>
            </div>
            <div className="model-line mono small muted">{p.baseUrls.join(" · ") || "адрес не задан"}</div>
            <div className="model-line">
              <KeyForm p={p} onSaved={saved} />
            </div>
            <div className="model-line muted small">
              Ключ: <span className="mono">{p.keyEnv}</span>
            </div>
            <div className="model-line">
              {p.models.length ? (
                <span className="chips">
                  {p.models.map((m) => (
                    <span key={m} className={m === p.defaultModel ? "chip chip-on" : "chip"} title={m === p.defaultModel ? "модель по умолчанию" : ""}>
                      {m}
                      {m === p.defaultModel && " ★"}
                    </span>
                  ))}
                </span>
              ) : (
                <span className="muted small">модели не заданы</span>
              )}
            </div>
          </section>
        ))}
      </div>
      <p className="muted small">
        Сравнение моделей по умности на уровнях усилий (low…max) и по бенчмаркам появится на следующем этапе.
      </p>
    </>
  );
}
