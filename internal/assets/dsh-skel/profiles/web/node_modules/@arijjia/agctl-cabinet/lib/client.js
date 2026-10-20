// agctl cabinet — client half (hand-written Lazy-CJS bundle).
//
// Формат повторяет собранные бандлы dsh (см. @deepseek-ai/dsh-client-ui-jobs):
// window.__ModuleLoader__.load({ id, factory(require) -> module.exports })
// с экспортами inject (имена сервисов для cordis-инъекции) и apply(ctx).
// Волна 1: панель «Кабинет» в root-scoped sidebar.panellist + страница в
// keyed slot "main" (тот же id) — паттерн dsh-client-ui-plugin-manager.
// Живой agctl REST — вход, /api/me, список/создание/старт/стоп воркспейсов,
// optional SSH public key/port. База адреса: явный URL (localStorage) либо
// относительный путь (панель через шлюз agctl /w/<id>/ — same-origin, cookie
// уже стоит). Сборка не нужна: файл пишется руками в формате хоста.
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
				padding: "6px 12px",
				fontSize: 13,
				borderRadius: 8,
				border: "1px solid var(--dsw-alias-border-l1, #ccc)",
				background: "var(--dsw-specific-button, #eee)",
				color: "inherit",
				cursor: "pointer",
			},
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
			mini: { padding: "3px 8px", fontSize: 12 },
		};

		function CabinetPage() {
			const [url, setUrl] = React.useState(() => {
				try {
					return localStorage.getItem(URL_KEY) || "";
				} catch (e) {
					return "";
				}
			});
			const [phase, setPhase] = React.useState("connecting"); // connecting | login | ready
			const [me, setMe] = React.useState(null);
			const [places, setPlaces] = React.useState([]);
			const [kits, setKits] = React.useState(["base"]);
			const [login, setLogin] = React.useState("");
			const [password, setPassword] = React.useState("");
			const [name, setName] = React.useState("");
			const [kit, setKit] = React.useState("base");
			const [llmBaseURL, setLLMBaseURL] = React.useState("");
			const [llmModel, setLLMModel] = React.useState("");
			const [llmAPIKey, setLLMAPIKey] = React.useState("");
			const [sshPubKey, setSSHPubKey] = React.useState("");
			const [busy, setBusy] = React.useState(false);
			const [msg, setMsg] = React.useState("");
			const [cfg, setCfg] = React.useState({ requireApproval: false });
			const [myRequests, setMyRequests] = React.useState([]);
			const [adminRequests, setAdminRequests] = React.useState([]);
			const [localEnabled, setLocalEnabled] = React.useState(null); // null = unknown
			const [localConnected, setLocalConnected] = React.useState(false);
			const [localGrant, setLocalGrant] = React.useState(null);

			const base = () => url.trim().replace(/\/+$/, "");

			const api = async (path, opts) => {
				const res = await fetch(base() + path, {
					credentials: "include",
					headers: Object.assign(
						{ Accept: "application/json" },
						opts && opts.body ? { "Content-Type": "application/json" } : {},
					),
					method: (opts && opts.method) || "GET",
					body: opts && opts.body,
				});
				const text = await res.text();
				let data = null;
				try {
					data = text ? JSON.parse(text) : null;
				} catch (e) {
					data = null;
				}
				return { ok: res.ok, status: res.status, data };
			};

			const load = async () => {
				setBusy(true);
				try {
					const meRes = await api("/api/me");
					if (!meRes.ok) {
						setMe(null);
						setPhase(meRes.status === 401 || meRes.status === 403 ? "login" : "connecting");
						setMsg("Сервер отвечает (HTTP " + meRes.status + "): нужна сессия кабинета.");
						return;
					}
					const currentUser = meRes.data && meRes.data.user ? meRes.data.user : meRes.data;
					setMe(currentUser);
					const wsRes = await api("/api/workspaces");
					setPlaces(
						wsRes.ok && wsRes.data && Array.isArray(wsRes.data.workspaces) ? wsRes.data.workspaces : [],
					);
					const kitRes = await api("/api/kits");
					if (kitRes.ok && kitRes.data) {
						const list = Array.isArray(kitRes.data.kits) ? kitRes.data.kits : [];
						const names = list
							.map((k) => (typeof k === "string" ? k : k && (k.id || k.name)))
							.filter(Boolean);
						if (names.length) setKits(names);
					}
					const cfgRes = await api("/api/config");
					if (cfgRes.ok && cfgRes.data) setCfg(cfgRes.data);
					const reqRes = await api("/api/workspace-requests");
					if (reqRes.ok && reqRes.data && Array.isArray(reqRes.data.requests)) {
						const requestList = reqRes.data.requests;
						setAdminRequests(currentUser.role === "admin" ? requestList.filter((r) => r.status === "pending") : []);
						setMyRequests(currentUser.role === "admin" ? requestList.filter((r) => r.login === currentUser.login) : requestList);
					} else {
						setAdminRequests([]);
						setMyRequests([]);
					}
					const localRes = await api("/api/local-backend/status");
					if (localRes.ok && localRes.data) {
						setLocalEnabled(true);
						setLocalConnected(!!localRes.data.connected);
					} else {
						// Flag off (404): the experimental connector is not configured.
						setLocalEnabled(false);
						setLocalConnected(false);
					}
					setPhase("ready");
					setMsg("");
				} catch (e) {
					setPhase("connecting");
					setMsg("Не отвечает: " + String((e && e.message) || e));
				} finally {
					setBusy(false);
				}
			};

			React.useEffect(() => {
				load();
			}, []);

			const issueLocalGrant = async () => {
				setBusy(true);
				try {
					const res = await api("/api/local-backend/grant", { method: "POST" });
					if (res.ok && res.data && res.data.grant) {
						setLocalGrant(res.data);
						setMsg("");
					} else {
						setMsg("Не удалось создать pairing grant (HTTP " + res.status + ").");
					}
				} finally {
					setBusy(false);
				}
			};

			const revokeLocalBackend = async () => {
				setBusy(true);
				try {
					await api("/api/local-backend", { method: "DELETE" });
					setLocalGrant(null);
					const res = await api("/api/local-backend/status");
					if (res.ok && res.data) setLocalConnected(!!res.data.connected);
					setMsg("Локальный коннектор отключён.");
				} finally {
					setBusy(false);
				}
			};

			const doLogin = async (e) => {
				e.preventDefault();
				setBusy(true);
				setMsg("Вхожу…");
				try {
					const res = await api("/api/login", {
						method: "POST",
						body: JSON.stringify({ login, password }),
					});
					if (!res.ok) {
						setMsg(
							"Вход не удался (HTTP " + res.status + "): " + ((res.data && res.data.error) || "проверь логин/пароль"),
						);
						return;
					}
					setPassword("");
					await load();
				} catch (err) {
					setMsg("Вход не удался: " + String((err && err.message) || err));
				} finally {
					setBusy(false);
				}
			};

			const doCreate = async (e) => {
				e.preventDefault();
				const clean = name.trim();
				if (!clean) {
					setMsg("Придумай имя рабочего места");
					return;
				}
				if (!llmBaseURL.trim() || !llmModel.trim()) {
					setMsg("Укажи Base URL и модель LLM-провайдера (токен можно оставить пустым, если он уже сохранён)");
					return;
				}
				const payload = {
					name: clean,
					kit,
					baseURL: llmBaseURL.trim(),
					models: [llmModel.trim()],
					apiKey: llmAPIKey.trim(),
					sshPubKey: sshPubKey.trim(),
				};
				const endpoint = cfg.requireApproval ? "/api/workspace-requests" : "/api/workspaces";
				setBusy(true);
				setMsg(
					cfg.requireApproval
						? "Отправляю заявку «" + clean + "» администратору…"
						: "Создаю «" + clean + "» (это занимает несколько секунд)…",
				);
				try {
					const res = await api(endpoint, {
						method: "POST",
						body: JSON.stringify(payload),
					});
					if (!res.ok) {
						setMsg("Не создалось (HTTP " + res.status + "): " + ((res.data && res.data.error) || ""));
						return;
					}
					setName("");
					setLLMAPIKey("");
					setSSHPubKey("");
					setMsg(
						cfg.requireApproval
							? "Заявка «" + clean + "» отправлена администратору — статус появится в «Мои заявки»."
							: "Готово: место «" + clean + "» создано.",
					);
					await load();
				} catch (err) {
					setMsg("Не создалось: " + String((err && err.message) || err));
				} finally {
					setBusy(false);
				}
			};

			const decideRequest = (request, decision) => async () => {
				setBusy(true);
				setMsg((decision === "approve" ? "Одобряю" : "Отклоняю") + " заявку «" + request.name + "»…");
				try {
					const res = await api("/api/workspace-requests/" + request.id + "/" + decision, {
						method: "POST",
						body: JSON.stringify(decision === "reject" ? { reason: "Отклонено администратором" } : {}),
					});
					if (!res.ok) {
						setMsg("Решение не сохранено (HTTP " + res.status + "): " + ((res.data && res.data.error) || ""));
						return;
					}
					setMsg(decision === "approve" ? "Заявка одобрена, workspace создаётся." : "Заявка отклонена.");
					await load();
				} catch (err) {
					setMsg("Ошибка решения: " + String((err && err.message) || err));
				} finally {
					setBusy(false);
				}
			};

			const act = (ws, action) => async () => {
				setBusy(true);
				setMsg((action === "start" ? "Запускаю" : "Останавливаю") + " «" + ws.name + "»…");
				try {
					const res = await api("/api/workspaces/" + ws.id + "/" + action, { method: "POST" });
					setMsg(res.ok ? "Готово." : "HTTP " + res.status + ": " + ((res.data && res.data.error) || ""));
					await load();
				} finally {
					setBusy(false);
				}
			};

			const saveUrl = (v) => {
				setUrl(v);
				try {
					localStorage.setItem(URL_KEY, v.trim());
				} catch (e) {}
			};

			const conn = h(
				"div",
				{ style: styles.card },
				h("h2", { style: styles.cardTitle }, "Подключение к agctl"),
				h(
					"div",
					{ style: styles.row },
					h("input", {
						style: styles.input,
						placeholder: "https://agctl.example.org (пусто = тот же origin)",
						value: url,
						onChange: (e) => saveUrl(e.target.value),
						onKeyDown: (e) => {
							if (e.key === "Enter") load();
						},
					}),
					h("button", { style: styles.button, onClick: load, disabled: busy }, "Обновить"),
				),
				msg && h("p", { style: styles.status }, msg),
			);

			if (phase === "login") {
				return h(
					"div",
					{ style: styles.wrap },
					h("h1", { style: styles.title }, "Кабинет agctl"),
					h("p", { style: styles.subtitle }, "Войди, чтобы видеть свои рабочие места с агентами."),
					conn,
					h(
						"form",
						{ style: styles.card, onSubmit: doLogin },
						h("h2", { style: styles.cardTitle }, "Вход"),
						h(
							"div",
							{ style: styles.row },
							h("input", {
								style: styles.input,
								placeholder: "логин",
								value: login,
								onChange: (e) => setLogin(e.target.value),
							}),
							h("input", {
								style: styles.input,
								type: "password",
								placeholder: "пароль",
								value: password,
								onChange: (e) => setPassword(e.target.value),
							}),
							h("button", { style: styles.button, type: "submit", disabled: busy }, "Войти"),
						),
					),
				);
			}

			return h(
				"div",
				{ style: styles.wrap },
				h("h1", { style: styles.title }, "Кабинет agctl"),
				h(
					"p",
					{ style: styles.subtitle },
					(me
						? "Вошёл как " + me.login + (me.role === "admin" ? " (admin)" : "") + ". "
						: "") + "Дефолтный чат этой среды — базовый уровень платформы (Tier 0).",
				),
				conn,
				phase === "ready" &&
					h(
						"form",
						{ style: styles.card, onSubmit: doCreate },
						h("h2", { style: styles.cardTitle }, "Новое рабочее место"),
						h(
							"div",
							{ style: styles.row },
							h("input", {
								style: styles.input,
								placeholder: "имя (например, demo-1c)",
								value: name,
								onChange: (e) => setName(e.target.value),
							}),
							h(
								"select",
								{ style: { ...styles.input, maxWidth: 140 }, value: kit, onChange: (e) => setKit(e.target.value) },
								kits.map((k) => h("option", { value: k, key: k }, k)),
							),
							h("button", { style: styles.button, type: "submit", disabled: busy }, cfg.requireApproval ? "Запросить" : "Создать"),
						),
						h(
							"div",
							{ style: { ...styles.row, marginTop: 10 } },
							h("input", {
								style: styles.input,
								placeholder: "LLM Base URL (например, https://litellm.example/v1)",
								value: llmBaseURL,
								onChange: (e) => setLLMBaseURL(e.target.value),
							}),
							h("input", {
								style: styles.input,
								placeholder: "модель (например, gpt-4.1)",
								value: llmModel,
								onChange: (e) => setLLMModel(e.target.value),
							}),
						),
						h("input", {
							style: { ...styles.input, width: "100%", marginTop: 10 },
							type: "password",
							placeholder: "API-токен (не сохраняется в браузере; пусто = использовать сохранённый)",
							value: llmAPIKey,
							autoComplete: "new-password",
							onChange: (e) => setLLMAPIKey(e.target.value),
						}),
						h("textarea", {
							style: { ...styles.input, width: "100%", minHeight: 58, marginTop: 10, resize: "vertical" },
							placeholder: "SSH public key (необязательно; например, содержимое ~/.ssh/id_ed25519.pub)",
							value: sshPubKey,
							onChange: (e) => setSSHPubKey(e.target.value),
						}),
						h("p", { style: styles.note }, "SSH доступен только по ключу и привязан к 127.0.0.1 сервера; для удалённой машины сначала нужен SSH-туннель до сервера."),
					),
				phase === "ready" &&
					h(
						"div",
						null,
						me && me.role === "admin" && cfg.requireApproval &&
							h(
								"section",
								null,
								h("h2", { style: styles.cardTitle }, "Заявки на апрув (администратор)"),
								adminRequests.length === 0 && h("p", { style: styles.note }, "Нет ожидающих заявок."),
								adminRequests.map((r) =>
									h(
										"div",
										{ style: styles.place, key: r.id },
										h(
											"span",
											null,
											h("span", { style: styles.placeName }, r.name + " "),
											h("span", { style: styles.placeMeta }, "· " + r.login + " · " + r.kit),
										),
										h(
											"span",
											{ style: styles.row },
											h("button", { style: { ...styles.button, ...styles.mini }, onClick: decideRequest(r, "approve"), disabled: busy }, "Одобрить"),
											h("button", { style: { ...styles.button, ...styles.mini }, onClick: decideRequest(r, "reject"), disabled: busy }, "Отклонить"),
										),
									),
								),
							),
						),
						h("h2", { style: styles.cardTitle }, "Мои заявки"),
						myRequests.length === 0
							? h("p", { style: styles.note }, "Заявок нет.")
							: myRequests.map((r) =>
									h(
										"div",
										{ style: styles.place, key: r.id },
										h(
											"span",
											null,
											h("span", { style: styles.placeName }, r.name + " "),
											h("span", { style: styles.placeMeta }, "· " + r.status + (r.reason ? " · " + r.reason : "")),
										),
									),
								),
						h("h2", { style: styles.cardTitle }, "Локальный бэкенд (experimental)"),
						h(
							"div",
							{ style: styles.card },
							localEnabled === false &&
								h("p", { style: styles.note }, "На сервере не включён --experimental-local-bridge. Fallback: запусти dsh локально (`dsh web --port 3080 --no-open`) — UI откроется тоже локально."),
							localEnabled !== false && [
								h("p", { style: styles.note },
									localConnected
										? "Локальный коннектор подключён: твой dsh работает у тебя, UI — здесь."
										: "Коннектор не подключён. Получи команду и запусти её на своём компьютере."),
								!localConnected &&
									h("button", { style: { ...styles.button, ...styles.mini }, onClick: issueLocalGrant, disabled: busy }, "Получить команду подключения"),
								localConnected &&
									h("button", { style: { ...styles.button, ...styles.mini }, onClick: revokeLocalBackend, disabled: busy }, "Отключить"),
								localGrant &&
									h("pre", { style: { ...styles.note, background: "var(--dsw-specific-menu, #222)", padding: 8, borderRadius: 8, overflowX: "auto" } },
										"agctl local connect --server " + base() + " --grant " + localGrant.grant + " --launch-dsh"),
								localGrant &&
									h("p", { style: styles.note }, "Команда одноразовая и действует " + (localGrant.expiresInSeconds || 120) + " с; запусти её в терминале своего компьютера. Токен больше нигде не показывается."),
							],
						),
						h("h2", { style: styles.cardTitle }, "Рабочие места"),
						places.length === 0 && h("p", { style: styles.note }, "Пока пусто — создай первое место."),
						places.map((ws) =>
							h(
								"div",
								{ style: styles.place, key: ws.id },
								h(
									"span",
									null,
									h("span", { style: styles.placeName }, ws.name + " "),
									h(
										"span",
										{ style: styles.placeMeta },
										"· " + (ws.status || "?") + (ws.kit ? " · " + ws.kit : "") +
											(ws.sshEnabled && ws.sshPort ? " · SSH :" + ws.sshPort : ""),
									),
								),
								h(
									"span",
									{ style: styles.row },
									ws.url &&
										h(
											"a",
											{ href: ws.url, target: "_blank", style: { ...styles.button, textDecoration: "none" } },
											"Открыть",
										),
									ws.status === "running" &&
										h(
											"button",
											{ style: { ...styles.button, ...styles.mini }, onClick: act(ws, "stop"), disabled: busy },
											"Стоп",
										),
									ws.status !== "running" &&
										h(
											"button",
											{ style: { ...styles.button, ...styles.mini }, onClick: act(ws, "start"), disabled: busy },
											"Старт",
										),
								),
								ws.sshEnabled && ws.sshPort && h("div", { style: { ...styles.note, width: "100%" } },
									"SSH (на сервере): agctl connect --host 127.0.0.1 --port " + ws.sshPort +
									"  ·  IDE: VS Code → Remote-SSH к этому хосту/порту"),
							),
						),
				phase === "connecting" && h("p", { style: styles.note }, "Подключаюсь к agctl…"),
				h(
					"p",
					{ style: styles.note },
					"Кабинет agctl: управление местами и SSH-ключом. Удаление мест и настройки провайдера доступны администратору agctl.",
				),
			);
		}

		const inject = ["slots", "layout"];

		/**
		 * Contribute the «Кабинет» entry to the sidebar and the page it opens.
		 * @param ctx - the browser plugin context.
		 */
		function apply(ctx) {
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
