import { useEffect, useState, type ReactNode } from "react";
import { applyTheme, currentTheme, toggleTheme, type Theme } from "./theme";

// Shell — единая оболочка веб-приложения для обоих режимов и всех ролей:
// боковая панель слева (бренд организации, разделы, состояние,
// пользователь) и содержимое раздела справа на всю оставшуюся ширину. Набор разделов задаёт
// вызывающий экран по роли и режиму; выбранный раздел живёт в адресе
// (#/раздел), поэтому на него можно дать ссылку и он переживает обновление.

type Brand = { name?: string; logo?: string; letter?: string };
const brand: Brand = (window as any).__AGCTL_BRAND__ ?? {};
export const brandName = brand.name || "agctl";

export type NavItem = {
  id: string;
  label: string;
  // group — подпись группы разделов в панели (например «Администрирование»).
  group?: string;
  warn?: boolean;
};
export type Pill = { label: string; tone?: "ok" | "data" | "warn" | "muted"; title?: string };

// useSection — выбранный раздел из адреса; неизвестный — первый из списка.
export function useSection(items: NavItem[], fallback?: string): [string, (id: string) => void] {
  const read = () => {
    const id = decodeURIComponent(window.location.hash.replace(/^#\/?/, ""));
    return items.some((i) => i.id === id) ? id : fallback || items[0]?.id || "";
  };
  const [section, setSection] = useState(read);
  useEffect(() => {
    const onHash = () => setSection(read());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items.map((i) => i.id).join("|")]);
  const go = (id: string) => {
    window.location.hash = "/" + encodeURIComponent(id);
    setSection(id);
  };
  return [section, go];
}

function BrandMark() {
  // Название делится на две части разного тона — как знак в шапке.
  const words = brandName.split(/\s+/);
  const head = words.length > 1 ? words.slice(0, -1).join(" ") : brandName.slice(0, Math.ceil(brandName.length / 2));
  const tail = words.length > 1 ? words[words.length - 1] : brandName.slice(Math.ceil(brandName.length / 2));
  return (
    <div className="shell-brand" title={brandName}>
      {brand.logo ? (
        <img className="shell-logo" src={brand.logo} alt="" />
      ) : (
        <span className="shell-letter">{(brand.letter || brandName[0] || "a").toUpperCase()}</span>
      )}
      <span className="shell-brand-head">{head}</span>
      <span className="shell-brand-tail">{tail}</span>
    </div>
  );
}

export default function Shell({
  items,
  active,
  onSelect,
  pills = [],
  user,
  onLogout,
  title,
  subtitle,
  actions,
  children,
}: {
  items: NavItem[];
  active: string;
  onSelect: (id: string) => void;
  pills?: Pill[];
  user: string;
  onLogout?: () => void;
  title?: string;
  subtitle?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  const [theme, setTheme] = useState<Theme>(currentTheme);
  useEffect(() => applyTheme(theme), [theme]);
  let lastGroup = "";
  return (
    <div className="shell">
      <aside className="shell-side">
        <BrandMark />
        <nav className="shell-nav">
          {items.map((it) => {
            const group = it.group && it.group !== lastGroup ? it.group : "";
            lastGroup = it.group || "";
            return (
              <span key={it.id} className="shell-nav-item">
                {group && <span className="shell-nav-group">{group}</span>}
                <a
                  href={"#/" + encodeURIComponent(it.id)}
                  className={it.id === active ? "active" : ""}
                  onClick={(e) => {
                    e.preventDefault();
                    onSelect(it.id);
                  }}
                >
                  {it.label}
                  {it.warn && <span className="shell-nav-warn" title="требует внимания" />}
                </a>
              </span>
            );
          })}
        </nav>
        <div className="shell-actions">
          {pills.length > 0 && (
            <div className="shell-pills">
              {pills.map((p) => (
                <span key={p.label} className="shell-pill" title={p.title}>
                  <span className={`shell-pill-dot ${p.tone || "muted"}`} />
                  {p.label}
                </span>
              ))}
            </div>
          )}
          <span className="shell-user">
            <span className="shell-user-dot" />
            {user}
          </span>
          <div className="shell-buttons">
            <button className="shell-btn" onClick={() => setTheme(toggleTheme(theme))} title="Светлая / тёмная тема">
              {theme === "dark" ? "Светлая" : "Тёмная"}
            </button>
            {onLogout && (
              <button className="shell-btn" onClick={onLogout}>
                Выйти
              </button>
            )}
          </div>
        </div>
      </aside>
      <main className="shell-page">
        {(title || actions) && (
          <div className="page-head">
            <div>
              {title && <h1>{title}</h1>}
              {subtitle && <p className="muted small">{subtitle}</p>}
            </div>
            {actions && <div className="page-head-actions">{actions}</div>}
          </div>
        )}
        {children}
      </main>
    </div>
  );
}

// Panel — блок раздела с подписью в стиле оболочки.
export function Panel({ label, children, aside }: { label: string; children: ReactNode; aside?: ReactNode }) {
  return (
    <section className="panel">
      <div className="panel-head">
        <span className="panel-label">{label}</span>
        {aside}
      </div>
      {children}
    </section>
  );
}
