import { Fragment, useCallback, useEffect, useState } from "react";
import { imagesApi, type ImageAction, type ImageContext, type ImageFile, type ImagePlan, type ImageStage, type ImageTask, type ImageView } from "./api";
import { Code, langOf } from "./highlight";
import { Panel } from "./Shell";

// ImagesView — раздел «Образы» лаунчера: каталог образов (что внутри, какие
// места их используют, чем отличаются), как каждый собирается (ступени,
// Dockerfile, аргументы, файлы контекста сборки — каждый открывается, что
// качается из сети) и действия: скачать или собрать, скачать из реестра,
// пересобрать на этой машине, удалить.
// Операции идут фоном на сервере; все копии лаунчера видят их журнал
// (поток событий перечитывает список).

const KIND: Record<string, string> = {
  full: "полный",
  min: "минимальный",
  web: "web (браузер)",
};

const kindOf = (im: ImageView) => KIND[im.variant || im.flavor || "full"] || im.variant || im.flavor || "полный";

const gb = (mb?: number) => (mb ? `${(mb / 1024).toFixed(1)} ГБ` : "—");

const bytes = (n: number) =>
  n < 1024 ? `${n} Б` : n < 1 << 20 ? `${(n / 1024).toFixed(1)} КБ` : n < 1 << 30 ? `${(n / (1 << 20)).toFixed(1)} МБ` : `${(n / (1 << 30)).toFixed(1)} ГБ`;

// nFiles — «1 файл», «3 файла», «12 файлов».
const nFiles = (n: number) => {
  const d = n % 10;
  const dd = n % 100;
  return `${n} ${d === 1 && dd !== 11 ? "файл" : d >= 2 && d <= 4 && (dd < 12 || dd > 14) ? "файла" : "файлов"}`;
};

