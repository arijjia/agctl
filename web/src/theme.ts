// Тема лаунчера: тёмная по умолчанию (как всегда была), светлая — по выбору
// через кнопку в шапке. Выбор хранится в localStorage, поэтому вкладка
// браузера и встроенные табы dsh (тот же origin) показывают одно и то же.
// data-theme ставится только на <html> из лаунчера: остальные страницы agctl
// (серверный режим) выглядят как раньше.

export type Theme = "light" | "dark";

const KEY = "agctl-theme";

export function currentTheme(): Theme {
  const v = localStorage.getItem(KEY);
  return v === "light" || v === "dark" ? v : "dark";
}

export function applyTheme(t: Theme): void {
  document.documentElement.dataset.theme = t;
}

// toggleTheme переключает тему, сохраняет выбор и возвращает новое значение.
export function toggleTheme(t: Theme): Theme {
  const next: Theme = t === "dark" ? "light" : "dark";
  localStorage.setItem(KEY, next);
  applyTheme(next);
  return next;
}
