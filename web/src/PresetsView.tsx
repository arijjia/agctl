import { useCallback, useEffect, useState } from "react";
import { presets, type Preset, type PresetsCatalog } from "./api";

// PresetsView — конструктор пресетов возможностей: разработчик собирает пресет
// галочками (MCP-серверы платформы, скиллы из каталога профиля) и выбирает
// рабочие места применения. agctl материализует пресет в dsh-home мест; в dsh
// при создании сессии остаётся просто выбрать пресет по имени (id — agctl-<id>).

function CheckList({
  items,
  picked,
  onToggle,
}: {
  items: { id: string; title: string; description: string; hint?: string }[];
  picked: string[];
  onToggle: (id: string) => void;
}) {
  if (!items.length) return <p className="muted small">в каталоге пусто</p>;
  return (
    <div className="check-list">
      {items.map((it) => (
        <label key={it.id} className="check-row">
          <input type="checkbox" checked={picked.includes(it.id)} onChange={() => onToggle(it.id)} />
          <span>
            <span>{it.title}</span>
            <span className="muted small"> — {it.description}</span>
            {it.hint && <span className="mono muted small"> {it.hint}</span>}
          </span>
        </label>
      ))}
    </div>
  );
}

function toggle(list: string[], id: string): string[] {
  return list.includes(id) ? list.filter((x) => x !== id) : [...list, id];
}