export default function ImagesView({ note, refreshKey }: { note: (m: string, err?: boolean) => void; refreshKey: number }) {
  const [list, setList] = useState<ImageView[] | null>(null);
  const [task, setTask] = useState<ImageTask | null>(null);
  const [actions, setActions] = useState(false);
  const [open, setOpen] = useState("");
  const [plan, setPlan] = useState<ImagePlan | null>(null);
  const [planErr, setPlanErr] = useState("");

  const load = useCallback(() => {
    imagesApi
      .list()
      .then((d) => {
        setList(d.images);
        setTask(d.task);
        setActions(d.actions);
      })
      .catch((e) => note(e.message, true));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(load, [load, refreshKey]);

  const showPlan = async (ref: string) => {
    if (open === ref) {
      setOpen("");
      return;
    }
    setOpen(ref);
    setPlan(null);
    setPlanErr("");
    try {
      setPlan(await imagesApi.plan(ref));
    } catch (e: any) {
      setPlanErr(e.message);
    }
  };

  const act = async (action: ImageAction, im: ImageView) => {
    const users = im.usedBy.map((u) => `«${u.name}»`).join(", ");
    if (action === "remove" && !confirm(`Удалить образ ${im.ref} с этой машины?${users ? `\nЕго используют места ${users} — при запуске он скачается или соберётся снова.` : ""}`)) return;
    if (
      action === "rebuild" &&
      !confirm(`Пересобрать ${im.ref} на этой машине? Прежний образ удаляется, новый собирается из контекста, встроенного в agctl (готовые нижние ступени и слои — из кэша Docker, быстро). Готовый образ из реестра — кнопка «Скачать из реестра».`)
    )
      return;
    try {
      const r = await imagesApi.action(action, im.ref);
      setTask(r.task);
      const what = { remove: "удаляю", rebuild: "пересобираю на этой машине", pull: `скачиваю из ${im.pullFrom}`, ensure: "скачиваю или собираю" }[action];
      note(`${im.ref}: ${what} — журнал ниже`);
    } catch (e: any) {
      note(e.message, true);
    }
  };

  const busy = task?.state === "building";
  return (
    <>
      <p className="muted small">
        Образ собирается ступенями: базовый → вкус (web — с браузером) → надстройка профиля. agctl сначала ищет его в
        реестре образов, а если не находит — собирает на этой машине.
      </p>
      <details className="explain">
        <summary>Как это устроено</summary>
        <ul>
          <li>
            <b>Откуда.</b> Реестр — <code>images.registry</code> профиля, по умолчанию <code>ghcr.io/arijjia</code> (там{" "}
            <code>agctl-dsh</code> и <code>agctl-dsh-min</code>). Чего в реестре нет, собирается здесь; готовые слои берутся
            из кэша Docker, поэтому новая версия dsh пересобирает только верхние слои.
          </li>
          <li>
            <b>Скачать из реестра</b> — свежий образ, в том числе для работающего места: новое применится при его
            перезапуске.
          </li>
          <li>
            <b>Пересобрать</b> — сборка на этой машине без реестра. Исходники не нужны: Dockerfile ступеней, entrypoint и
            скелет dsh-home встроены в бинарь agctl, guest лежит в <code>~/.agctl/bin/guest</code>.
          </li>
          <li>
            <b>Как собирается</b> — Dockerfile каждой ступени, аргументы, файлы контекста сборки и всё, что качается из
            сети.
          </li>
        </ul>
      </details>
      {task && (task.state === "building" || task.state === "error" || task.log.length > 0) && (
        <Panel label={`Операция с образом ${task.image}`} aside={<span className={`badge ${busy ? "state-created" : task.state === "error" ? "state-removed" : "state-running"}`}>{busy ? "идёт" : task.state === "error" ? "ошибка" : "готово"}</span>}>
          {task.error && <div className="error">{task.error}</div>}
          <pre className="logs mono small">{task.log.join("\n") || "…"}</pre>
        </Panel>
      )}
      <Panel label="Каталог образов">
        {list === null && <p className="muted small">Загрузка…</p>}
        {list && list.length === 0 && <p className="muted small">В профиле нет каталога образов.</p>}
        {list && list.length > 0 && (
          <table className="data-table">
            <thead>
              <tr>
                <th>Образ</th>
                <th>Вид</th>
                <th>dsh</th>
                <th>guest</th>
                <th>Собран</th>
                <th>Размер</th>
                <th>Места</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {list.map((im) => (
                <tr key={im.ref}>
                  <td>
                    <span className="mono">{im.ref}</span>{" "}
                    {im.current && <span className="badge">по умолчанию</span>}{" "}
                    <span className={`badge ${im.present ? "state-running" : "state-exited"}`}>{im.present ? "на машине" : "нет на машине"}</span>{" "}
                    {im.outdated && im.present && (
                      <span className="badge state-removed" title={im.note || ""}>
                        устарел
                      </span>
                    )}
                  </td>
                  <td>{kindOf(im)}</td>
                  <td className="mono nowrap">{im.dshVersion || "—"}</td>
                  <td className="mono nowrap">{im.guestRevision || "—"}</td>
                  <td className="nowrap">{im.built || "—"}</td>
                  <td className="nowrap">{gb(im.sizeMb)}</td>
                  <td>
                    {im.usedBy.length === 0
                      ? "—"
                      : im.usedBy.map((u) => (
                          <div key={u.id} title={u.running ? "работает" : "остановлено"}>
                            {u.running ? "● " : ""}
                            {u.name}
                          </div>
                        ))}
                  </td>
                  <td>
                    <div className="ws-actions" style={{ marginTop: 0 }}>
                      <button className="ghost small" onClick={() => showPlan(im.ref)}>
                        {open === im.ref ? "Скрыть" : "Как собирается"}
                      </button>
                      {!im.present && (
                        <button className="primary small" disabled={busy} onClick={() => act("ensure", im)}>
                          Скачать или собрать
                        </button>
                      )}
                      {actions && (
                        <button
                          className="ghost small"
                          disabled={busy || !im.pullFrom}
                          title={
                            im.pullFrom
                              ? `docker pull ${im.pullFrom}${im.usedBy.some((u) => u.running) ? " — работающие места получат новую версию при перезапуске" : ""}`
                              : "реестр образов выключен в профиле (images.registry: none) — образ только собирается на этой машине"
                          }
                          onClick={() => act("pull", im)}
                        >
                          Скачать из реестра
                        </button>
                      )}
                      {im.present && actions && (
                        <button className="ghost small" disabled={busy || im.usedBy.some((u) => u.running)} title={im.usedBy.some((u) => u.running) ? "образ работающего места — сначала остановите его" : ""} onClick={() => act("rebuild", im)}>
                          Пересобрать
                        </button>
                      )}
                      {im.present && actions && (
                        <button className="ghost danger small" disabled={busy || im.usedBy.some((u) => u.running)} title={im.usedBy.some((u) => u.running) ? "образ работающего места — сначала остановите его" : ""} onClick={() => act("remove", im)}>
                          Удалить
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
      {open && (
        <Panel label={`Как собирается ${open}`}>
          {planErr && <div className="error">{planErr}</div>}
          {!plan && !planErr && <p className="muted small">Загрузка…</p>}
          {plan && (
            <>
              <p className="small">{plan.pull}</p>
              {plan.stages.map((st, i) => (
                <StageCard key={st.ref} image={plan.ref} st={st} n={i + 1} total={plan.stages.length} />
              ))}
            </>
          )}
        </Panel>
      )}
    </>
  );
}

function StageCard({ image, st, n, total }: { image: string; st: ImageStage; n: number; total: number }) {
  const args = Object.entries(st.args || {});
  const list = (label: string, items?: string[]) =>
    items && items.length > 0 ? (
      <div className="small">
        <span className="muted">{label}: </span>
        <span className="mono">{items.join(", ")}</span>
      </div>
    ) : null;
  return (
    <div className="card" style={{ marginTop: 10 }}>
      <div className="ws-head">
        <div>
          <div className="ws-name">
            {n}/{total} · <span className="mono">{st.ref}</span>
          </div>
          <div className="muted small">{st.title}</div>
        </div>
        <span className={`badge ${st.present ? "state-running" : "state-exited"}`}>{st.present ? "готова" : "соберётся"}</span>
      </div>
      <div className="small" style={{ marginTop: 6 }}>
        <span className="muted">Dockerfile: </span>
        {st.source}
      </div>
      {args.length > 0 && (
        <div className="small">
          <span className="muted">аргументы: </span>
          <span className="mono">{args.map(([k, v]) => `${k}=${v}`).join("  ")}</span>
        </div>
      )}
      <ContextTable image={image} stage={n} items={st.context} />
      {list("базовые образы", st.from)}
      {list("пакеты apt", st.apt)}
      {list("пакеты npm", st.npm)}
      {list("загрузки", st.downloads)}
      <details style={{ marginTop: 6 }}>
        <summary className="small">Dockerfile целиком</summary>
        <Code text={st.dockerfile} lang="dockerfile" />
      </details>
    </div>
  );
}

// ContextTable — контекст сборки ступени, кроме Dockerfile: что за файл,
// откуда он (встроен в бинарь agctl, скелет dsh-home, guest-бинарь на
// диске, файл профиля), размер; строка раскрывается — текст файла с
// подсветкой или файлы каталога.
function ContextTable({ image, stage, items }: { image: string; stage: number; items?: ImageContext[] }) {
  const [open, setOpen] = useState("");
  if (!items || items.length === 0) return null;
  return (
    <div className="ctx">
      <div className="small muted">контекст сборки, кроме Dockerfile (строка открывает файл):</div>
      <table className="data-table ctx-table">
        <tbody>
          {items.map((it) => {
            const key = it.name.replace(/\/$/, "");
            const isOpen = open === key;
            return (
              <Fragment key={it.name}>
                <tr className="ctx-row" onClick={() => setOpen(isOpen ? "" : key)}>
                  <td className="mono ctx-name">
                    <span className="ctx-caret">{isOpen ? "▾" : "▸"}</span> {it.name}
                  </td>
                  <td>
                    {it.from}
                    {it.path && <div className="muted mono ctx-path">{it.path}</div>}
                  </td>
                  <td className="ctx-size">
                    {it.missing ? <span className="badge state-removed">нет на диске</span> : bytes(it.size)}
                    {it.dir && <div className="muted">{nFiles(it.files || 0)}</div>}
                    {it.modified && <div className="muted">{it.modified}</div>}
                  </td>
                </tr>
                {isOpen && (
                  <tr className="ctx-open">
                    <td colSpan={3}>
                      {it.missing ? <div className="error small">{it.note || "файла нет на диске"}</div> : <ContextPath image={image} stage={stage} path={key} />}
                    </td>
                  </tr>
                )}
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// ContextPath — файл контекста (текст с подсветкой: Dockerfile, shell — по
// расширению .sh или shebang) или каталог (файлы, каждый раскрывается).
function ContextPath({ image, stage, path }: { image: string; stage: number; path: string }) {
  const [data, setData] = useState<ImageFile | null>(null);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState("");
  useEffect(() => {
    let live = true;
    setData(null);
    setErr("");
    imagesApi
      .file(image, stage, path)
      .then((d) => {
        if (live) setData(d);
      })
      .catch((e) => {
        if (live) setErr(e.message);
      });
    return () => {
      live = false;
    };
  }, [image, stage, path]);
  if (err) return <div className="error small">{err}</div>;
  if (!data) return <p className="muted small">Загрузка…</p>;
  if (!data.dir) {
    const text = data.text || "";
    return <Code text={text} lang={langOf(path, text)} />;
  }
  const files = data.files || [];
  return (
    <div className="ctx-files">
      {files.length === 0 && <p className="muted small">Каталог пуст.</p>}
      {files.map((f) => (
        <div key={f.path}>
          <button type="button" className="link-btn" onClick={() => setOpen(open === f.path ? "" : f.path)}>
            {open === f.path ? "▾" : "▸"} {f.path.startsWith(path + "/") ? f.path.slice(path.length + 1) : f.path}
          </button>{" "}
          <span className="muted small">{bytes(f.size)}</span>
          {open === f.path && <ContextPath image={image} stage={stage} path={f.path} />}
        </div>
      ))}
    </div>
  );
}
