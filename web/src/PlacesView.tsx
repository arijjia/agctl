import { useCallback, useEffect, useState } from "react";
import { places, type FsListing, type LocalImage, type LocalUI, type Place, type PlaceMount } from "./api";
import { LogsPanel } from "./LogsPanel";

// PlacesView — рабочие места на этой машине: у каждого свой контейнер, свои
// папки хоста (одна → /workspace, несколько → /workspace/<имя>), свой образ и
// свой dsh-home (сессии не смешиваются). Основное место — из config.yaml дома.

const stateTitle: Record<Place["state"], string> = {
  running: "работает",
  sleeping: "остановлено",
  starting: "запускается…",
  building: "готовит образ…",
  error: "ошибка",
};

function flavorTitle(f?: string) {
  if (f === "web") return "Веб: Playwright + Chromium поверх базового";
  if (f === "min") return "Минимальный: только dsh и git";
  if (f) return `Вкус профиля: ${f}`;
  return "Базовый: dsh, git, glab, jira-cli, uv, kubectl, офис";
}

// imageFacts — что внутри образа одной строкой: версия dsh, сборка guest,
// дата и размер (по меткам образа).
function imageFacts(im?: LocalImage) {
  if (!im) return "";
  const parts: string[] = [];
  if (im.dshVersion) parts.push(`dsh ${im.dshVersion}`);
  if (im.guestRevision) parts.push(`guest ${im.guestRevision}`);
  if (im.built) parts.push(`собран ${im.built}`);
  if (im.sizeMb) parts.push(`${(im.sizeMb / 1024).toFixed(1)} ГБ`);
  return parts.join(" · ");
}

function RevealButtons({ path, note }: { path: string; note: (m: string, err?: boolean) => void }) {
  const open = async (app: "finder" | "vscode") => {
    try {
      await places.reveal(path, app);
    } catch (e: any) {
      note(e.message, true);
    }
  };
  return (
    <span className="reveal">
      <button type="button" className="ghost small" title="Показать в Finder" onClick={() => open("finder")}>
        Finder
      </button>
      <button type="button" className="ghost small" title="Открыть в VS Code" onClick={() => open("vscode")}>
        VS Code
      </button>
    </span>
  );
}

// FolderPicker — дерево папок внутри домашнего каталога: заход в подпапки,
// крошки пути, выбор текущей папки (весь $HOME и служебное выбрать нельзя).
function FolderPicker({ onPick, picked }: { onPick: (path: string) => void; picked: string[] }) {
  const [listing, setListing] = useState<FsListing | null>(null);
  const [err, setErr] = useState("");
  const [hidden, setHidden] = useState(false);
  const go = useCallback(
    (path: string) => {
      places
        .fs(path, hidden)
        .then((l) => {
          setListing(l);
          setErr("");
        })
        .catch((e) => setErr(e.message));
    },
    [hidden],
  );
  useEffect(() => go(listing?.path || ""), [hidden]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!listing) return err ? <div className="error">{err}</div> : <span className="spinner" />;
  const crumbs = listing.path
    .slice(listing.home.length)
    .split("/")
    .filter(Boolean)
    .reduce<{ name: string; path: string }[]>((acc, part) => {
      const prev = acc.length ? acc[acc.length - 1].path : listing.home;
      return [...acc, { name: part, path: prev + "/" + part }];
    }, []);
  const already = picked.includes(listing.path);
  return (
    <div className="folder-picker">
      <div className="crumbs">
        <button type="button" className="link-btn" onClick={() => go(listing.home)}>
          ~
        </button>
        {crumbs.map((c) => (
          <span key={c.path}>
            {" / "}
            <button type="button" className="link-btn" onClick={() => go(c.path)}>
              {c.name}
            </button>
          </span>
        ))}
        <span className="spacer" />
        <label className="inline small">
          <input type="checkbox" checked={hidden} onChange={(e) => setHidden(e.target.checked)} /> скрытые
        </label>
      </div>
      <div className="folder-list">
        {listing.parent && (
          <button type="button" className="folder-item" onClick={() => go(listing.parent)}>
            ↰ выше
          </button>
        )}
        {listing.entries.map((e) => (
          <button type="button" key={e.path} className="folder-item" onClick={() => go(e.path)}>
            📁 {e.name} {e.git && <span className="badge">git</span>}
          </button>
        ))}
        {!listing.entries.length && <div className="muted small">подпапок нет</div>}
      </div>
      <div className="toolbar">
        <span className="mono small">{listing.path}</span>
        <span className="spacer" />
        <button
          type="button"
          className="primary small"
          disabled={!listing.selectable || already}
          title={listing.reason}
          onClick={() => onPick(listing.path)}
        >
          {already ? "уже выбрана" : "Выбрать эту папку"}
        </button>
      </div>
      {!listing.selectable && listing.reason && <div className="muted small">{listing.reason}</div>}
      {err && <div className="error">{err}</div>}
    </div>
  );
}

