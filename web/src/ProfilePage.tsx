import { useEffect, useMemo, useRef, useState } from "react";
import { parseDocument, type Document } from "yaml";
import { profileApi, type ProfileSource } from "./api";
import { Panel } from "./Shell";

// ProfilePage — профиль организации: исходный profile.yaml и форма по нему.
// Форма и текст — два вида одного файла: правка поля меняет текст (комментарии
// и порядок сохраняются), правка текста обновляет поля. Сохранение пишет
// profile.yaml в каталог профиля на диске — тот же файл, что лежит в
// репозитории профиля. Профиль читается при старте, поэтому новые значения
// видны после перезапуска платформы.

type Path = (string | number)[];

const str = (doc: Document | null, path: Path): string => {
  const v = doc?.getIn(path);
  return v === undefined || v === null ? "" : String(v);
};

function Field({
  label,
  hint,
  value,
  onChange,
  disabled,
  mono,
}: {
  label: string;
  hint?: string;
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
  mono?: boolean;
}) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      <input className={mono ? "mono" : ""} value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)} />
      {hint && <span className="muted small">{hint}</span>}
    </label>
  );
}

type Extra = { name: string; base_url: string; model: string; models: string; api_key_env: string };

export default function ProfilePage() {
  const [src, setSrc] = useState<ProfileSource | null>(null);
  const [text, setText] = useState("");
  const [saved, setSaved] = useState("");
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [logoTick, setLogoTick] = useState(0);
  const fileRef = useRef<HTMLInputElement>(null);

  const load = () =>
    profileApi
      .get()
      .then((s) => {
        setSrc(s);
        setText(s.yaml);
        setSaved(s.yaml);
      })
      .catch((e) => setErr(e.message));
  useEffect(() => {
    load();
  }, []);

  // Документ YAML по текущему тексту; при синтаксической ошибке форма
  // замирает на последнем разборе, а ошибка показывается у редактора.
  const { doc, syntax } = useMemo(() => {
    const d = parseDocument(text);
    return { doc: d.errors.length ? null : d, syntax: d.errors[0]?.message || "" };
  }, [text]);
  const lastDoc = useRef<Document | null>(null);
  if (doc) lastDoc.current = doc;
  const view = doc || lastDoc.current;

  const set = (path: Path, value: string) => {
    if (!doc) return;
    if (value === "") doc.deleteIn(path);
    else doc.setIn(path, value);
    setText(doc.toString());
  };

  const extras: Extra[] = useMemo(() => {
    const v = view?.getIn(["llm", "extra_providers"]) as any;
    const list = v?.toJSON ? v.toJSON() : [];
    return Array.isArray(list) ? list.map((e: any) => ({ name: "", base_url: "", model: "", models: "", api_key_env: "", ...e })) : [];
  }, [view, text]);
  const setExtras = (list: Extra[]) => {
    if (!doc) return;
    if (!list.length) doc.deleteIn(["llm", "extra_providers"]);
    else doc.setIn(["llm", "extra_providers"], doc.createNode(list.map((e) => Object.fromEntries(Object.entries(e).filter(([, v]) => v !== "")))));
    setText(doc.toString());
  };

  const dirty = text !== saved;
  const readOnly = !src?.writable;

  const save = async () => {
    setBusy(true);
    setErr("");
    setMsg("");
    try {
      const r = await profileApi.save(text);
      setSaved(text);
      setMsg(`Сохранено в ${r.path}. Изменения вступят в силу после перезапуска платформы.`);
    } catch (e: any) {
      setErr(e.message);
    } finally {
      setBusy(false);
    }
  };
  const check = async () => {
    setErr("");
    setMsg("");
    try {
      const r = await profileApi.check(text);
      if (r.ok) setMsg("Проверка пройдена: файл можно сохранять.");
      else setErr(r.error || "ошибка проверки");
    } catch (e: any) {
      setErr(e.message);
    }
  };
  const uploadLogo = async (f: File) => {
    setErr("");
    try {
      const r = await profileApi.logo(f);
      if (doc && !str(doc, ["brand", "logo"])) set(["brand", "logo"], r.path);
      setLogoTick((t) => t + 1);
      setMsg(`Логотип записан в ${r.path}. Он появится в интерфейсе после перезапуска платформы.`);
    } catch (e: any) {
      setErr(e.message);
    }
  };

  if (!src) return err ? <div className="error">{err}</div> : <p className="muted">Загрузка…</p>;

  return (
    <>
      <div className="kv-strip">
        <span>
          <span className="kv-key">Профиль</span> <b className="mono">{src.id}</b>
        </span>
        <span>
          <span className="kv-key">Каталог</span> <span className="mono">{src.dir || "встроен в agctl"}</span>
        </span>
        {src.realDir && (
          <span>
            <span className="kv-key">Ссылка на</span> <span className="mono">{src.realDir}</span>
          </span>
        )}
        <span>
          <span className="kv-key">Файл</span> <span className="mono">profile.yaml</span>
        </span>
      </div>
      {src.reason && <div className="notice">{src.reason}</div>}
      {msg && <div className="notice ok">{msg}</div>}
      {err && <div className="error">{err}</div>}

      <div className="split">
        <div className="split-col">
          <Panel label="Бренд">
            <div className="logo-row">
              <div className="logo-box" key={logoTick}>
                {str(view, ["brand", "logo"]) ? <span className="mono small">{str(view, ["brand", "logo"])}</span> : <span className="muted small">без логотипа</span>}
              </div>
              <div>
                <button className="ghost" disabled={readOnly} onClick={() => fileRef.current?.click()}>
                  Загрузить логотип (PNG)
                </button>
                <input
                  ref={fileRef}
                  type="file"
                  accept="image/png"
                  hidden
                  onChange={(e) => {
                    const f = e.target.files?.[0];
                    if (f) uploadLogo(f);
                    e.target.value = "";
                  }}
                />
                <p className="muted small">Квадрат от 128×128, до 512 КБ. Пишется в каталог профиля.</p>
              </div>
            </div>
            <Field
              label="Название"
              hint="Над левой панелью чата, в шапке приложения и на странице входа."
              value={str(view, ["brand", "name"])}
              disabled={readOnly || !doc}
              onChange={(v) => set(["brand", "name"], v)}
            />
            <Field
              label="Приветственная фраза"
              hint="Крупно в центре экрана при открытии нового чата."
              value={str(view, ["brand", "tagline"])}
              disabled={readOnly || !doc}
              onChange={(v) => set(["brand", "tagline"], v)}
            />
            <Field
              label="Подзаголовок страницы входа"
              value={str(view, ["login", "subtitle"])}
              disabled={readOnly || !doc}
              onChange={(v) => set(["login", "subtitle"], v)}
            />
          </Panel>

          <Panel label="Основной провайдер моделей">
            <Field label="Имя провайдера" hint="Под этим именем он виден в списке моделей." mono value={str(view, ["llm", "provider_name"])} disabled={readOnly || !doc} onChange={(v) => set(["llm", "provider_name"], v)} />
            <Field label="Адрес API" mono value={str(view, ["llm", "default_base_url"])} disabled={readOnly || !doc} onChange={(v) => set(["llm", "default_base_url"], v)} />
            <Field label="Модели через запятую" mono value={str(view, ["llm", "default_models"])} disabled={readOnly || !doc} onChange={(v) => set(["llm", "default_models"], v)} />
            <Field label="Модель по умолчанию" mono value={str(view, ["llm", "default_model"])} disabled={readOnly || !doc} onChange={(v) => set(["llm", "default_model"], v)} />
            <p className="muted small">
              Ключ в профиль не пишется. Его задаёт разработчик во вкладке «Доступ» или администратор в окружении сервера
              (AGCTL_LLM_API_KEY).
            </p>
          </Panel>

          <Panel
            label="Дополнительные провайдеры"
            aside={
              <button
                className="ghost small"
                disabled={readOnly || !doc}
                onClick={() => setExtras([...extras, { name: "", base_url: "", model: "", models: "", api_key_env: "" }])}
              >
                + Добавить
              </button>
            }
          >
            {!extras.length && <p className="muted small">Дополнительных провайдеров нет.</p>}
            {extras.map((e, i) => {
              const upd = (k: keyof Extra, v: string) => setExtras(extras.map((x, j) => (j === i ? { ...x, [k]: v } : x)));
              return (
                <div key={i} className="sub-card">
                  <div className="field-grid">
                    <Field label="Имя" mono value={e.name} disabled={readOnly || !doc} onChange={(v) => upd("name", v)} />
                    <Field label="Адрес API" mono value={e.base_url} disabled={readOnly || !doc} onChange={(v) => upd("base_url", v)} />
                    <Field label="Модели через запятую" mono value={e.models} disabled={readOnly || !doc} onChange={(v) => upd("models", v)} />
                    <Field label="Модель по умолчанию" mono value={e.model} disabled={readOnly || !doc} onChange={(v) => upd("model", v)} />
                    <Field label="Имя переменной с ключом" hint="Значение — не здесь." mono value={e.api_key_env} disabled={readOnly || !doc} onChange={(v) => upd("api_key_env", v)} />
                  </div>
                  <button className="ghost danger small" disabled={readOnly || !doc} onClick={() => setExtras(extras.filter((_, j) => j !== i))}>
                    Удалить провайдера
                  </button>
                </div>
              );
            })}
          </Panel>

          <Panel label="Что лежит в каталоге профиля">
            <pre className="tree">{src.files.length ? src.files.join("\n") : "—"}</pre>
            <p className="muted small">
              Инструменты для пресетов — пакеты в catalog/, команды на компьютере разработчика — local/bridge-tools.yaml,
              надстройка образа — image/Dockerfile. Как их писать — README в каталоге профиля.
            </p>
          </Panel>
        </div>

        <div className="split-col">
          <Panel
            label="profile.yaml"
            aside={<span className={`badge ${dirty ? "state-exited" : "state-running"}`}>{dirty ? "есть несохранённые правки" : "совпадает с диском"}</span>}
          >
            <textarea
              className="code-editor"
              spellCheck={false}
              value={text}
              readOnly={readOnly}
              onChange={(e) => setText(e.target.value)}
            />
            {syntax && <div className="error">Синтаксис YAML: {syntax}</div>}
            <div className="row-actions">
              <button className="primary" disabled={busy || readOnly || !dirty || !!syntax} onClick={save}>
                Сохранить в каталог профиля
              </button>
              <button className="ghost" disabled={busy || readOnly || !!syntax} onClick={check}>
                Проверить
              </button>
              <button className="ghost" disabled={busy || !dirty} onClick={() => setText(saved)}>
                Отменить правки
              </button>
            </div>
          </Panel>
        </div>
      </div>
    </>
  );
}
