import { useCallback, useEffect, useState } from "react";
import { imagesApi, type ImagePlan, type ImageStage, type ImageTask, type ImageView } from "./api";
import { Panel } from "./Shell";

// ImagesView — раздел «Образы» лаунчера: каталог образов (что внутри, какие
// места их используют, чем отличаются), как каждый собирается (ступени,
// Dockerfile, аргументы, что качается из сети) и действия: скачать или
// собрать, пересобрать, удалить. Операции идут фоном на сервере; все копии
// лаунчера видят их журнал (поток событий перечитывает список).

const KIND: Record<string, string> = {
  full: "полный",
  min: "минимальный",
  web: "web (браузер)",
};

const kindOf = (im: ImageView) => KIND[im.variant || im.flavor || "full"] || im.variant || im.flavor || "полный";

const gb = (mb?: number) => (mb ? `${(mb / 1024).toFixed(1)} ГБ` : "—");

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

  const act = async (action: "ensure" | "rebuild" | "remove", im: ImageView) => {
    const users = im.usedBy.map((u) => `«${u.name}»`).join(", ");
    if (action === "remove" && !confirm(`Удалить образ ${im.ref} с этой машины?${users ? `\nЕго используют места ${users} — при запуске он скачается или соберётся снова.` : ""}`)) return;
    if (action === "rebuild" && !confirm(`Пересобрать ${im.ref}? Прежний образ удаляется, новый берётся из реестра или собирается на этой машине (из кэша Docker — быстро).`)) return;
    try {
      const r = await imagesApi.action(action, im.ref);
      setTask(r.task);
      note(`${im.ref}: ${action === "remove" ? "удаляю" : action === "rebuild" ? "пересобираю" : "скачиваю или собираю"} — журнал ниже`);
    } catch (e: any) {
      note(e.message, true);
    }
  };

  const busy = task?.state === "building";
  return (
    <>
      <p className="muted small">
        Образ рабочего места — цепочка ступеней: базовый (Debian и Node, инструменты, прогретые MCP, dsh, guest agctl) →
        вкус (web — с браузером) → надстройка профиля. При запуске места agctl сначала пробует <code>docker pull</code>, а
        если образа в реестре нет — собирает недостающие ступени на этой машине; готовые ступени и слои Docker берутся из
        кэша, поэтому новая версия dsh — это пересборка только верхних слоёв. «Как собирается» показывает Dockerfile
        каждой ступени, аргументы и всё, что качается из сети.
      </p>
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
                  <td className="mono">{im.dshVersion || "—"}</td>
                  <td className="mono">{im.guestRevision || "—"}</td>
                  <td>{im.built || "—"}</td>
                  <td>{gb(im.sizeMb)}</td>
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
                <StageCard key={st.ref} st={st} n={i + 1} total={plan.stages.length} />
              ))}
            </>
          )}
        </Panel>
      )}
    </>
  );
}

function StageCard({ st, n, total }: { st: ImageStage; n: number; total: number }) {
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
      {list("контекст сборки", st.context)}
      {list("базовые образы", st.from)}
      {list("пакеты apt", st.apt)}
      {list("пакеты npm", st.npm)}
      {list("загрузки", st.downloads)}
      <details style={{ marginTop: 6 }}>
        <summary className="small">Dockerfile целиком</summary>
        <pre className="logs mono small">{st.dockerfile}</pre>
      </details>
    </div>
  );
}