// PresetEditor — сборка нового или изменение существующего пресета.
function PresetEditor({
  cat,
  preset,
  onClose,
  onSaved,
}: {
  cat: PresetsCatalog;
  preset?: Preset;
  onClose: () => void;
  onSaved: (m: string, err?: boolean) => void;
}) {
  const [name, setName] = useState(preset?.name || "");
  const [description, setDescription] = useState(preset?.description || "");
  const [mcp, setMcp] = useState<string[]>(preset?.mcp || []);
  const [skills, setSkills] = useState<string[]>(preset?.skills || []);
  const [places, setPlaces] = useState<string[]>(preset?.places || ["main"]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const save = async () => {
    setBusy(true);
    setErr("");
    try {
      if (preset) {
        await presets.update(preset.id, { name, description, mcp, skills, places, revision: preset.revision });
        onSaved(`Пресет «${name}» сохранён — работающие места подхватят его после перезапуска`);
      } else {
        await presets.create({ name, description, mcp, skills, places });
        onSaved(`Пресет «${name}» собран — после перезапуска места выберите его при создании сессии в dsh`);
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
        <h2>{preset ? `Пресет «${preset.name}»` : "Новый пресет"}</h2>
        <p className="muted small">
          Состав применяется к рабочим местам галочками ниже; в dsh пресет выбирается при создании сессии.
        </p>
        <label>
          Название
          <input value={name} autoFocus placeholder="например, Поиск или Веб-набор" onChange={(e) => setName(e.target.value)} />
        </label>
        <label>
          Описание
          <input value={description} placeholder="зачем этот набор" onChange={(e) => setDescription(e.target.value)} />
        </label>
        <h3 className="small-title">MCP-серверы</h3>
        <CheckList items={cat.mcp} picked={mcp} onToggle={(id) => setMcp((m) => toggle(m, id))} />
        <h3 className="small-title">Скиллы (каталог профиля)</h3>
        <CheckList items={cat.skills} picked={skills} onToggle={(id) => setSkills((s) => toggle(s, id))} />
        <h3 className="small-title">Рабочие места</h3>
        <CheckList
          items={cat.places.map((p) => ({ id: p.id, title: p.name, description: p.id === "main" ? "основное место" : "/p/" + p.id + "/" }))}
          picked={places}
          onToggle={(id) => setPlaces((p) => toggle(p, id))}
        />
        {err && <div className="error">{err}</div>}
        <div className="toolbar">
          <button className="primary" disabled={busy || !name.trim()} onClick={save}>
            {preset ? "Сохранить" : "Собрать пресет"}
          </button>
          <button className="ghost" onClick={onClose}>
            Отмена
          </button>
        </div>
      </div>
    </div>
  );
}

export default function PresetsView({ note, refreshKey }: { note: (m: string, err?: boolean) => void; refreshKey: number }) {
  const [cat, setCat] = useState<PresetsCatalog | null>(null);
  const [editing, setEditing] = useState<Preset | "new" | null>(null);
  const [err, setErr] = useState("");
  const load = useCallback(() => {
    presets
      .list()
      .then((r) => {
        setCat(r);
        setErr("");
      })
      .catch((e) => setErr(e.message));
  }, []);
  useEffect(() => {
    load();
  }, [load, refreshKey]);
  const saved = (m: string, isErr?: boolean) => {
    note(m, isErr);
    load();
  };
  if (err) return <div className="error">{err}</div>;
  if (!cat) return <span className="spinner" />;
  const titleOf = (list: { id: string; title: string }[], id: string) => list.find((x) => x.id === id)?.title || id;
  const placeName = (id: string) => cat.places.find((p) => p.id === id)?.name || id;
  return (
    <>
      <p className="muted small">
        Пресет — набор MCP-серверов и скиллов контейнера. Соберите его до запуска сессии: в dsh остаётся выбрать
        пресет по имени. Пресет ложится в выбранные места сразу, а dsh видит пресеты со старта: работающее
        место покажет кнопку «Перезапустить».
      </p>
      <div className="preset-list">
        {cat.presets.map((p) => (
          <section key={p.id} className="card preset-card">
            <div className="place-head">
              <div>
                <h3>{p.name}</h3>
                <div className="mono muted small">agctl-{p.id}{p.description ? ` · ${p.description}` : ""}</div>
              </div>
              <span className="badge">{p.places.length ? `мест: ${p.places.length}` : "без мест"}</span>
            </div>
            <div className="preset-lines">
              <div className="preset-line">
                <span className="muted small">MCP:</span>
                {p.mcp.length ? (
                  <span className="chips">
                    {p.mcp.map((id) => (
                      <span
                        key={id}
                        className={p.missing?.[id] ? "chip chip-warn" : "chip"}
                        title={p.missing?.[id] ? `Отключён: не задано ${p.missing[id].join(", ")}` : cat.mcp.find((m) => m.id === id)?.description}
                      >
                        {titleOf(cat.mcp, id)}
                      </span>
                    ))}
                  </span>
                ) : (
                  <span className="muted small">нет</span>
                )}
              </div>
              <div className="preset-line">
                <span className="muted small">Скиллы:</span>
                {p.skills.length ? (
                  <span className="chips">
                    {p.skills.map((id) => (
                      <span key={id} className="chip" title={cat.skills.find((s) => s.id === id)?.description}>
                        {titleOf(cat.skills, id)}
                      </span>
                    ))}
                  </span>
                ) : (
                  <span className="muted small">нет</span>
                )}
              </div>
              {p.missing && Object.keys(p.missing).length > 0 && (
                <div className="notice small">
                  Отключены до заполнения доступов во вкладке «Доступ»:{" "}
                  {Object.entries(p.missing)
                    .map(([id, names]) => `${titleOf(cat.mcp, id)} — ${names.map((n) => n.replace("|", " или ")).join(", ")}`)
                    .join("; ")}
                  . Остальные инструменты пресета работают.
                </div>
              )}
              <div className="preset-line">
                <span className="muted small">Места:</span>
                {p.places.length ? <span>{p.places.map(placeName).join(", ")}</span> : <span className="muted small">не применён</span>}
              </div>
            </div>
            <div className="toolbar">
              <button onClick={() => setEditing(p)}>Изменить</button>
              <span className="spacer" />
              <button
                className="ghost danger"
                onClick={async () => {
                  if (!confirm(`Удалить пресет «${p.name}»? С выбранных мест его состав будет снят.`)) return;
                  try {
                    await presets.remove(p.id);
                    saved(`Пресет «${p.name}» удалён`);
                  } catch (e: any) {
                    saved(e.message, true);
                  }
                }}
              >
                Удалить
              </button>
            </div>
          </section>
        ))}
        {!cat.presets.length && <p className="muted">Пресетов ещё нет — соберите первый: галочками MCP, скиллы и места.</p>}
        <button className="card place-new" onClick={() => setEditing("new")}>
          <span className="plus">＋</span>
          <span>Новый пресет</span>
          <span className="muted small">MCP, скиллы и места — галочками</span>
        </button>
      </div>
      {editing && (
        <PresetEditor cat={cat} preset={editing === "new" ? undefined : editing} onClose={() => setEditing(null)} onSaved={saved} />
      )}
    </>
  );
}
