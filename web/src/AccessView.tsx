import { useEffect, useState } from "react";
import { local, providers as providersApi, type LocalEntry, type LocalUI, type Provider } from "./api";

// AccessView — вкладка «Доступ» лаунчера: провайдеры моделей (заданные
// профилем организации и личные) и доступы рабочего места (GitLab, Jira,
// свои переменные). Значения ключей сервер не возвращает: видно только
// «задан / не задан»; несекретное (адреса, логины) показывается как есть.

const groupTitle: Record<string, string> = {
  gitlab: "GitLab",
  confluence: "Confluence",
  jira: "Jira",
  other: "Другие переменные",
};

export function SecretRow({ e, onSaved }: { e: LocalEntry; onSaved: (m: string, err?: boolean) => void }) {
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

// parseModels — список моделей из поля ввода: по одной на строку или через запятую.
const parseModels = (text: string) =>
  text
    .split(/[\n,]/)
    .map((m) => m.trim())
    .filter(Boolean);

// ProviderForm — добавление и правка личного провайдера. Имя задаётся один
// раз: под ним провайдер виден в списке моделей харнесса.
function ProviderForm({
  initial,
  onDone,
  onCancel,
}: {
  initial?: Provider;
  onDone: (m: string, err?: boolean) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(initial?.name || "");
  const [baseURL, setBaseURL] = useState(initial?.baseUrls[0] || "");
  const [models, setModels] = useState((initial?.models || []).join("\n"));
  const [defaultModel, setDefaultModel] = useState(initial?.defaultModel || "");
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const list = parseModels(models);
  const save = async () => {
    setBusy(true);
    try {
      const body = {
        baseURL,
        models: list,
        defaultModel: list.includes(defaultModel) ? defaultModel : "",
        ...(key.trim() ? { key: key.trim() } : {}),
      };
      if (initial) await providersApi.update(initial.name, body);
      else await providersApi.create({ name: name.trim(), ...body });
      onDone(`Провайдер ${initial?.name || name.trim()}: сохранён — перезапустите рабочее место`);
    } catch (e: any) {
      onDone(e.message, true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      className="provider-form"
      onSubmit={(ev) => {
        ev.preventDefault();
        save();
      }}
    >
      <label>
        Имя провайдера
        <input
          value={name}
          disabled={!!initial}
          placeholder="например zai или my-openrouter"
          onChange={(e) => setName(e.target.value)}
        />
        <span className="muted small">Под этим именем провайдер виден в списке моделей харнесса.</span>
      </label>
      <label>
        Адрес API (OpenAI-совместимый)
        <input value={baseURL} placeholder="https://api.example.com/v1" onChange={(e) => setBaseURL(e.target.value)} />
      </label>
      <label>
        Модели — по одной на строку
        <textarea rows={4} value={models} placeholder={"model-fast\nmodel-deep"} onChange={(e) => setModels(e.target.value)} />
      </label>
      <label>
        Модель по умолчанию
        <select value={list.includes(defaultModel) ? defaultModel : ""} onChange={(e) => setDefaultModel(e.target.value)}>
          <option value="">первая в списке</option>
          {list.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
      </label>
      <label>
        Ключ API
        <input
          type="password"
          autoComplete="off"
          value={key}
          placeholder={initial?.keySet ? "оставьте пустым — ключ не изменится" : "вставьте ключ"}
          onChange={(e) => setKey(e.target.value)}
        />
      </label>
      <div className="row-actions">
        <button className="primary" disabled={busy || !baseURL.trim() || !list.length || (!initial && !name.trim())}>
          {initial ? "Сохранить" : "Добавить провайдера"}
        </button>
        <button type="button" className="ghost" onClick={onCancel}>
          Отмена
        </button>
      </div>
    </form>
  );
}

// ProviderCard — провайдер: имя, откуда он, адрес, модели, состояние ключа.
function ProviderCard({ p, onChanged }: { p: Provider; onChanged: (m: string, err?: boolean) => void }) {
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const run = async (fn: () => Promise<any>, ok: string) => {
    setBusy(true);
    try {
      await fn();
      setKey("");
      onChanged(ok);
    } catch (e: any) {
      onChanged(e.message, true);
    } finally {
      setBusy(false);
    }
  };
  if (editing)
    return (
      <div className="provider-card">
        <ProviderForm
          initial={p}
          onCancel={() => setEditing(false)}
          onDone={(m, err) => {
            if (!err) setEditing(false);
            onChanged(m, err);
          }}
        />
      </div>
    );
  return (
    <div className="provider-card">
      <div className="secret-head">
        <span className="provider-name">
          <b className="mono">{p.name}</b>
          <span className="badge">{p.source === "own" ? "личный" : "организация"}</span>
          {p.primary && <span className="badge">по умолчанию</span>}
        </span>
        <span className={`badge ${p.keySet ? "state-running" : "state-exited"}`}>{p.keySet ? "ключ задан" : "ключ не задан"}</span>
      </div>
      <div className="mono small muted">{p.baseUrls.join(" · ")}</div>
      <div className="chips">
        {p.models.map((m) => (
          <span key={m} className={m === p.defaultModel ? "chip chip-on" : "chip"} title={m === p.defaultModel ? "модель по умолчанию" : ""}>
            {m}
          </span>
        ))}
      </div>
      <form
        className="secret-form"
        onSubmit={(ev) => {
          ev.preventDefault();
          if (!key.trim()) return;
          run(
            () =>
              p.source === "own"
                ? providersApi.update(p.name, { baseURL: p.baseUrls[0], models: p.models, defaultModel: p.defaultModel, key: key.trim() })
                : local.setSecret(p.keyEnv, key.trim(), false),
            `${p.name}: ключ сохранён`
          );
        }}
      >
        <input
          type="password"
          autoComplete="off"
          value={key}
          placeholder={p.keySet ? "новый ключ (заменит текущий)" : "вставьте ключ API"}
          onChange={(e) => setKey(e.target.value)}
        />
        <button className="primary" disabled={busy || !key.trim()}>
          Сохранить ключ
        </button>
        {p.source === "own" && (
          <>
            <button type="button" className="ghost" disabled={busy} onClick={() => setEditing(true)}>
              Изменить
            </button>
            <button
              type="button"
              className="ghost danger"
              disabled={busy}
              onClick={() => confirm(`Удалить провайдера ${p.name} вместе с ключом?`) && run(() => providersApi.remove(p.name), `${p.name}: удалён`)}
            >
              Удалить
            </button>
          </>
        )}
      </form>
    </div>
  );
}

export default function AccessView({
  ui,
  note,
  refreshKey,
}: {
  ui: LocalUI;
  note: (m: string, err?: boolean) => void;
  refreshKey: number;
}) {
  const [newName, setNewName] = useState("");
  const [newVal, setNewVal] = useState("");
  const [newPlain, setNewPlain] = useState(false);
  const [list, setList] = useState<Provider[] | null>(null);
  const [canAdd, setCanAdd] = useState(false);
  const [adding, setAdding] = useState(false);
  useEffect(() => {
    providersApi
      .list()
      .then((r) => {
        setList(r.providers);
        setCanAdd(r.canAdd);
      })
      .catch((e) => note(e.message, true));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshKey]);
  // Ключи моделей показаны в карточках провайдеров — в списке переменных их нет.
  const modelKeys = new Set((list || []).map((p) => p.keyEnv));
  const groups = ["gitlab", "confluence", "jira", "other"]
    .map((g) => ({ g, items: ui.secrets.filter((s) => s.group === g && !modelKeys.has(s.name)) }))
    .filter((x) => x.items.length);
  return (
    <>
      <p className="muted small">
        Секреты хранятся в: {ui.storage}. Значения сюда не возвращаются — видно только, задан ли доступ. Доступ общий
        для всех рабочих мест и попадает в рабочее место при его перезапуске.
      </p>
      <section>
        <h2 className="small">Провайдеры моделей</h2>
        {list === null && <p className="muted small">Загрузка…</p>}
        {list?.length === 0 && <p className="muted small">Провайдеров нет — добавьте своего.</p>}
        <div className="provider-grid">
          {list?.map((p) => (
            <ProviderCard key={p.name} p={p} onChanged={note} />
          ))}
        </div>
        {canAdd &&
          (adding ? (
            <div className="provider-card">
              <ProviderForm
                onCancel={() => setAdding(false)}
                onDone={(m, err) => {
                  if (!err) setAdding(false);
                  note(m, err);
                }}
              />
            </div>
          ) : (
            <button className="primary" onClick={() => setAdding(true)}>
              + Добавить провайдера
            </button>
          ))}
      </section>
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
          const name = newName.trim().toUpperCase();
          try {
            await local.setSecret(name, newVal, newPlain);
            setNewName("");
            setNewVal("");
            note(`${name}: сохранено`);
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
  );
}
