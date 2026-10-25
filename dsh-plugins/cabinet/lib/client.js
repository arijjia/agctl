// agctl cabinet v3 — one personal runtime, projects, roles and SSH.
window.__ModuleLoader__.load({
	id: "@arijjia/agctl-cabinet",
	factory: (require) => {
		var module = { exports: {} };
		var exports = module.exports;
		Object.defineProperty(exports, Symbol.toStringTag, { value: "Module" });
		const React = require("react");
		const h = React.createElement;

		const PANEL_ID = "agctl-cabinet";
		let pluginCtx = null;

		function CabinetIcon() {
			return h(
				"svg",
				{
					width: 16,
					height: 16,
					viewBox: "0 0 16 16",
					fill: "none",
					stroke: "currentColor",
					strokeWidth: 1.4,
					strokeLinecap: "round",
					strokeLinejoin: "round",
					"aria-hidden": true,
				},
				h("rect", { x: 2, y: 2.5, width: 12, height: 11, rx: 2 }),
				h("path", { d: "M2 6.5h12" }),
				h("path", { d: "M6.5 6.5v7" }),
			);
		}

		const styles = {
			wrap: {
				height: "100%",
				boxSizing: "border-box",
				overflow: "auto",
				padding: "24px 28px",
				fontFamily: "inherit",
				color: "var(--dsw-alias-label-primary, inherit)",
			},
			title: { margin: "0 0 4px", fontSize: 20, fontWeight: 650 },
			subtitle: {
				margin: "0 0 20px",
				fontSize: 13,
				color: "var(--dsw-alias-label-tertiary, #888)",
			},
			card: {
				border: "1px solid var(--dsw-alias-border-l1, #ddd)",
				borderRadius: 10,
				padding: "14px 16px",
				marginBottom: 16,
				maxWidth: 620,
			},
			cardTitle: { margin: "0 0 10px", fontSize: 14, fontWeight: 600 },
			row: { display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" },
			input: {
				flex: 1,
				minWidth: 160,
				boxSizing: "border-box",
				padding: "6px 10px",
				fontSize: 13,
				borderRadius: 8,
				border: "1px solid var(--dsw-alias-border-l1, #ccc)",
				background: "transparent",
				color: "inherit",
			},
			button: {
				padding: "7px 13px",
				fontSize: 13,
				fontWeight: 650,
				borderRadius: 8,
				border: "1px solid #1459b8",
				background: "#1769d2",
				color: "#fff",
				cursor: "pointer",
				boxShadow: "0 1px 2px rgba(0,0,0,.16)",
			},
			field: { display: "flex", flexDirection: "column", gap: 6, width: "100%", marginTop: 10 },
			fieldLabel: { fontSize: 12, fontWeight: 600, color: "var(--dsw-alias-label-secondary, #555)" },
			formStack: { display: "flex", flexDirection: "column", gap: 10, maxWidth: 620 },
			logPanel: { marginTop: 8, padding: 12, border: "1px solid var(--dsw-alias-border-l1, #ddd)", borderRadius: 10, maxWidth: 900 },
			logs: { margin: "8px 0 0", padding: 12, maxHeight: 360, overflow: "auto", whiteSpace: "pre-wrap", overflowWrap: "anywhere", fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace", fontSize: 12, color: "var(--dsw-alias-label-primary, #222)", background: "var(--dsw-alias-bg-module-platform, rgba(0,0,0,.04))", borderRadius: 8 },

			place: {
				display: "flex",
				justifyContent: "space-between",
				alignItems: "center",
				gap: 10,
				padding: "10px 12px",
				border: "1px solid var(--dsw-alias-border-l1, #ddd)",
				borderRadius: 10,
				marginBottom: 8,
				maxWidth: 620,
				flexWrap: "wrap",
			},
			placeName: { fontSize: 13, fontWeight: 600 },
			placeMeta: { fontSize: 12, color: "var(--dsw-alias-label-tertiary, #888)" },
			note: { fontSize: 12, color: "var(--dsw-alias-label-tertiary, #888)", marginTop: 14 },
			status: { fontSize: 12, marginTop: 8, whiteSpace: "pre-wrap" },
			mini: { padding: "5px 10px", fontSize: 12 },
		};

		function ensureCabinetStyles() {
			if (document.querySelector("style[data-agctl-cabinet-styles]")) return;
			const style = document.createElement("style");
			style.dataset.agctlCabinetStyles = "1";
			style.textContent = `
				[data-agctl-cabinet-root] button,[data-agctl-cabinet-root] a { transition: background .15s ease, box-shadow .15s ease, transform .15s ease; }
				[data-agctl-cabinet-root] button:hover:not(:disabled),[data-agctl-cabinet-root] a:hover { background:#1255aa!important; border-color:#1255aa!important; color:#fff!important; }
				[data-agctl-cabinet-root] button:focus-visible,[data-agctl-cabinet-root] a:focus-visible { outline:3px solid rgba(22,136,245,.35); outline-offset:2px; }
				[data-agctl-cabinet-root] button:disabled { opacity:.58; cursor:not-allowed!important; box-shadow:none; }
				[data-agctl-cabinet-root] input,[data-agctl-cabinet-root] textarea,[data-agctl-cabinet-root] select { min-height:38px; }
			`;
			document.head.appendChild(style);
		}

		function CabinetPage() {
			const [phase, setPhase] = React.useState("connecting");
			const [me, setMe] = React.useState(null);
			const [login, setLogin] = React.useState("");
			const [password, setPassword] = React.useState("");
			const [motivation, setMotivation] = React.useState("");
			const [requests, setRequests] = React.useState([]);
			const [projects, setProjects] = React.useState([]);
			const [name, setName] = React.useState("");
			const [cloneUrl, setCloneUrl] = React.useState("");
			const [pubKey, setPubKey] = React.useState("");
			const [sshPort, setSSHPort] = React.useState(0);
			const [sshLoaded, setSSHLoaded] = React.useState(false);
			const [runtime, setRuntime] = React.useState(null);
			const [runtimeLogs, setRuntimeLogs] = React.useState("");
			const [busy, setBusy] = React.useState(false);
			const [msg, setMsg] = React.useState("");
			const isDev = me && ["developer", "admin"].includes(me.role);
			const api = async (path, method = "GET", body) => {
				const res = await fetch(path, {
					credentials: "include", method,
					headers: { Accept: "application/json", ...(body !== undefined ? { "Content-Type": "application/json" } : {}) },
					body: body === undefined ? undefined : JSON.stringify(body),
				});
				let data;
				try { data = await res.json(); } catch { data = null; }
				if (!res.ok) {
					if (res.status === 401 || (path === "/api/me" && res.status === 403)) { setMe(null); setPhase("login"); }
					throw new Error("HTTP " + res.status + ": " + ((data && data.error) || "Запрос не выполнен"));
				}
				return data;
			};
			const runtimeApi = async (path, method = "GET", body) => {
				const res = await fetch(path, {
					credentials: "include", method,
					headers: { Accept: "text/plain", ...(body !== undefined ? { "Content-Type": "application/json" } : {}) },
					body: body === undefined ? undefined : JSON.stringify(body),
				});
				if (!res.ok) {
					let data = null;
					try { data = await res.json(); } catch { data = null; }
					throw new Error("HTTP " + res.status + ": " + ((data && data.error) || "Запрос не выполнен"));
				}
				return res.text();
			};
			const refresh = async () => {
				setMe(null);
				setPhase("connecting");
				setSSHLoaded(false);
				const data = await api("/api/me");
				if (!data || !data.user || !data.user.login || !data.user.role) throw new Error("Некорректный ответ /api/me");
				const user = data.user;
				setMe(user);
				setPhase("ready");
				setRequests([]);
				setProjects([]);
				const errors = [];
				try {
					const result = await api("/api/role-requests");
					setRequests(Array.isArray(result && result.requests) ? result.requests : []);
				} catch (err) { errors.push("Заявки: " + err.message); }
				try {
					const result = await api("/api/projects");
					const list = Array.isArray(result && result.projects) ? result.projects : [];
					setProjects(list);
					// Воркспейс = каталог проекта; регистрируем его настоящим DSH
					// Workspace, чтобы новые сессии получали cwd=/workspace/<name>.
					const workspaces = pluginCtx && pluginCtx.get && pluginCtx.get("workspaces");
					if (workspaces && typeof workspaces.create === "function") {
						for (const project of list) {
							if (project && project.directory) {
								try { await workspaces.create({ path: project.directory }); }
								catch (err) { console.warn("[agctl] DSH Workspace для воркспейса не зарегистрирован:", project.name, err); }
							}
						}
					}
				} catch (err) { errors.push("Воркспейсы: " + err.message); }
				if (["developer", "admin"].includes(user.role)) {
					try {
						const ssh = await api("/api/me/ssh");
						setPubKey(ssh.pubKey || ""); setSSHPort(ssh.sshPort || 0); setSSHLoaded(true);
					} catch (err) { errors.push("SSH: " + err.message); }
					try {
						setRuntime(await api("/api/me/runtime"));
					} catch (err) { errors.push("Рантайм: " + err.message); }
				} else {
					setRuntime(null);
				}
				if (errors.length) throw new Error(errors.join("\n"));
			};
			const run = async (action) => {
				setBusy(true); setMsg("");
				try { await action(); } catch (err) { setMsg(String(err.message || err)); }
				finally { setBusy(false); }
			};
			React.useEffect(() => { run(refresh); }, []);
			const submit = (action) => (e) => { e.preventDefault(); run(action); };
			const button = (text, props = {}) => h("button", { style: styles.button, disabled: busy, ...props }, text);
			const field = (label, node) => h("label", { style: styles.field }, h("span", { style: styles.fieldLabel }, label), node);
			const requestCard = (r, admin) => h("div", { key: r.id, style: styles.card },
				h("strong", null, r.login + " · " + r.status),
				h("p", { style: { whiteSpace: "pre-wrap", overflowWrap: "anywhere" } }, r.motivation),
				h("p", { style: styles.note }, "Создана: " + r.createdAt + (r.decidedBy ? " · Решение: " + r.decidedBy : "") + (r.decidedAt ? " · " + r.decidedAt : "")),
				admin && r.status === "pending" && h("div", { style: styles.row }, ...["approve", "reject"].map((decision) => button(decision === "approve" ? "Одобрить" : "Отклонить", {
					key: decision, onClick: () => run(async () => {
						await api("/api/role-requests/" + encodeURIComponent(r.id) + "/" + decision, "POST", {});
						await refresh(); setMsg("Решение сохранено.");
					}),
				}))),
			);
			const runtimeControls = isDev && h("section", { style: styles.card }, h("h2", { style: styles.cardTitle }, "Мой рантайм"),
				h("p", { style: styles.note }, runtime ? (runtime.status === "running" ? "Работает." + (runtime.sshPort > 0 ? " SSH-порт: " + runtime.sshPort : "") : "Спит (том и сессии сохранены).") : "Статус недоступен."),
				h("div", { style: styles.row },
					button("Запустить", { "aria-label": "Запустить рантайм", onClick: () => run(async () => {
						await api("/api/me/runtime/start", "POST", {}); setRuntime({ status: "running", sshPort: runtime && runtime.sshPort }); setMsg("Рантайм запущен.");
					}) }),
					button("Остановить", { "aria-label": "Остановить рантайм", onClick: () => run(async () => {
						await api("/api/me/runtime/stop", "POST", {}); setRuntime({ status: "sleeping", sshPort: 0 }); setRuntimeLogs(""); setMsg("Рантайм остановлен; данные сохранены.");
					}) }),
					button("Логи", { "aria-label": "Логи рантайма", onClick: () => run(async () => {
						setRuntimeLogs(await runtimeApi("/api/me/runtime/logs?tail=300")); setMsg("");
					}) }),
				),
				runtimeLogs && h("pre", { style: styles.logs, "aria-label": "Логи контейнера" }, runtimeLogs),
			);
			return h("div", { style: styles.wrap, "data-agctl-cabinet-root": "1" },
				h("h1", { style: styles.title }, "Кабинет agctl"),
				h("p", { style: styles.subtitle }, me ? me.login + " · " + me.role : "Личный кабинет"),
				msg && h("p", { style: styles.status, role: "status" }, msg),
				phase === "connecting" && h("p", { style: styles.note }, "Подключаюсь…"),
				phase === "login" && h("form", { style: styles.card, onSubmit: submit(async () => {
					await api("/api/login", "POST", { login, password }); setPassword(""); await refresh();
				}) }, h("h2", { style: styles.cardTitle }, "Вход"),
					field("Логин", h("input", { style: styles.input, value: login, required: true, autoComplete: "username", onChange: (e) => setLogin(e.target.value) })),
					field("Пароль", h("input", { style: styles.input, type: "password", value: password, required: true, autoComplete: "current-password", onChange: (e) => setPassword(e.target.value) })),
					button("Войти", { type: "submit" })),
				phase === "ready" && me && h(React.Fragment, null,
					me.role === "user" && h("form", { style: styles.card, onSubmit: submit(async () => {
						if (!motivation.trim()) throw new Error("Укажи мотивацию.");
						await api("/api/role-requests", "POST", { motivation: motivation.trim() }); setMotivation(""); await refresh(); setMsg("Заявка на роль developer отправлена.");
					}) }, h("h2", { style: styles.cardTitle }, "Заявка на роль developer"),
						field("Мотивация", h("textarea", { style: { ...styles.input, minHeight: 90 }, value: motivation, required: true, onChange: (e) => setMotivation(e.target.value) })),
						button("Отправить заявку", { type: "submit", disabled: busy || requests.some((r) => r.status === "pending") })),
					["user", "admin"].includes(me.role) && h("section", null,
						h("h2", { style: styles.cardTitle }, me.role === "admin" ? "Заявки на роль developer" : "Мои заявки"),
						requests.length ? requests.map((r) => requestCard(r, me.role === "admin")) : h("p", { style: styles.note }, "Заявок нет.")),
					isDev && h("form", { style: styles.card, onSubmit: submit(async () => {
						const ssh = await api("/api/me/ssh", "PUT", { pubKey: pubKey.trim() });
						setPubKey(ssh.pubKey || ""); setSSHPort(ssh.sshPort || 0); setSSHLoaded(true); setMsg("SSH-ключ сохранён.");
					}) }, h("h2", { style: styles.cardTitle }, "SSH"),
						field("Публичный SSH-ключ (пусто — отключить SSH)", h("textarea", { style: { ...styles.input, minHeight: 90 }, value: pubKey, disabled: !sshLoaded, placeholder: "ssh-ed25519 …", onChange: (e) => setPubKey(e.target.value) })),
						button("Сохранить ключ", { type: "submit", disabled: busy || !sshLoaded }),
						sshLoaded && (sshPort > 0 ? h("pre", { style: styles.logs }, "ssh root@127.0.0.1 -p " + sshPort) : h("p", { style: styles.note }, "Рантайм спит (sshPort = 0). Запусти его в «Моём рантайме» или открой чат.")),
						h("p", { style: styles.note }, "SSH слушает только loopback 127.0.0.1 сервера, не твоего компьютера. Для удалённого подключения нужен SSH-туннель. Приватный ключ не отправляй.")),
					runtimeControls,
					h("section", { style: styles.card }, h("h2", { style: styles.cardTitle }, "Воркспейсы"),
						h("form", { style: styles.formStack, onSubmit: submit(async () => {
							if (!name.trim()) throw new Error("Укажи имя воркспейса.");
							const body = { name: name.trim() };
							if (isDev && cloneUrl.trim()) body.cloneUrl = cloneUrl.trim();
							await api("/api/projects", "POST", body);
							setName(""); setCloneUrl(""); await refresh(); setMsg("Воркспейс создан.");
						}) },
							field("Имя", h("input", { style: styles.input, "aria-label": "Имя воркспейса", placeholder: "Имя воркспейса", value: name, required: true, onChange: (e) => setName(e.target.value) })),
							isDev && field("Git-репозиторий (необязательно; клонируется в воркспейс)", h("input", { style: styles.input, "aria-label": "Git URL воркспейса", placeholder: "https://github.com/user/repo.git", value: cloneUrl, onChange: (e) => setCloneUrl(e.target.value) })),
							h("div", { style: styles.row }, button("Добавить воркспейс", { type: "submit" }))),
						h("p", { style: styles.note }, "Воркспейсы появляются деревом в боковой панели чата; каждый — каталог /workspace/<имя> с локальной Git-историей. Автопуш во внешний Git не выполняется."),
						projects.length ? projects.map((project) => h("div", { key: project.name, style: { marginTop: 12 } },
							h("strong", null, project.name),
							h("div", { style: { ...styles.placeMeta, overflowWrap: "anywhere" } }, project.directory || ""))) : h("p", { style: styles.note }, "Воркспейсов пока нет.")),
				),
			);
		}

		const inject = ["slots", "layout"];

		function apply(ctx) {
			pluginCtx = ctx;
			ensureCabinetStyles();
			ctx.slots.inject("main", () =>
				ctx.slots.register(
					{
						name: "main",
						key: PANEL_ID,
						inject: () => ({}),
					},
					CabinetPage,
				),
			);
			ctx.slots.inject("sidebar.panellist", () =>
				ctx.slots.register(
					{
						name: "sidebar.panellist",
						id: PANEL_ID,
						order: 25,
						label: "Кабинет",
					},
					CabinetIcon,
				),
			);
		}

		exports.inject = inject;
		exports.apply = apply;
		exports.PANEL_ID = PANEL_ID;
		return module.exports;
	},
});
