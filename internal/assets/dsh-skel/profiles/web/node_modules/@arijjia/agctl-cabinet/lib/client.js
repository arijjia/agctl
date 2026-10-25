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
		const URL_KEY = "agctl-cabinet.url";
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
			const [url, setUrl] = React.useState(() => { try { return localStorage.getItem(URL_KEY) || ""; } catch { return ""; } });
			const [phase, setPhase] = React.useState("connecting");
			const [me, setMe] = React.useState(null);
			const [login, setLogin] = React.useState("");
			const [password, setPassword] = React.useState("");
			const [motivation, setMotivation] = React.useState("");
			const [requests, setRequests] = React.useState([]);
			const [projects, setProjects] = React.useState([]);
			const [name, setName] = React.useState("");
			const [pubKey, setPubKey] = React.useState("");
			const [sshPort, setSSHPort] = React.useState(0);
			const [sshLoaded, setSSHLoaded] = React.useState(false);
			const [busy, setBusy] = React.useState(false);
			const [msg, setMsg] = React.useState("");
			const base = () => url.trim().replace(/\/+$/, "");
			const api = async (path, method = "GET", body) => {
				const res = await fetch(base() + path, {
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
					// Проектный каталог должен стать и настоящим DSH Workspace, чтобы
					// session.create(workspaceId) получил cwd=/workspace/<name>.
					const workspaces = pluginCtx && pluginCtx.get && pluginCtx.get("workspaces");
					if (workspaces && typeof workspaces.create === "function") {
						for (const project of list) {
							if (project && project.directory) {
								try { await workspaces.create({ path: project.directory }); }
								catch (err) { console.warn("[agctl] DSH Workspace для проекта не зарегистрирован:", project.name, err); }
							}
						}
					}
				} catch (err) { errors.push("Проекты: " + err.message); }
				if (["developer", "admin"].includes(user.role)) {
					try {
						const ssh = await api("/api/me/ssh");
						setPubKey(ssh.pubKey || ""); setSSHPort(ssh.sshPort || 0); setSSHLoaded(true);
					} catch (err) { errors.push("SSH: " + err.message); }
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
			return h("div", { style: styles.wrap, "data-agctl-cabinet-root": "1" },
				h("h1", { style: styles.title }, "Кабинет agctl"),
				h("p", { style: styles.subtitle }, me ? me.login + " · " + me.role : "Личный кабинет"),
				h("section", { style: styles.card },
					h("h2", { style: styles.cardTitle }, "Подключение к agctl"),
					h("div", { style: styles.row }, h("input", { style: styles.input, "aria-label": "URL agctl", placeholder: "URL agctl (пусто = тот же origin)", value: url,
						onChange: (e) => { setUrl(e.target.value); try { localStorage.setItem(URL_KEY, e.target.value.trim()); } catch {} },
					}), button("Обновить", { onClick: () => run(refresh) })),
				),
				msg && h("p", { style: styles.status, role: "status" }, msg),
				phase === "connecting" && h("p", { style: styles.note }, "Подключение к agctl: нажми «Обновить», если сервер недоступен."),
				phase === "login" && h("form", { style: styles.card, onSubmit: submit(async () => {
					await api("/api/login", "POST", { login, password }); setPassword(""); await refresh();
				}) }, h("h2", { style: styles.cardTitle }, "Вход"),
					field("Логин", h("input", { style: styles.input, value: login, required: true, autoComplete: "username", onChange: (e) => setLogin(e.target.value) })),
					field("Пароль", h("input", { style: styles.input, type: "password", value: password, required: true, autoComplete: "current-password", onChange: (e) => setPassword(e.target.value) })),
					button("Войти", { type: "submit" })),
				phase === "ready" && me && h(React.Fragment, null,
					h("section", { style: styles.card }, h("h2", { style: styles.cardTitle }, "Личный runtime"),
						h("a", { href: base() + "/chat/", style: { ...styles.button, display: "inline-block", textDecoration: "none" } }, "Открыть личный чат"),
						h("p", { style: styles.note }, "Один личный runtime для чата и работы с проектами.")),
					me.role === "user" && h("form", { style: styles.card, onSubmit: submit(async () => {
						if (!motivation.trim()) throw new Error("Укажи мотивацию.");
						await api("/api/role-requests", "POST", { motivation: motivation.trim() }); setMotivation(""); await refresh(); setMsg("Заявка на роль developer отправлена.");
					}) }, h("h2", { style: styles.cardTitle }, "Заявка на роль developer"),
						field("Мотивация", h("textarea", { style: { ...styles.input, minHeight: 90 }, value: motivation, required: true, onChange: (e) => setMotivation(e.target.value) })),
						button("Отправить заявку", { type: "submit", disabled: busy || requests.some((r) => r.status === "pending") })),
					["user", "admin"].includes(me.role) && h("section", null,
						h("h2", { style: styles.cardTitle }, me.role === "admin" ? "Заявки на роль developer" : "Мои заявки"),
						requests.length ? requests.map((r) => requestCard(r, me.role === "admin")) : h("p", { style: styles.note }, "Заявок нет.")),
					["developer", "admin"].includes(me.role) && h("form", { style: styles.card, onSubmit: submit(async () => {
						const ssh = await api("/api/me/ssh", "PUT", { pubKey: pubKey.trim() });
						setPubKey(ssh.pubKey || ""); setSSHPort(ssh.sshPort || 0); setSSHLoaded(true); setMsg("SSH-ключ сохранён.");
					}) }, h("h2", { style: styles.cardTitle }, "SSH"),
						field("Публичный SSH-ключ (пусто — отключить SSH)", h("textarea", { style: { ...styles.input, minHeight: 90 }, value: pubKey, disabled: !sshLoaded, placeholder: "ssh-ed25519 …", onChange: (e) => setPubKey(e.target.value) })),
						button("Сохранить ключ", { type: "submit", disabled: busy || !sshLoaded }),
						sshLoaded && (sshPort > 0 ? h("pre", { style: styles.logs }, "ssh root@127.0.0.1 -p " + sshPort) : h("p", { style: styles.note }, "Runtime спит (sshPort = 0). Открой личный чат и обнови кабинет.")),
						h("p", { style: styles.note }, "SSH слушает только loopback 127.0.0.1 сервера, не твоего компьютера. Для удалённого подключения нужен SSH-туннель. Приватный ключ не отправляй.")),
					h("section", { style: styles.card }, h("h2", { style: styles.cardTitle }, "Проекты"),
						h("form", { style: styles.row, onSubmit: submit(async () => {
							if (!name.trim()) throw new Error("Укажи имя проекта.");
							await api("/api/projects", "POST", { name: name.trim() }); setName(""); await refresh(); setMsg("Проект создан.");
						}) }, h("input", { style: styles.input, "aria-label": "Имя проекта", placeholder: "Имя проекта", value: name, required: true, onChange: (e) => setName(e.target.value) }), button("Создать проект", { type: "submit" })),
						projects.length ? projects.map((project) => h("div", { key: project.name, style: { marginTop: 12 } },
							h("strong", null, project.name), h("div", { style: { ...styles.placeMeta, overflowWrap: "anywhere" } }, project.git || ""))) : h("p", { style: styles.note }, "Проектов пока нет.")),
				),
			);
		}

		const inject = ["slots", "layout"];

		const CHAT_ACCENT = "#1676df";
		const FOLDER_PATH = "M1.5 4.2c0-.9.7-1.6 1.6-1.6h3l1.6 1.8h5.2c.9 0 1.6.7 1.6 1.6v6c0 .9-.7 1.6-1.6 1.6H3.1c-.9 0-1.6-.7-1.6-1.6v-7.8z";

		function el(tag, style, text) {
			const node = document.createElement(tag);
			if (style) node.style.cssText = style;
			if (text !== undefined) node.textContent = text;
			return node;
		}

		function folderIcon(color) {
			const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
			svg.setAttribute("width", "15");
			svg.setAttribute("height", "15");
			svg.setAttribute("viewBox", "0 0 16 16");
			svg.setAttribute("fill", "none");
			svg.setAttribute("aria-hidden", "true");
			for (const [fill, stroke] of [[color, "none"], ["none", color]]) {
				const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
				path.setAttribute("d", FOLDER_PATH);
				path.setAttribute("fill", fill);
				path.setAttribute("stroke", stroke);
				path.setAttribute("stroke-width", "1.3");
				path.setAttribute("stroke-linejoin", "round");
				if (fill !== "none") path.setAttribute("opacity", "0.22");
				svg.appendChild(path);
			}
			return svg;
		}

		function navRow(href, label, color, extra) {
			const active = decodeURIComponent(location.pathname) === href || (href !== "/chat/" && location.pathname.startsWith(href));
			const row = el("a", "display:flex;align-items:center;gap:8px;padding:5px 10px;margin-left:4px;border-radius:8px;text-decoration:none;font-size:13px;color:inherit;cursor:pointer;background:" + (active ? "var(--dsw-alias-interactive-bg-hover, rgba(0,0,0,.06))" : "transparent") + ";font-weight:" + (active ? 650 : 400));
			row.dataset.agctlNavRow = "1";
			row.href = href;
			row.title = label;
			row.appendChild(folderIcon(color));
			const text = el("span", "flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap", label);
			text.dataset.agctlNavLabel = "1";
			row.appendChild(text);
			if (extra) row.appendChild(extra);
			return row;
		}

		function refreshNav(root) {
			const nav = el("div", "display:flex;flex-direction:column;gap:2px;padding:6px 4px 10px;min-width:0");
			nav.setAttribute("data-agctl-nav", "1");
			nav.appendChild(navRow("/chat/", "Личный чат", CHAT_ACCENT));
			root.replaceChildren(nav);
			syncNavWidth(root);
		}

		function syncNavWidth(root) {
			const nav = root && root.querySelector('[data-agctl-nav]');
			if (!nav) return;
			const sidebar = document.querySelector('[class*="hHd-Xa_root"]');
			const collapsed = !!(sidebar && sidebar.classList.contains('hHd-Xa_collapsed'));
			nav.dataset.collapsed = collapsed ? "1" : "0";
			nav.style.alignItems = collapsed ? "center" : "stretch";
			nav.style.padding = collapsed ? "6px 0 10px" : "6px 4px 10px";
			const heading = nav.querySelector('[data-agctl-nav-heading]');
			if (heading) heading.style.display = collapsed ? "none" : "block";
			for (const row of nav.querySelectorAll('[data-agctl-nav-row]')) {
				row.style.width = collapsed ? "38px" : "auto";
				row.style.height = collapsed ? "36px" : "auto";
				row.style.padding = collapsed ? "7px 0" : "5px 10px";
				row.style.margin = collapsed ? "0 auto" : "0 4px";
				row.style.justifyContent = collapsed ? "center" : "flex-start";
				const label = row.querySelector('[data-agctl-nav-label]');
				if (label) label.style.display = collapsed ? "none" : "block";
			}
		}

		function mountNav() {
			if (document.querySelector('[data-agctl-nav-host]')) return;
			const tree = document.querySelector('[role="tree"]');
			if (!tree) return;
			const host = tree.closest('[class*="root"]') || tree.parentElement;
			if (!host || !host.parentElement) return;
			// Сырой DOM (не React): слот живёт вне React-дерева dsh.
			const slot = document.createElement("div");
			slot.setAttribute("data-agctl-nav-host", "1");
			host.parentElement.insertBefore(slot, host);
			refreshNav(slot);
			const sidebar = tree.closest('[class*="hHd-Xa_root"]');
			if (sidebar && !window.__agctlNavWidthObserver) {
				window.__agctlNavWidthObserver = new MutationObserver(() => syncNavWidth(slot));
				window.__agctlNavWidthObserver.observe(sidebar, { attributes: true, attributeFilter: ["class"] });
			}
		}

		function apply(ctx) {
			pluginCtx = ctx;
			ensureCabinetStyles();
			const startNav = () => {
				mountNav();
				if (!window.__agctlNavObserver) {
					window.__agctlNavObserver = new MutationObserver(() => mountNav());
					window.__agctlNavObserver.observe(document.body, { childList: true, subtree: true });
				}
			};
			if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", startNav);
			else startNav();
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
