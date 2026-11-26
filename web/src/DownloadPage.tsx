import { useEffect, useState } from "react";
import { downloadsApi, type Downloads } from "./api";
import { Panel } from "./Shell";

// DownloadPage — «Скачать»: как разработчику поставить agctl на свой
// компьютер. Команды установки (комплект организации и нейтральный agctl),
// файлы, которые организация раздаёт со своего сервера, первые шаги и то,
// что установка положит на диск.

function Cmd({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="cmd">
      <pre className="mono">{text}</pre>
      <button
        className="ghost small"
        onClick={() => {
          navigator.clipboard?.writeText(text).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
          });
        }}
      >
        {copied ? "Скопировано" : "Копировать"}
      </button>
    </div>
  );
}

const fmtSize = (n: number) => (n > 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} МБ` : `${Math.max(1, Math.round(n / 1024))} КБ`);

export default function DownloadPage() {
  const [d, setD] = useState<Downloads | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    downloadsApi.get().then(setD).catch((e) => setErr(e.message));
  }, []);
  if (err) return <div className="error">{err}</div>;
  if (!d) return <p className="muted">Загрузка…</p>;
  const sh = `https://raw.githubusercontent.com/${d.coreRepo}/main/bin/install.sh`;
  const ps = `https://raw.githubusercontent.com/${d.coreRepo}/main/bin/install.ps1`;
  const kit = d.kitRepo;
  return (
    <div className="split">
      <div className="split-col">
        {kit && (
          <Panel label={`Комплект организации · профиль ${d.profile}`}>
            <p className="muted small">Бинарь agctl вместе с профилем организации. Установщик сам создаст дом и подскажет следующий шаг.</p>
            <div className="field-label">macOS и Linux</div>
            <Cmd text={`curl -fsSL ${sh} | AGCTL_REPO=${kit} sh`} />
            <div className="field-label">Windows (PowerShell)</div>
            <Cmd text={`$env:AGCTL_REPO="${kit}"; irm ${ps} | iex`} />
            <p className="small">
              <a href={`https://github.com/${kit}/releases`} target="_blank" rel="noreferrer">
                Релизы комплекта на GitHub →
              </a>
            </p>
          </Panel>
        )}
        <Panel label="Нейтральный agctl">
          <p className="muted small">Только ядро, без профиля организации. Профиль можно подключить позже.</p>
          <div className="field-label">macOS и Linux</div>
          <Cmd text={`curl -fsSL ${sh} | sh`} />
          <div className="field-label">Windows (PowerShell)</div>
          <Cmd text={`irm ${ps} | iex`} />
          <p className="small">
            <a href={`https://github.com/${d.coreRepo}`} target="_blank" rel="noreferrer">
              Исходный код →
            </a>{" "}
            ·{" "}
            <a href={`https://github.com/${d.coreRepo}/releases`} target="_blank" rel="noreferrer">
              Релизы →
            </a>
          </p>
        </Panel>
        <Panel label="Файлы с этого сервера">
          {d.files.length ? (
            <table className="data-table">
              <tbody>
                {d.files.map((f) => (
                  <tr key={f.name}>
                    <td className="mono">
                      <a href={f.url}>{f.name}</a>
                    </td>
                    <td className="muted">{fmtSize(f.size)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <p className="muted small">
              Файлов нет. Чтобы раздавать комплекты без доступа к GitHub, администратор кладёт архивы в{" "}
              <span className="mono">{d.downloadsDir}</span> — они появятся здесь.
            </p>
          )}
        </Panel>
      </div>
      <div className="split-col">
        <Panel label="Первые шаги после установки">
          <ol className="steps">
            <li>
              Запустите Docker Desktop (или Docker Engine на Linux).
            </li>
            <li>
              Поднимите рабочее место: <span className="mono">agctl dev up</span> — откроется лаунчер в браузере.
            </li>
            <li>В лаунчере на вкладке «Доступ» задайте ключ модели, при необходимости — токены GitLab и Jira.</li>
            <li>На вкладке «Рабочие места» выберите папку проектов и откройте чат с агентом.</li>
          </ol>
          <Cmd text={"agctl dev up        # поднять и открыть лаунчер\nagctl dev status    # что запущено\nagctl dev paths     # где что лежит на этой машине\nagctl dev doctor    # самодиагностика\nagctl dev down      # остановить"} />
        </Panel>
        <Panel label="Что установка положит на диск">
          <pre className="tree">{`~/.agctl/
├─ bin/               бинарь agctl и guest-бинари для сборки образа
├─ profiles/<id>/     профиль организации — единственный экземпляр
└─ homes/<id>/        дом: config.yaml, data/, logs/, run/, backups/`}</pre>
          <p className="muted small">
            Кроме этого agctl пишет только в папки, которые вы выберете: папку проектов (первый{" "}
            <span className="mono">agctl dev up</span> без настроенного дома берёт <span className="mono">~/src</span>) и
            свою папку dsh-home, если вы её зададите. Секреты хранятся в хранилище ОС, по записи на дом. Агент видит
            только папки своего рабочего места.
          </p>
        </Panel>
        <p className="muted small">Версия на этом сервере: <span className="mono">{d.version || "dev"}</span></p>
      </div>
    </div>
  );
}