// MountRows — строки папок на всю ширину, строго друг под другом: путь
// редактируется, «Выбрать…» открывает системный диалог папки (окно Finder
// на macOS и т.п.), при отказе — дерево в этой форме. Пустая форма нового
// места валидна: сервер заведёт папку по умолчанию <папка проектов>/<id>.
function MountRows({
  mounts,
  onChange,
}: {
  mounts: PlaceMount[];
  onChange: (m: PlaceMount[]) => void;
}) {
  const [treeFor, setTreeFor] = useState<number | null>(null); // строка, где открыто дерево
  const setAt = (i: number, host: string) => onChange(mounts.map((m, j) => (j === i ? { ...m, host } : m)));
  const pickDialog = async (i: number) => {
    try {
      const r = await places.fsDialog();
      if (!r.cancelled && r.path) setAt(i, r.path);
      else setTreeFor(i); // отмена пользователя → оставляем поле как было
    } catch {
      setTreeFor(i); // нет системного диалога → фолбэк: дерево папок
    }
  };
  return (
    <>
      {mounts.map((m, i) => (
        <div key={i} className="mount-edit">
          <div className="mount-edit-row">
            <input
              className="mono grow"
              value={m.host}
              placeholder="папка на этой машине, например ~/projects/сайт"
              onChange={(e) => setAt(i, e.target.value)}
            />
            <button type="button" className="small" onClick={() => pickDialog(i)}>
              Выбрать…
            </button>
            <button type="button" className="ghost small" onClick={() => onChange(mounts.filter((_, j) => j !== i))}>
              убрать
            </button>
          </div>
          {mounts.length > 1 && (
            <div className="mount-edit-row">
              <span className="mono muted small">→ /workspace/</span>
              <input
                className="mono mount-name"
                value={m.name}
                placeholder="имя (по умолчанию — имя папки)"
                onChange={(e) => onChange(mounts.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))}
              />
            </div>
          )}
          {treeFor === i && (
            <FolderPicker
              picked={mounts.map((x) => x.host)}
              onPick={(path) => {
                setAt(i, path);
                setTreeFor(null);
              }}
            />
          )}
        </div>
      ))}
      <div className="toolbar">
        <button type="button" onClick={() => onChange([...mounts, { host: "", name: "" }])}>
          + Добавить папку…
        </button>
        {mounts.length > 1 && <span className="muted small">первая папка — /workspace, остальные — /workspace/&lt;имя&gt;</span>}
      </div>
    </>
  );
}

