import { useEffect, useState } from "react";
import { api } from "./api";

// Фронт agctl — только страница входа/регистрации. Всё остальное (чат,
// воркспейсы, профиль) живёт в dsh; после успешного входа корень платформы
// перенаправляет в /chat/.
function Login({ onDone }: { onDone: () => void }) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      if (mode === "login") await api.login(login, password);
      else await api.register(login, password);
      onDone();
    } catch (err: any) {
      setError(err.message || String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="center-wrap">
      <form className="card login-card" onSubmit={submit}>
        <h1>agctl</h1>
        <p className="muted">ИИ-мастерская ТюмГУ: вход в личное рабочее место</p>
        <div className="tabs">
          <button type="button" className={mode === "login" ? "tab active" : "tab"} onClick={() => setMode("login")}>
            Вход
          </button>
          <button type="button" className={mode === "register" ? "tab active" : "tab"} onClick={() => setMode("register")}>
            Регистрация
          </button>
        </div>
        <label>
          Логин
          <input value={login} onChange={(e) => setLogin(e.target.value)} autoFocus minLength={3} required />
        </label>
        <label>
          Пароль
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} minLength={6} required />
        </label>
        {error && <div className="error">{error}</div>}
        <button className="primary" disabled={busy}>
          {busy ? "…" : mode === "login" ? "Войти" : "Создать аккаунт"}
        </button>
        <p className="muted small">Первый зарегистрированный пользователь становится администратором.</p>
      </form>
    </div>
  );
}

export default function App() {
  const [checking, setChecking] = useState(true);

  useEffect(() => {
    // Уже залогинен — корень сам перенаправит в /chat/.
    api
      .me()
      .then(() => window.location.replace("/"))
      .catch(() => setChecking(false));
  }, []);

  if (checking) {
    return (
      <div className="center-wrap">
        <span className="spinner big" />
      </div>
    );
  }
  return <Login onDone={() => window.location.replace("/")} />;
}
