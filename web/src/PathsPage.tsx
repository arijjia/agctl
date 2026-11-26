import { useEffect, useState } from "react";
import { pathsApi, type PathItem, type PathsReport } from "./api";
import { Panel } from "./Shell";

// PathsPage — «Где что лежит»: всё, что agctl держит на этой машине. Тот же
// отчёт печатает `agctl dev paths`: корень, бинари, профиль, дом и каждый
// файл в нём, порты, контейнеры, маунты и место хранения секретов.

function Row({ name, path, purpose, exists }: PathItem) {
  return (
    <tr>
      <td className="path-mark">
        <span className={`dot ${exists ? "ok" : "muted"}`} title={exists ? "есть на диске" : "появится по мере работы"} />
      </td>
      <td className="mono">{name}</td>
      <td className="mono path-cell">{path}</td>
      <td className="muted">{purpose}</td>
    </tr>
  );
}

function Files({ items }: { items: PathItem[] }) {
  return (
    <table className="data-table">
      <tbody>
        {items.map((i) => (
          <Row key={i.path + i.name} {...i} />
        ))}
      </tbody>
    </table>
  );
}

const KV = ({ k, children }: { k: string; children: React.ReactNode }) => (
  <div className="kv">
    <span className="kv-key">{k}</span>
    <span className="kv-val">{children}</span>
  </div>
);

export default function PathsPage() {
  const [r, setR] = useState<PathsReport | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    pathsApi.get().then(setR).catch((e) => setErr(e.message));
  }, []);
  if (err) return <div className="error">{err}</div>;
  if (!r) return <p className="muted">Загрузка…</p>;
  return (
    <>
      <div className="layout-map mono">
        <div>
          <b>{r.root.path}</b> <span className="muted">— корень: всё, что ставит agctl, лежит здесь</span>
        </div>
        <div>├─ bin/ <span className="muted">— бинарь agctl и guest-бинари для сборки образа</span></div>
        <div>├─ profiles/{r.profile.id || "<id>"}/ <span className="muted">— профиль организации, единственный экземпляр</span></div>
        <div>└─ homes/{r.home.id}/ <span className="muted">— дом: настройки, данные, логи, состояние</span></div>
        <div className="layout-map-extra">
          <b>{r.workdir || "папка проектов не задана"}</b> <span className="muted">— папка проектов: единственное, что агент видит на этой машине</span>
        </div>
      </div>

      <div className="split">
        <div className="split-col">
          <Panel label="Бинари">
            <Files items={[...(r.binaries || []), ...(r.guest?.files || [])]} />
          </Panel>
          <Panel label="Профиль">
            <KV k="id">{r.profile.id || "—"}</KV>
            <KV k="каталог">
              <span className="mono">{r.profile.dir || "встроен в бинарь"}</span>
            </KV>
            {r.profile.link && (
              <KV k="ссылка на">
                <span className="mono">{r.profile.link}</span>
              </KV>
            )}
            <KV k="чем выбран">{r.profile.origin || "—"}</KV>
            <KV k="в корне есть">{r.profile.available?.join(", ") || "—"}</KV>
            {r.profile.error && <div className="error">{r.profile.error}</div>}
          </Panel>
          <Panel label="Сеть и порты">
            <KV k="интерфейс">
              <span className="mono">{r.ui.url}</span> <span className="muted">— только этот компьютер (loopback)</span>
            </KV>
            <KV k="мост к хосту">
              <span className="mono">{r.bridge.url}</span> <span className="muted">— так его видит контейнер; инструментов: {r.bridge.tools}</span>
            </KV>
            <KV k="сеть egress">
              <span className="mono">{r.docker.egressNetwork}</span>{" "}
              <span className="muted">{r.docker.egressEnabled ? "— включена: наружу только разрешённые хосты" : "— выключена: контейнер в обычной сети Docker"}</span>
            </KV>
          </Panel>
          <Panel label="Секреты">
            <KV k="хранилище">{r.secrets.backend}</KV>
            {r.secrets.service && (
              <KV k="сервис">
                <span className="mono">{r.secrets.service}</span>
              </KV>
            )}
            {r.secrets.file && (
              <KV k="файл">
                <span className="mono">{r.secrets.file}</span>
              </KV>
            )}
            <KV k="имена">
              <span className="mono">{r.secrets.names?.join(", ") || "—"}</span> <span className="muted">— значений в файлах настроек и логах нет</span>
            </KV>
          </Panel>
        </div>
        <div className="split-col">
          <Panel label={`Дом ${r.home.id}`} aside={<span className="muted small">{r.home.source}</span>}>
            <Files items={r.home.files || []} />
            {r.home.others?.length > 0 && <p className="muted small">Другие дома: {r.home.others.join(", ")}</p>}
          </Panel>
          <Panel label="Образ и контейнеры">
            <KV k="образ">
              <span className="mono">{r.image.ref}</span> <span className="muted">— {r.image.source}</span>
            </KV>
            <table className="data-table">
              <tbody>
                {(r.docker.containers || []).map((c) => (
                  <tr key={c.name}>
                    <td className="mono">{c.name}</td>
                    <td className="muted">{c.purpose}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Panel>
          <Panel label="Что из этой машины видно в контейнере">
            <table className="data-table">
              <thead>
                <tr>
                  <th>На этой машине</th>
                  <th>В контейнере</th>
                  <th>Зачем</th>
                </tr>
              </thead>
              <tbody>
                {(r.docker.mounts || []).map((m) => (
                  <tr key={m.host + m.container}>
                    <td className="mono path-cell">{m.host}</td>
                    <td className="mono">{m.container}</td>
                    <td className="muted">{m.purpose}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Panel>
        </div>
      </div>
      <p className="muted small">
        Тот же отчёт в терминале: <span className="mono">agctl dev paths</span>
      </p>
    </>
  );
}
