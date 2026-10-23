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

		const parseList = (text) => [...new Set(String(text || "").split(String.fromCharCode(10)).flatMap((line) => line.split(/[;,]/)).map((v) => v.trim()).filter(Boolean))];

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
			const [llmBaseURLsText, setLLMBaseURLsText] = React.useState("");
			const [llmModelsText, setLLMModelsText] = React.useState("");
			const [llmAPIKey, setLLMAPIKey] = React.useState("");
			const [sshPubKey, setSSHPubKey] = React.useState("");
			const [editingID, setEditingID] = React.useState("");
			const [editBaseURLsText, setEditBaseURLsText] = React.useState("");
			const [editModelsText, setEditModelsText] = React.useState("");
			const [editAPIKey, setEditAPIKey] = React.useState("");
			const [editSSHPubKey, setEditSSHPubKey] = React.useState("");
			const [disableSSH, setDisableSSH] = React.useState(false);
			const [logsID, setLogsID] = React.useState("");
			const [logsText, setLogsText] = React.useState("");
			const [logsBusy, setLogsBusy] = React.useState(false);
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
				const baseURLs = parseList(llmBaseURLsText);
				const models = parseList(llmModelsText);
				if (!baseURLs.length || !models.length) {
					setMsg("Укажи хотя бы один Base URL и одну модель (по одному значению на строку; токен можно оставить пустым, если он уже сохранён)");
					return;
				}
				const payload = {
					name: clean,
					kit,
					baseURL: baseURLs[0],
					baseURLs,
					models,
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

			const loadLogs = async (ws) => {
				setLogsID(ws.id);
				setLogsBusy(true);
				setLogsText("Загружаю последние строки…");
				try {
					const res = await api("/api/workspaces/" + ws.id + "/logs?tail=500");
					setLogsText(res.ok && res.data ? (res.data.logs || "Логи пусты.") : "Не удалось загрузить логи (HTTP " + res.status + ").");
				} catch (err) {
					setLogsText("Ошибка загрузки логов: " + String((err && err.message) || err));
				} finally {
					setLogsBusy(false);
				}
			};

			const openEdit = (ws) => {
				setLogsID("");
				setEditingID(ws.id);
				const urls = Array.isArray(ws.baseURLs) && ws.baseURLs.length ? ws.baseURLs : (ws.baseURL ? [ws.baseURL] : []);
				const models = Array.isArray(ws.models) && ws.models.length ? ws.models : (ws.model ? [ws.model] : []);
				setEditBaseURLsText(urls.join("\n"));
				setEditModelsText(models.join("\n"));
				setEditAPIKey("");
				setEditSSHPubKey("");
				setDisableSSH(false);
			};

			const waitWorkspaceSettled = async (id) => {
				for (let i = 0; i < 120; i++) {
					const res = await api("/api/workspaces/" + id);
					const current = res.ok && res.data ? res.data.workspace : null;
					if (!current || !["provisioning", "stopping"].includes(current.status)) return current;
					await new Promise((resolve) => setTimeout(resolve, 500));
				}
				return null;
			};

			const saveWorkspaceEdit = (ws) => async (e) => {
				e.preventDefault();
				const baseURLs = parseList(editBaseURLsText);
				const models = parseList(editModelsText);
				if (!baseURLs.length || !models.length) {
					setMsg("Нужны хотя бы один Base URL и одна модель.");
					return;
				}
				setBusy(true);
				setMsg("Сохраняю настройки «" + ws.name + "»…");
				try {
					const res = await api("/api/workspaces/" + ws.id + "/provider", {
						method: "PATCH",
						body: JSON.stringify({ baseURL: baseURLs[0], baseURLs, models, apiKey: editAPIKey.trim() }),
					});
					if (!res.ok) {
						setMsg("Провайдер не сохранён (HTTP " + res.status + "): " + ((res.data && res.data.error) || ""));
						return;
					}
					let current = res.data && res.data.workspace;
					if (current && current.status === "provisioning") {
						current = await waitWorkspaceSettled(ws.id);
						if (!current) {
							setMsg("Контейнер всё ещё применяет провайдера; обнови список через несколько секунд.");
							await load();
							return;
						}
					}
					if (current && current.status === "failed") {
						setMsg("Провайдер сохранён, но пересоздание контейнера завершилось ошибкой: " + (current.error || "см. логи"));
						await load();
						return;
					}
					if (editSSHPubKey.trim() || disableSSH) {
						const sshRes = await api("/api/workspaces/" + ws.id + "/ssh", {
							method: "PATCH",
							body: JSON.stringify({ pubKey: disableSSH ? "" : editSSHPubKey.trim() }),
						});
						if (!sshRes.ok) {
							setMsg("Провайдер сохранён, но SSH не изменён (HTTP " + sshRes.status + "): " + ((sshRes.data && sshRes.data.error) || ""));
							await load();
						return;
						}
						if (sshRes.data && sshRes.data.status === "provisioning") {
							const settled = await waitWorkspaceSettled(ws.id);
							if (!settled) {
								setMsg("SSH обновляется; контейнер ещё перезапускается, обнови список через несколько секунд.");
								await load();
								return;
							}
						}
					}
					setMsg("Настройки «" + ws.name + "» обновлены.");
					setEditingID("");
					await load();
				} catch (err) {
					setMsg("Не удалось сохранить настройки: " + String((err && err.message) || err));
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
					{ style: styles.wrap, "data-agctl-cabinet-root": "1" },
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
				{ style: styles.wrap, "data-agctl-cabinet-root": "1" },
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
							{ style: styles.formStack },
							h(
								"label",
								{ style: styles.field },
								h("span", { style: styles.fieldLabel }, "Имя рабочего места"),
								h("input", { style: { ...styles.input, width: "100%" }, placeholder: "например, demo-1c", value: name, onChange: (e) => setName(e.target.value) }),
							),
							h(
								"label",
								{ style: styles.field },
								h("span", { style: styles.fieldLabel }, "Комплект"),
								h("select", { style: { ...styles.input, width: "100%" }, value: kit, onChange: (e) => setKit(e.target.value) }, kits.map((k) => h("option", { value: k, key: k }, k))),
							),
							h(
								"label",
								{ style: styles.field },
								h("span", { style: styles.fieldLabel }, "Base URL провайдера (несколько — по одному на строку)"),
								h("textarea", { style: { ...styles.input, width: "100%", minHeight: 72, resize: "vertical" }, placeholder: "https://litellm.example/v1, https://backup.example/v1", value: llmBaseURLsText, onChange: (e) => setLLMBaseURLsText(e.target.value) }),
							),
							h(
								"label",
								{ style: styles.field },
								h("span", { style: styles.fieldLabel }, "Модели (общие для каждого Base URL; по одной на строку)"),
								h("textarea", { style: { ...styles.input, width: "100%", minHeight: 72, resize: "vertical" }, placeholder: "qwen-3.8-fast, qwen-3.8-deep", value: llmModelsText, onChange: (e) => setLLMModelsText(e.target.value) }),
							),
							h(
								"label",
								{ style: styles.field },
								h("span", { style: styles.fieldLabel }, "API-токен"),
								h("input", { style: { ...styles.input, width: "100%" }, type: "password", placeholder: "пусто = использовать сохранённый токен", value: llmAPIKey, autoComplete: "new-password", onChange: (e) => setLLMAPIKey(e.target.value) }),
							),
							h(
								"label",
								{ style: styles.field },
								h("span", { style: styles.fieldLabel }, "SSH public key (необязательно)"),
								h("textarea", { style: { ...styles.input, width: "100%", minHeight: 72, resize: "vertical" }, placeholder: "содержимое ~/.ssh/id_ed25519.pub", value: sshPubKey, onChange: (e) => setSSHPubKey(e.target.value) }),
							),
							h("p", { style: styles.note }, "SSH доступен только по ключу и привязан к 127.0.0.1 сервера; для удалённой машины сначала нужен SSH-туннель. Токен можно не вводить, если он уже сохранён."),
							h("button", { style: { ...styles.button, alignSelf: "flex-start" }, type: "submit", disabled: busy }, cfg.requireApproval ? "Отправить заявку" : "Создать рабочее место"),
						),
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
								"section",
								{ key: ws.id, style: { marginBottom: 12 } },
								h(
									"div",
									{ style: styles.place },
									h(
										"span",
										null,
										h("span", { style: styles.placeName }, ws.name + " "),
										h("span", { style: styles.placeMeta }, "· " + (ws.status || "?") + (ws.kit ? " · " + ws.kit : "") + (ws.sshEnabled && ws.sshPort ? " · SSH :" + ws.sshPort : "")),
									),
									h(
										"span",
										{ style: styles.row },
										ws.url && h("a", { href: ws.url, target: "_blank", rel: "noreferrer", style: { ...styles.button, textDecoration: "none" } }, "Открыть"),
										h("button", { style: { ...styles.button, ...styles.mini }, onClick: () => logsID === ws.id ? setLogsID("") : loadLogs(ws), disabled: logsBusy }, logsID === ws.id ? "Скрыть логи" : "Логи"),
										h("button", { style: { ...styles.button, ...styles.mini }, onClick: () => editingID === ws.id ? setEditingID("") : openEdit(ws), disabled: busy }, editingID === ws.id ? "Закрыть" : "Настройки"),
										ws.status === "running" && h("button", { style: { ...styles.button, ...styles.mini }, onClick: act(ws, "stop"), disabled: busy }, "Стоп"),
										ws.status !== "running" && h("button", { style: { ...styles.button, ...styles.mini }, onClick: act(ws, "start"), disabled: busy || ws.status === "provisioning" }, "Старт"),
									),
								),
								ws.sshEnabled && ws.sshPort && h("p", { style: styles.note }, "SSH: agctl connect --host 127.0.0.1 --port " + ws.sshPort + " · VS Code Remote-SSH"),
								logsID === ws.id && h("div", { style: styles.logPanel },
									h("div", { style: { ...styles.row, justifyContent: "space-between" } },
										h("h3", { style: styles.cardTitle }, "Логи контейнера · " + ws.name),
										h("button", { style: { ...styles.button, ...styles.mini }, onClick: () => loadLogs(ws), disabled: logsBusy }, logsBusy ? "Загружаю…" : "Обновить"),
									),
									h("pre", { style: styles.logs }, logsText),
								),
								editingID === ws.id && h("form", { style: styles.logPanel, onSubmit: saveWorkspaceEdit(ws) },
									h("h3", { style: styles.cardTitle }, "Настройки · " + ws.name),
									h("label", { style: styles.field },
										h("span", { style: styles.fieldLabel }, "Base URL (по одному на строку)"),
										h("textarea", { style: { ...styles.input, width: "100%", minHeight: 64, resize: "vertical" }, value: editBaseURLsText, onChange: (e) => setEditBaseURLsText(e.target.value) }),
									),
									h("label", { style: styles.field },
										h("span", { style: styles.fieldLabel }, "Модели (одинаковый список для каждого Base URL)"),
										h("textarea", { style: { ...styles.input, width: "100%", minHeight: 64, resize: "vertical" }, value: editModelsText, onChange: (e) => setEditModelsText(e.target.value) }),
									),
									h("label", { style: styles.field },
										h("span", { style: styles.fieldLabel }, "Новый API-токен (пусто = использовать сохранённый)"),
										h("input", { style: { ...styles.input, width: "100%" }, type: "password", autoComplete: "new-password", value: editAPIKey, onChange: (e) => setEditAPIKey(e.target.value) }),
									),
									h("label", { style: styles.field },
										h("span", { style: styles.fieldLabel }, "Новый SSH public key (пусто = не менять текущий)"),
										h("textarea", { style: { ...styles.input, width: "100%", minHeight: 64, resize: "vertical" }, value: editSSHPubKey, onChange: (e) => setEditSSHPubKey(e.target.value) }),
									),
									h("label", { style: { display: "flex", alignItems: "center", gap: 8, marginTop: 10, fontSize: 13 } },
										h("input", { type: "checkbox", checked: disableSSH, onChange: (e) => setDisableSSH(e.target.checked) }),
										"Отключить SSH",
									),
									h("div", { style: { ...styles.row, marginTop: 12 } },
										h("button", { style: { ...styles.button, ...styles.mini }, type: "submit", disabled: busy }, busy ? "Сохраняю…" : "Сохранить параметры"),
										h("button", { style: { ...styles.button, ...styles.mini }, type: "button", onClick: () => setEditingID("") }, "Отмена"),
									),
									h("p", { style: styles.note }, "Состав комплекта и имя репозитория — неизменяемые после создания; провайдер можно менять безопасно с пересозданием контейнера."),
								),
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
		// ── единая навигация режимов: чат (синий) ↔ рабочие места (оранжевые) ──
		// ВАЖНО: секция строится сырым DOM (createElement/appendChild), а не
		// React-h() — слот живёт вне React-дерева dsh, h() дал бы [object Object].
		const CHAT_ACCENT = "#1676df";
		const DEV_ACCENT = "#e8833a";
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

		function statusDot(status) {
			const color = status === "running" ? "#2fa653" : status === "provisioning" ? "#e8b13a" : "#9aa1ab";
			const dot = el("span", "width:7px;height:7px;border-radius:999px;background:" + color + ";flex:none");
			dot.title = status || "";
			return dot;
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

		async function refreshNav(root) {
			let places = [];
			try {
				const res = await fetch("/api/workspaces", { credentials: "include", headers: { Accept: "application/json" } });
				if (res.ok) {
					const data = await res.json();
					if (Array.isArray(data && data.workspaces)) places = data.workspaces;
				}
			} catch (e) {}
			const nav = el("div", "display:flex;flex-direction:column;gap:2px;padding:6px 4px 10px;min-width:0");
			nav.setAttribute("data-agctl-nav", "1");
			const heading = el("div", "font-size:11px;font-weight:650;letter-spacing:.4px;color:var(--dsw-alias-label-tertiary,#888);padding:0 10px 2px", "РЕЖИМЫ");
			heading.dataset.agctlNavHeading = "1";
			nav.appendChild(heading);
			nav.appendChild(navRow("/chat/", "Личный чат", CHAT_ACCENT));
			for (const ws of places) {
				nav.appendChild(navRow(ws.url || "/w/" + ws.id + "/", ws.name, DEV_ACCENT, statusDot(ws.status)));
			}
			root.replaceChildren(nav);
			syncModeNavWidth(root);
		}

		function syncModeNavWidth(root) {
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

		function mountModeNav() {
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
				window.__agctlNavWidthObserver = new MutationObserver(() => syncModeNavWidth(slot));
				window.__agctlNavWidthObserver.observe(sidebar, { attributes: true, attributeFilter: ["class"] });
			}
			if (!window.__agctlNavTimer) window.__agctlNavTimer = setInterval(() => refreshNav(slot), 15000);
		}

		function apply(ctx) {
			ensureCabinetStyles();
			const startNav = () => {
				mountModeNav();
				if (!window.__agctlNavObserver) {
					window.__agctlNavObserver = new MutationObserver(() => mountModeNav());
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