// PlaceEditor — мастер создания или изменения рабочего места.
function PlaceEditor({
  ui,
  place,
  onClose,
  onSaved,
}: {
  ui: LocalUI;
  place?: Place;
  onClose: () => void;
  onSaved: (m: string, err?: boolean) => void;
}) {
  const isMain = !!place?.main;
  const [name, setName] = useState(place?.name || "");
  const [mounts, setMounts] = useState<PlaceMount[]>(place?.mounts || []);
  const [image, setImage] = useState(place?.image || ui.current);
  const [startNow, setStartNow] = useState(!place);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const save = async () => {
    setBusy(true);
    setErr("");
    try {
      // пустые строки — не папки; у нового места пустой список = дефолт сервера
      const clean = mounts.filter((m) => m.host.trim() !== "");
      if (place) {
        await places.update(place.id, { name, image, mounts: clean, revision: place.revision });
        onSaved(`«${name}»: сохранено` + (place.state === "running" ? " — перезапустите, чтобы применить" : ""));
      } else {
        const p = await places.create({ name, image, mounts: clean });
        if (startNow) await places.action(p.id, "start");
        onSaved(`«${name}»: создано` + (startNow ? ", запускаю…" : ""));
      }
      onClose();
    } catch (e: any) {
      setErr(e.message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal card" onClick={(e) => e.stopPropagation()}>
        <h2>{place ? `Рабочее место «${place.name}»` : "Новое рабочее место"}</h2>
        {!isMain && (
          <label>
            Название
            <input value={name} autoFocus placeholder="например, Фронтенд или Аналитика" onChange={(e) => setName(e.target.value)} />
          </label>
        )}
        <h3 className="small-title">Папки на этой машине</h3>
        <p className="muted small">
          {isMain
            ? "Основное рабочее место — одна папка, она становится /workspace."
            : mounts.length === 0
              ? `По умолчанию заведём ${ui.workdir}/<имя места> → /workspace. Правьте строки ниже, если нужно иначе.`
              : "Одна папка становится /workspace целиком; несколько — каждая подкаталогом /workspace/<имя>."}
        </p>
        <MountRows mounts={mounts} onChange={setMounts} />
        <h3 className="small-title">Образ</h3>
        <div className="image-list">
          {ui.images.map((im) => (
            <label key={im.ref} className="image-option">
              <input type="radio" name="place-image" checked={image === im.ref} onChange={() => setImage(im.ref)} />
              <span>
                <span>{flavorTitle(im.flavor)}</span>
                <br />
                <span className="mono muted small">{im.ref}</span>{" "}
                {im.present ? (
                  <span className="badge state-running">есть на машине</span>
                ) : (
                  <span className="badge state-exited">скачается или соберётся при запуске</span>
                )}
                {im.outdated && <span className="badge state-removed">устарел</span>}
                {imageFacts(im) && (
                  <>
                    <br />
                    <span className="muted small">{imageFacts(im)}</span>
                  </>
                )}
                {im.note && (
                  <>
                    <br />
                    <span className="muted small">{im.note}</span>
                  </>
                )}
              </span>
            </label>
          ))}
        </div>
        {!place && (
          <label className="inline">
            <input type="checkbox" checked={startNow} onChange={(e) => setStartNow(e.target.checked)} /> запустить сразу
          </label>
        )}
        {err && <div className="error">{err}</div>}
        <div className="toolbar">
          <button className="primary" disabled={busy || !name.trim() || (!!place && !mounts.some((m) => m.host.trim()))} onClick={save}>
            {place ? "Сохранить" : "Создать"}
          </button>
          <button className="ghost" onClick={onClose}>
            Отмена
          </button>
        </div>
      </div>
    </div>
  );
}

function PlaceCard({
  p,
  ui,
  note,
  onEdit,
  compact,
}: {
  p: Place;
  ui: LocalUI;
  note: (m: string, err?: boolean) => void;
  onEdit: () => void;
  compact?: boolean;
}) {
  const [logs, setLogs] = useState(false);
  const busy = p.state === "starting" || p.state === "building";
  const running = p.state === "running";
  const img = ui.images.find((i) => i.ref === p.image);
  const present = img?.present;
  const act = async (a: "start" | "restart" | "stop") => {
    try {
      await places.action(p.id, a);
      note(a === "stop" ? `«${p.name}» остановлено, данные сохранены` : `«${p.name}»: готовлю…`);
    } catch (e: any) {
      note(e.message, true);
    }
  };
  const del = async () => {
    if (!confirm(`Удалить рабочее место «${p.name}»? Контейнер остановится, слот освободится; папки на диске не тронутся, сессии dsh уйдут в архив.`)) return;
    try {
      const r = await places.remove(p.id);
      note(r.archived ? `«${p.name}» удалено; сессии dsh в архиве ${r.archived}` : `«${p.name}» удалено`);
    } catch (e: any) {
      note(e.message, true);
    }
  };
  const fetchLogs = useCallback(
    () => places.logs(p.id, 300).then((r) => ({ container: p.name, state: r.running ? "running" : "stopped", logs: r.logs })),
    [p.id, p.name],
  );
  return (
    <section className={`card place-card state-${p.state}`}>
      <div className="place-head">
        <div>
          <h3>
            {p.name} {p.main && <span className="badge">основное</span>}
          </h3>
          <div className="mono muted small">
            слот {p.slot} · {p.image}
          </div>
          {imageFacts(img) && <div className="muted small">{imageFacts(img)}</div>}
        </div>
        <span className={`badge ${running ? "state-running" : p.state === "error" ? "state-removed" : "state-exited"}`}>
          {stateTitle[p.state]}
        </span>
      </div>
      {img?.outdated && img.present && <div className="notice small">Образ: {img.note}</div>}
      {p.task.restartNeeded && (
        <div className="notice small">
          Папки, образ, доступы или пресеты изменены — место работает на старых.
          <button className="primary small" disabled={busy} onClick={() => act("restart")}>
            Перезапустить
          </button>
        </div>
      )}
      <div className="mounts">
        {p.layout.map((m) => (
          <div key={m.host} className="mount-row">
            <span className="mono small grow" title={m.host}>
              {m.host}
            </span>
            <span className="mono small muted">→ {m.container}</span>
            <RevealButtons path={m.host} note={note} />
          </div>
        ))}
        {!compact && (
          <div className="mount-row">
            <span className="small muted grow">dsh-home (сессии, скиллы, настройки):</span>
            <RevealButtons path={p.dshHome} note={note} />
          </div>
        )}
      </div>
      <div className="toolbar">
        <a className="button primary" href={p.url} target="_blank" rel="noreferrer" aria-disabled={!running}>
          Открыть чат →
        </a>
        {!running ? (
          <button disabled={busy} onClick={() => act("start")}>
            {busy ? "…" : present === false ? "Подготовить и запустить" : "Запустить"}
          </button>
        ) : (
          <button disabled={busy} onClick={() => act("restart")}>
            Перезапустить
          </button>
        )}
        <button className="ghost" disabled={busy || !running} onClick={() => act("stop")}>
          Остановить
        </button>
        <button className="ghost" disabled={!running} onClick={() => setLogs(true)}>
          Логи
        </button>
        {!compact && (
          <>
            <span className="spacer" />
            <button className="ghost" disabled={busy} onClick={onEdit}>
              Изменить
            </button>
            {!p.main && (
              <button className="ghost danger" disabled={busy} onClick={del}>
                Удалить
              </button>
            )}
          </>
        )}
      </div>
      {(busy || p.state === "error") && (p.task.log.length > 0 || p.task.error) && (
        <pre className="task-log">
          {p.task.log.join("\n")}
          {p.task.error && `\n\nОшибка: ${p.task.error}`}
          {busy && "\n…"}
        </pre>
      )}
      {logs && <LogsPanel fetchLogs={fetchLogs} onClose={() => setLogs(false)} />}
    </section>
  );
}

export default function PlacesView({
  ui,
  note,
  refreshKey,
  only,
}: {
  ui: LocalUI;
  note: (m: string, err?: boolean) => void;
  refreshKey: number;
  only?: string; // встроенный режим: показать одно рабочее место
}) {
  const [list, setList] = useState<Place[] | null>(null);
  const [editing, setEditing] = useState<Place | "new" | null>(null);
  const [err, setErr] = useState("");
  const load = useCallback(() => {
    places
      .list()
      .then((r) => {
        setList(r.places);
        setErr("");
      })
      .catch((e) => setErr(e.message));
  }, []);
  const anyBusy = !!list?.some((p) => p.state === "starting" || p.state === "building");
  useEffect(() => {
    load();
    const t = setInterval(load, anyBusy ? 2000 : 15000);
    return () => clearInterval(t);
  }, [load, anyBusy, refreshKey]);
  const saved = (m: string, isErr?: boolean) => {
    note(m, isErr);
    load();
  };
  if (err) return <div className="error">{err}</div>;
  if (!list) return <span className="spinner" />;
  const shown = only ? list.filter((p) => p.id === only) : list;
  return (
    <>
      {!only && (
        <p className="muted small">
          У каждого рабочего места свой контейнер, свои папки и свои сессии dsh. Доступы и ключи моделей — общие.
        </p>
      )}
      <div className="place-grid">
        {shown.map((p) => (
          <PlaceCard key={p.id} p={p} ui={ui} note={saved} onEdit={() => setEditing(p)} compact={!!only} />
        ))}
        {!only && (
          <button className="card place-new" onClick={() => setEditing("new")}>
            <span className="plus">＋</span>
            <span>Новое рабочее место</span>
            <span className="muted small">папки на этой машине, образ</span>
          </button>
        )}
      </div>
      {editing && (
        <PlaceEditor
          ui={ui}
          place={editing === "new" ? undefined : editing}
          onClose={() => setEditing(null)}
          onSaved={saved}
        />
      )}
    </>
  );
}
