import { useState } from "react";
import { local, type LocalEntry, type LocalUI } from "./api";

// SecretsView — доступы локального режима: ключ модели, GitLab, Jira, свои
// переменные. Значения сервер не возвращает: видно только «задан / не задан»,
// несекретное (адреса, логины) показывается как есть.

const groupTitle: Record<string, string> = {
  llm: "Модель (LLM)",
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

export default function SecretsView({ ui, note }: { ui: LocalUI; note: (m: string, err?: boolean) => void }) {
  const [newName, setNewName] = useState("");
  const [newVal, setNewVal] = useState("");
  const [newPlain, setNewPlain] = useState(false);
  const groups = ["llm", "gitlab", "confluence", "jira", "other"]
    .map((g) => ({ g, items: ui.secrets.filter((s) => s.group === g) }))
    .filter((x) => x.items.length);
  return (
    <>
      <p className="muted small">
        Секреты хранятся в: {ui.storage}. Значения сюда не возвращаются — видно только, задан ли доступ. Доступы
        общие для всех рабочих мест; изменения попадают в рабочее место при его перезапуске, ключи моделей — сразу.
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
