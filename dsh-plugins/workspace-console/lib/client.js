// agctl workspace-console — вкладка «Воркспейсы: возможности».
//
// Полноценная панель в штатном списке панелей dsh (паттерн Plugins/Automation
// tasks: слот sidebar.panellist + страница в слоте main). Содержимое — единое
// место настройки воркспейсов для всех ролей:
//   • Возможности — платформенный каталог пакетов (навык + шаблоны/скрипты
//     одним кликом; каталог задаёт профиль организации — catalog/<id>);
//   • Системный промт — AGENTS.md воркспейса;
//   • Компоненты — скиллы/инструменты/данные/промпты с галочками включения
//     (выключенные уходят в серверный stash, невидимый агенту и autosave);
//   • Удалить — стирает воркспейс и его каталог на диске (agctl-ручка +
//     снятие записи реестра dsh).
// Тот же каталог пакетов показывается при создании воркспейса (chat-brand) —
// один и тот же серверный механизм, одно место правды.
window.__ModuleLoader__.load({
	id: "@arijjia/agctl-workspace-console",
	factory: (require) => {
		var exports = {};
		const React = require("react");

		const PANEL_ID = "agctl-workspaces";

		// Штатный сервис воркспейсов dsh (приходит в apply): удаление снимает
		// запись реестра; каталог стирает agctl (см. deleteProject).
		let dshWorkspaces = null;

		function ensureStyles() {
			if (document.querySelector("style[data-agctl-wsc-styles]")) return;
			const style = document.createElement("style");
			style.dataset.agctlWscStyles = "1";
			style.textContent = `
				[data-agctl-wsc-page] { height: 100%; box-sizing: border-box; overflow: auto; padding: 26px 30px 40px; }
				[data-agctl-wsc-page] input[type=checkbox] { width:16px;height:16px;accent-color:#1769d2; }
				[data-agctl-wsc-page] .wsc-card {
					border:1px solid var(--dsw-alias-border-l1,#333);border-radius:12px;padding:12px 14px;margin:0 0 10px;
					background:var(--dsw-alias-bg-layer-2, transparent);
				}
				[data-agctl-wsc-page] .wsc-row { display:flex;align-items:flex-start;gap:9px;padding:6px 2px; }
				[data-agctl-wsc-page] .wsc-row:hover { background: var(--dsw-alias-interactive-bg-hover, rgba(255,255,255,.06)); border-radius:8px; }
				[data-agctl-wsc-page] .wsc-btn {
					padding:7px 13px;border:1px solid #1459b8;border-radius:8px;background:#1769d2;
					color:#fff;cursor:pointer;font:inherit;font-size:13px;font-weight:600;
				}
				[data-agctl-wsc-page] .wsc-btn.secondary { background:transparent;color:inherit;border-color:var(--dsw-alias-border-l1,#555);font-weight:400; }
				[data-agctl-wsc-page] .wsc-btn.danger { color:#e06666; }
				[data-agctl-wsc-page] .wsc-btn:disabled { opacity:.6; cursor:not-allowed; }
				[data-agctl-wsc-page] .wsc-prompt {
					width:100%;min-height:120px;box-sizing:border-box;resize:vertical;
					border:1px solid var(--dsw-alias-border-l1,#555);border-radius:8px;padding:8px;
					background:transparent;color:inherit;font:inherit;font-size:12.5px;
				}
				[data-agctl-wsc-page] .wsc-badge {
					font-size:10px;padding:1px 7px;border-radius:999px;border:1px solid var(--dsw-alias-border-l1,#555);color:var(--dsw-alias-label-tertiary,#888);
				}
				[data-agctl-wsc-page] .wsc-wslist { display:flex; flex-direction:column; gap:8px; }
				[data-agctl-wsc-page] .wsc-wsitem {
					text-align:left;border:1px solid var(--dsw-alias-border-l1,#333);border-radius:10px;padding:10px 12px;
					background:transparent;color:inherit;cursor:pointer;font:inherit;
				}
				[data-agctl-wsc-page] .wsc-wsitem:hover { border-color: var(--dsw-alias-border-l2,#555); }
				[data-agctl-wsc-page] .wsc-wsitem[data-active="1"] { border-color:#1769d2; }
			`;
			document.head.appendChild(style);
		}

		function el(tag, css, text) {
			const node = document.createElement(tag);
			if (css) {
				// Мини-нотация: часть до «:» без «;» — классы, декларации со «:» —
				// style.cssText. Пример: "wsc-btn secondary;margin-left:auto".
				let className = "";
				let style = "";
				for (const part of css.split(";")) {
					const piece = part.trim();
					if (!piece) continue;
					if (piece.includes(":")) style += piece + ";";
					else className += (className ? " " : "") + piece;
				}
				if (className) node.className = className;
				if (style) node.style.cssText = style;
			}
			if (text !== undefined) node.textContent = text;
			return node;
		}

		function apiFactory() {
			return async (path, method = "GET", body) => {
				const res = await fetch(path, {
					credentials: "include", method,
					headers: { Accept: "application/json", ...(body !== undefined ? { "Content-Type": "application/json" } : {}) },
					body: body === undefined ? undefined : JSON.stringify(body),
				});
				let data;
				try { data = await res.json(); } catch { data = null; }
				if (!res.ok) throw new Error("HTTP " + res.status + ": " + ((data && data.error) || "Запрос не выполнен"));
				return data;
			};
		}

		// ── чистые функции (test seams) ────────────────────────────────────────

		// groupComponents — разбивка каталога по секциям страницы.
		function groupComponents(components) {
			const groups = { skills: [], tools: [], data: [], docs: [] };
			for (const comp of components || []) {
				if (comp.kind === "skill" || comp.kind === "dshskill") groups.skills.push(comp);
				else if (comp.kind === "tool") groups.tools.push(comp);
				else if (comp.kind === "data") groups.data.push(comp);
				else groups.docs.push(comp);
			}
			return groups;
		}

		// buildDisabledIDs — выбор по чекбоксам → список выключенных ID.
		function buildDisabledIDs(components, enabledByID) {
			const disabled = [];
			for (const comp of components || []) {
				if (comp.locked) continue;
				if (enabledByID.has(comp.id) === comp.enabled) continue; // без изменений
				if (!enabledByID.has(comp.id)) disabled.push(comp.id);
			}
			return disabled;
		}

		// deleteProject — удалить воркспейс с диска и из списка. Порядок
		// детерминированный: сначала agctl-каталог (DELETE /api/projects/<имя>),
		// затем запись dsh-реестра, если она есть. Для «сироты» (каталог без
		// записи — остаток старого нативного удаления) достаточно первого шага.
		async function deleteProject(api, project) {
			await api("/api/projects/" + encodeURIComponent(project.name), "DELETE");
			let record = null;
			try {
				const snap = dshWorkspaces && dshWorkspaces.list && dshWorkspaces.list.getSnapshot
					? dshWorkspaces.list.getSnapshot() : null;
				const items = snap && Array.isArray(snap.items) ? snap.items : [];
				record = items.find((w) => w && w.path === project.directory) || null;
			} catch (e) { record = null; }
			if (record && dshWorkspaces && typeof dshWorkspaces.delete === "function") {
				await dshWorkspaces.delete(record.workspaceId);
			}
		}

		// ── страница панели ────────────────────────────────────────────────────

		const GROUP_TITLES = {
			skills: "Навыки",
			tools: "Инструменты",
			data: "Данные и демо-проекты",
			docs: "Промпты и инструкции",
		};
		const GROUP_HINTS = {
			skills: "Процедуры работы агента (skills/ и .dsh/skills — автозагрузка).",
			tools: "CLI-инструменты контура (scripts/*.py).",
			data: "Фикстуры и демо-конфигурации для локальных проверок.",
			docs: "Системные инструкции воркспейса — выключать нельзя.",
		};

		function componentRow(comp, enabledByID, dirty) {
			const row = el("div", "wsc-row");
			const box = document.createElement("input");
			box.type = "checkbox";
			box.checked = enabledByID.has(comp.id);
			box.disabled = !!comp.locked;
			box.dataset.agctlWscComponent = comp.id;
			box.addEventListener("change", () => {
				if (box.checked) enabledByID.add(comp.id);
				else enabledByID.delete(comp.id);
				dirty();
			});
			const text = el("div", "min-width:0;flex:1");
			const title = el("div", "font-size:13px;font-weight:600;display:flex;gap:8px;align-items:baseline");
			title.appendChild(el("span", null, comp.title || comp.name));
			if (!comp.enabled) title.appendChild(el("span", "wsc-badge", "выключен"));
			if (comp.kind === "dshskill") title.appendChild(el("span", "wsc-badge", "автозагрузка"));
			if (comp.locked) title.appendChild(el("span", "wsc-badge", "базовый"));
			const meta = [comp.path, comp.description].filter(Boolean).join(" — ");
			text.append(title, el("div", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888);overflow-wrap:anywhere", meta));
			row.append(box, text);
			return row;
		}

		function catalogRow(api, pkg, installed, refresh) {
			const row = el("div", "wsc-row");
			const text = el("div", "min-width:0;flex:1");
			const title = el("div", "font-size:13px;font-weight:600;display:flex;gap:8px;align-items:baseline");
			title.appendChild(el("span", null, pkg.title || pkg.id));
			if (installed) title.appendChild(el("span", "wsc-badge", "установлен"));
			text.append(title, el("div", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888);overflow-wrap:anywhere", pkg.description || ""));
			const btn = el("button", "wsc-btn secondary", installed ? "Убрать" : "Установить");
			btn.addEventListener("click", async () => {
				btn.disabled = true;
				try {
					if (installed) {
						await api("/api/projects/" + encodeURIComponent(pkg._project) + "/catalog/" + pkg.id, "DELETE");
					} else {
						await api("/api/projects/" + encodeURIComponent(pkg._project) + "/catalog/" + pkg.id + "/install", "POST");
					}
					refresh();
				} catch (err) {
					btn.textContent = String(err.message || err).slice(0, 60);
					btn.disabled = false;
				}
			});
			row.append(text, btn);
			return row;
		}

		// WorkspacePanel — содержимое вкладки на всю область чата.
		function WorkspacePanel() {
			const hostRef = React.useRef(null);
			React.useEffect(() => {
				const host = hostRef.current;
				if (!host) return undefined;

				const api = apiFactory();
				let alive = true;
				const state = { projects: [], sel: "" };
				host.replaceChildren();

				const page = el("div", null, null);
				page.dataset.agctlWscPage = "1";
				host.appendChild(page);
				const inner = el("div", "max-width:1060px;margin:0 auto", null);
				page.appendChild(inner);

				inner.appendChild(el("h2", "margin:0 0 2px;font-size:18px;font-weight:650", "Воркспейсы: возможности"));
				inner.appendChild(el("p", "margin:0 0 16px;font-size:12px;color:var(--dsw-alias-label-tertiary,#888)",
					"Единое место настройки воркспейсов: возможности из каталога платформы, системный промт и компоненты. «Удалить» стирает воркспейс вместе с каталогом на диске."));

				const grid = el("div", "display:grid;grid-template-columns:300px minmax(0,1fr);gap:20px;align-items:start", null);
				inner.appendChild(grid);
				const left = el("div", null, null);
				const right = el("div", null, null);
				grid.append(left, right);

				const statusLine = el("p", "margin:10px 0 0;font-size:12px;color:var(--dsw-alias-label-tertiary,#888);min-height:16px", "");
				right.appendChild(statusLine);
				const setStatus = (t) => { statusLine.textContent = t || ""; };
				const guard = (p) => Promise.resolve(p).catch((err) => { if (alive) setStatus(String(err.message || err)); });

				// ── левая колонка: список воркспейсов ─────────────────────────────
				const listTitle = el("div", "font-size:12px;font-weight:700;margin:0 0 8px", "Воркспейсы");
				const listWrap = el("div", "wsc-wslist", null);
				const refreshBtn = el("button", "wsc-btn secondary", "Обновить список");
				refreshBtn.style.marginTop = "10px";
				left.append(listTitle, listWrap, refreshBtn);

				const renderList = () => {
					listWrap.replaceChildren();
					if (!state.projects.length) {
						listWrap.appendChild(el("p", "font-size:12.5px;color:var(--dsw-alias-label-tertiary,#888);margin:0",
							"Воркспейсов пока нет. Создайте первый кнопкой «+» в списке Workspaces — там же можно сразу выбрать возможности."));
						return;
					}
					for (const project of state.projects) {
						const item = el("button", "wsc-wsitem", null);
						item.dataset.active = project.name === state.sel ? "1" : "0";
						item.appendChild(el("div", "font-size:13.5px;font-weight:650", project.name));
						item.appendChild(el("div", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888);overflow-wrap:anywhere", project.directory || ""));
						item.addEventListener("click", () => {
							if (state.sel === project.name) return;
							state.sel = project.name;
							renderList();
							guard(loadDetails());
						});
						listWrap.appendChild(item);
					}
				};

				const loadDetails = async () => {
					if (!state.sel) { renderDetails(null, null, "", null); return; }
					setStatus("Читаем настройки воркспейса…");
					const encName = encodeURIComponent(state.sel);
					const [catData, compData, promptData, mcpData] = await Promise.all([
						api("/api/projects/" + encName + "/catalog"),
						api("/api/projects/" + encName + "/components"),
						api("/api/projects/" + encName + "/prompt").catch(() => ({ prompt: "" })),
						api("/api/projects/" + encName + "/mcp").catch(() => null),
					]);
					renderDetails(catData, compData, (promptData && promptData.prompt) || "", mcpData);
					setStatus("");
				};

				const loadProjects = async () => {
					listWrap.replaceChildren(el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888);margin:0", "Читаем воркспейсы…"));
					right.replaceChildren(statusLine);
					const data = await api("/api/projects");
					state.projects = Array.isArray(data.projects) ? data.projects : [];
					if (!state.projects.find((p) => p.name === state.sel)) state.sel = state.projects[0] ? state.projects[0].name : "";
					renderList();
					await loadDetails();
				};
				refreshBtn.addEventListener("click", () => guard(loadProjects()));

				// ── правая колонка: детали выбранного воркспейса ──────────────────
				const renderDetails = (catData, compData, promptText, mcpData) => {
					right.replaceChildren(statusLine);
					if (!state.sel) {
						right.appendChild(el("p", "font-size:12.5px;color:var(--dsw-alias-label-tertiary,#888)",
							"Выберите воркспейс слева — или создайте его кнопкой «+» в списке Workspaces."));
						return;
					}
					const encName = encodeURIComponent(state.sel);

					// шапка выбранного воркспейса
					const head = el("div", "display:flex;align-items:center;gap:10px;margin:0 0 14px;flex-wrap:wrap", null);
					head.appendChild(el("strong", "font-size:15px", state.sel));
					head.appendChild(el("span", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888)", "/workspace/" + state.sel));
					const del = el("button", "wsc-btn secondary danger;margin-left:auto", "Удалить воркспейс");
					del.title = "Удалить воркспейс и его каталог на диске (безвозвратно)";
					del.addEventListener("click", async () => {
						const project = state.projects.find((p) => p.name === state.sel);
						if (!project) return;
						const question = "Удалить воркспейс «" + project.name + "»?\n\n"
							+ "Каталог проекта на диске (" + project.directory + ") будет удалён вместе со всеми файлами — безвозвратно. "
							+ "История сессий в чате останется.";
						if (!window.confirm(question)) return;
						del.disabled = true;
						setStatus("Удаляем воркспейс и каталог…");
						try {
							await deleteProject(api, project);
							state.sel = "";
							await loadProjects();
							setStatus("Удалено.");
						} catch (err) {
							setStatus(String(err.message || err));
							del.disabled = false;
						}
					});
					head.appendChild(del);
					right.appendChild(head);

					// ── Возможности ────────────────────────────────────────────────
					right.appendChild(el("h3", "margin:0 0 2px;font-size:12px;font-weight:700", "Возможности"));
					right.appendChild(el("p", "margin:0 0 6px;font-size:11px;color:var(--dsw-alias-label-tertiary,#888)",
						"Пакеты платформы: навык + шаблоны/скрипты одним кликом. Ставятся в .dsh/skills и видны агенту в этом воркспейсе."));
					const catCard = el("div", "wsc-card", null);
					const packages = Array.isArray(catData.packages) ? catData.packages : [];
					for (const pkg of packages) {
						pkg._project = state.sel;
						catCard.appendChild(catalogRow(api, pkg, !!pkg.installed, () => guard(loadDetails())));
					}
					if (!packages.length) catCard.appendChild(el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888);margin:4px 0", "Каталог платформы пуст."));
					right.appendChild(catCard);

					// ── Системный промт ────────────────────────────────────────────
					right.appendChild(el("h3", "margin:14px 0 2px;font-size:12px;font-weight:700", "Системный промт воркспейса (AGENTS.md)"));
					right.appendChild(el("p", "margin:0 0 6px;font-size:11px;color:var(--dsw-alias-label-tertiary,#888)",
						"Один промт на воркспейс: агент читает его в каждой сессии этого воркспейса. Пустой промт удаляет файл."));
					const promptCard = el("div", "wsc-card", null);
					const ta = el("textarea", "wsc-prompt");
					ta.value = promptText || "";
					ta.placeholder = "Например: отчёты оформлять по ГОСТ, код — с тестами…";
					const promptSave = el("button", "wsc-btn secondary;margin-top:8px", "Сохранить промт");
					promptSave.disabled = true;
					ta.addEventListener("input", () => { promptSave.disabled = false; });
					promptSave.addEventListener("click", async () => {
						promptSave.disabled = true;
						setStatus("Сохраняем промт…");
						try {
							await api("/api/projects/" + encName + "/prompt", "PUT", { prompt: ta.value });
							setStatus("Промт сохранён.");
						} catch (err) {
							setStatus(String(err.message || err));
							promptSave.disabled = false;
						}
					});
					promptCard.append(ta, promptSave);
					right.appendChild(promptCard);

					// ── MCP-серверы (карточка workspace-mcp-config) ────────────────
					right.appendChild(el("h3", "margin:14px 0 2px;font-size:12px;font-weight:700", "MCP-серверы"));
					right.appendChild(el("p", "margin:0 0 6px;font-size:11px;color:var(--dsw-alias-label-tertiary,#888)",
						"Серверы платформенного каталога для этого воркспейса. Выбор собирает dsh-пресет — создайте сессию воркспейса с пресетом «" + (mcpData && mcpData.presetID || "agctl-ws-…") + "», и новая сессия получит ровно этот набор."));
					const mcpCard = el("div", "wsc-card", null);
					if (!mcpData) {
						mcpCard.appendChild(el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888);margin:4px 0",
							"Раздел MCP недоступен (нужен свежий agctl serve)."));
					} else {
						const mcpState = { selected: new Set(mcpData.selected || []) };
						const keyInput = el("input", null, null);
						keyInput.type = "password";
						keyInput.style.cssText = "width:100%;box-sizing:border-box;border:1px solid var(--dsw-alias-border-l1,#555);border-radius:8px;padding:7px 8px;background:transparent;color:inherit;font:inherit;font-size:12px";
						keyInput.placeholder = mcpData.tavilyKeySet ? "Ключ TAVILY_API_KEY сохранён — введите новый, чтобы заменить" : "Ключ TAVILY_API_KEY (tvly-…)";
						const mcpErr = el("p", "font-size:11px;color:#e06666;margin:6px 0 0;min-height:14px", "");
						const mcpRows = [];
						for (const server of mcpData.servers || []) {
							const row = el("label", "wsc-row;cursor:pointer");
							const box = el("input", null, null);
							box.type = "checkbox";
							box.checked = mcpState.selected.has(server.id);
							box.addEventListener("change", () => {
								if (box.checked) mcpState.selected.add(server.id);
								else mcpState.selected.delete(server.id);
								syncKeyVisibility();
							});
							const text = el("div", "min-width:0;flex:1");
							text.appendChild(el("div", "font-size:12.5px;font-weight:600", server.title || server.id));
							if (server.description) text.appendChild(el("div", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888)", server.description));
							row.append(box, text);
							mcpCard.appendChild(row);
							mcpRows.push({ server, box });
						}
						const keyWrap = el("div", "margin:8px 0 0", null);
						keyWrap.appendChild(keyInput);
						mcpCard.appendChild(keyWrap);
						const mcpSave = el("button", "wsc-btn secondary;margin-top:8px", "Сохранить MCP");
						const syncKeyVisibility = () => {
							const needKey = mcpRows.some(({ server, box }) => server.secretEnv && box.checked);
							keyWrap.style.display = needKey ? "block" : "none";
						};
						syncKeyVisibility();
						mcpSave.addEventListener("click", async () => {
							mcpSave.disabled = true;
							mcpErr.textContent = "";
							setStatus("Сохраняем MCP-набор…");
							try {
								const payload = { servers: mcpRows.filter(({ box }) => box.checked).map(({ server }) => server.id) };
								if (keyInput.value) payload.tavilyKey = keyInput.value;
								const fresh = await api("/api/projects/" + encName + "/mcp", "PUT", payload);
								keyInput.value = "";
								keyInput.placeholder = fresh.tavilyKeySet ? "Ключ TAVILY_API_KEY сохранён — введите новый, чтобы заменить" : keyInput.placeholder;
								setStatus("MCP-набор сохранён. Новые сессии с пресетом «" + (fresh.presetID || "agctl-ws-…") + "» получат этот набор.");
							} catch (err) {
								mcpErr.textContent = String(err.message || err);
								setStatus("");
							} finally {
								mcpSave.disabled = false;
							}
						});
						mcpCard.append(mcpSave, mcpErr);
					}
					right.appendChild(mcpCard);

					// ── Файлы (скачивание артефактов) ──────────────────────────────
					right.appendChild(el("h3", "margin:14px 0 2px;font-size:12px;font-weight:700", "Файлы"));
					right.appendChild(el("p", "margin:0 0 6px;font-size:11px;color:var(--dsw-alias-label-tertiary,#888)",
						"Артефакты агента (презентации, документы): скачать можно напрямую, без превью."));
					const filesCard = el("div", "wsc-card", null);
					const filesStatus = el("p", "margin:2px 0;font-size:11px;color:var(--dsw-alias-label-tertiary,#888);min-height:14px", "Читаем файлы…");
					const filesList = el("div", null, null);
					const filesRefresh = el("button", "wsc-btn secondary;margin-top:8px", "Обновить файлы");
					const fmtSize = (n) => !n ? "—" : n < 1024 ? n + " Б" : n < 1048576 ? (n / 1024).toFixed(1) + " КБ" : (n / 1048576).toFixed(1) + " МиБ";
					const renderFiles = (files) => {
						filesList.replaceChildren();
						const entries = (files || []).filter((f) => !f.isDir && f.size > 0);
						if (!entries.length) {
							filesList.appendChild(el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888);margin:4px 0",
								"Файлов пока нет. Попросите агента что-нибудь сделать — результаты появятся здесь."));
							return;
						}
						for (const f of entries) {
							const row = el("div", "wsc-row");
							const text = el("div", "min-width:0;flex:1");
							text.appendChild(el("div", "font-size:12.5px;font-weight:600;overflow-wrap:anywhere", f.path));
							text.appendChild(el("div", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888)", fmtSize(f.size)));
							const link = el("a", "wsc-btn secondary;flex:none;text-decoration:none", "Скачать");
							link.href = "/api/projects/" + encodeURIComponent(state.sel) + "/file?path=" + encodeURIComponent(f.path);
							link.setAttribute("download", f.path.split("/").pop() || "file");
							row.append(text, link);
							filesList.appendChild(row);
						}
					};
					const loadFiles = async () => {
						filesStatus.textContent = "Читаем файлы…";
						try {
							const data = await api("/api/projects/" + encodeURIComponent(state.sel) + "/files");
							renderFiles(Array.isArray(data.files) ? data.files : []);
							filesStatus.textContent = "";
						} catch (err) {
							filesStatus.textContent = String(err.message || err);
						}
					};
					filesRefresh.addEventListener("click", () => guard(loadFiles()));
					filesCard.append(filesStatus, filesList, filesRefresh);
					right.appendChild(filesCard);
					guard(loadFiles());

					// ── Компоненты ─────────────────────────────────────────────────
					right.appendChild(el("h3", "margin:14px 0 2px;font-size:12px;font-weight:700", "Компоненты"));
					const components = Array.isArray(compData.components) ? compData.components : [];
					const enabledByID = new Set(components.filter((c) => c.enabled).map((c) => c.id));
					const compSave = el("button", "wsc-btn;margin-top:8px", "Сохранить компоненты");
					compSave.disabled = true;
					const dirty = () => { compSave.disabled = false; setStatus("Есть изменения — нажмите «Сохранить»."); };
					const compCard = el("div", "wsc-card", null);
					const groups = groupComponents(components);
					for (const key of ["skills", "tools", "data", "docs"]) {
						if (!groups[key].length) continue;
						compCard.appendChild(el("div", "margin:10px 0 2px;font-size:12px;font-weight:700", GROUP_TITLES[key]));
						compCard.appendChild(el("p", "margin:0 0 4px;font-size:11px;color:var(--dsw-alias-label-tertiary,#888)", GROUP_HINTS[key]));
						for (const comp of groups[key]) compCard.appendChild(componentRow(comp, enabledByID, dirty));
					}
					if (!components.length) compCard.appendChild(el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888);margin:4px 0", "В воркспейсе нет скиллов и инструментов."));
					compSave.addEventListener("click", async () => {
						compSave.disabled = true;
						setStatus("Применяем…");
						try {
							const disabled = buildDisabledIDs(components, enabledByID);
							await api("/api/projects/" + encName + "/components", "PUT", { disabled });
							setStatus(disabled.length ? "Применено: выключено " + disabled.length + "." : "Применено: все компоненты включены.");
							await loadDetails();
						} catch (err) {
							setStatus(String(err.message || err));
							compSave.disabled = false;
						}
					});
					compCard.appendChild(compSave);
					right.appendChild(compCard);
				};

				guard(loadProjects());
				return () => { alive = false; };
			}, []);
			return React.createElement("div", {
				ref: hostRef,
				"data-agctl-wsc-panel-host": "1",
				style: { height: "100%" },
			});
		}

		// agctlDownloadUrl — полный путь /workspace/<ws>/<rel> → URL скачивания
		// через gateway (GET /api/projects/<ws>/file?path=<rel>). Не-агctl-пути
		// (не /workspace/, вложенные сегменты) → null.
		function agctlDownloadUrl(fullPath) {
			if (typeof fullPath !== "string" || !fullPath.startsWith("/workspace/")) return null;
			const parts = fullPath.slice("/workspace/".length).split("/").filter(Boolean);
			if (parts.length < 2) return null;
			const ws = parts[0];
			const rel = parts.slice(1).join("/");
			return "/api/projects/" + encodeURIComponent(ws) + "/file?path=" + encodeURIComponent(rel);
		}

		// downloadUrlFromCard — путь из title карточки deliverable (кладёт dsh:
		// абсолютный путь файла) → URL скачивания. Слот рендерится внутри
		// .nyYjTG_file[data-presented-file], title живёт на cardPreview-кнопке.
		function downloadUrlFromCard(node) {
			const card = node.closest && node.closest("[data-presented-file]");
			if (!card) return null;
			const src = card.querySelector("button[title^='/workspace/']");
			if (!src) return null;
			return agctlDownloadUrl(src.getAttribute("title"));
		}

		// DownloadAction — кнопка «Скачать» на карточке deliverable в чате:
		// забрать файл напрямую через gateway, без превью в сайдбаре.
		function DownloadAction() {
			const ref = React.useRef(null);
			React.useEffect(() => {
				const node = ref.current;
				if (!node) return undefined;
				const onClick = (ev) => {
					const url = downloadUrlFromCard(node);
					if (!url) { ev.preventDefault(); return; }
					// attachment-ответ gateway скачивает файл, не покидая чат
					window.location.href = url;
				};
				node.addEventListener("click", onClick);
				return () => node.removeEventListener("click", onClick);
			}, []);
			return React.createElement("button", {
				ref,
				type: "button",
				"data-agctl-download": "1",
				title: "Скачать файл через agctl",
				style: {
					padding: "3px 9px", border: "1px solid var(--dsw-alias-border-l1,#555)",
					borderRadius: "8px", background: "transparent", color: "inherit",
					cursor: "pointer", font: "inherit", fontSize: "11px",
					display: "inline-flex", alignItems: "center", gap: "4px",
				},
			}, "Скачать");
		}

		// Иконка строки панели (renderSlot передаёт {size, active}).
		function PanelIcon(props) {
			const size = (props && props.size) || 18;
			return React.createElement("svg", {
				width: size, height: size, viewBox: "0 0 24 24", fill: "none",
				stroke: "currentColor", strokeWidth: 1.8, strokeLinecap: "round", strokeLinejoin: "round",
				"aria-hidden": "true",
			},
				React.createElement("path", { d: "M4 6h9" }),
				React.createElement("circle", { cx: "18", cy: "6", r: "2.2" }),
				React.createElement("path", { d: "M4 12h16" }),
				React.createElement("path", { d: "M4 18h12" }),
			);
		}

		function apply(ctx) {
			ensureStyles();
			try { dshWorkspaces = ctx.get && ctx.get("workspaces"); } catch (e) { dshWorkspaces = null; }
			// Строка панели — после штатных Plugins/Automation tasks, перед More
			// (More — служебное меню-заглушка, всегда замыкает список панелей).
			ctx.slots.inject("sidebar.panellist", () => ctx.slots.register({
				name: "sidebar.panellist",
				id: PANEL_ID,
				order: 20,
				label: () => "Воркспейсы: возможности",
			}, PanelIcon));
			// Полноэкранная вкладка той же панели (ключ = id панели).
			ctx.slots.inject("main", () => ctx.slots.register({
				name: "main",
				key: PANEL_ID,
				inject: () => ({}),
			}, WorkspacePanel));
			// «Скачать» на каждой карточке deliverable в чате (файл — через
			// gateway, без превью в сайдбаре).
			ctx.slots.inject("deliverables.file.actions", () => ctx.slots.register({
				name: "deliverables.file.actions",
				id: "agctl-workspace-console-download",
				order: 5,
			}, DownloadAction));
		}

		exports.inject = ["slots"];
		exports.apply = apply;
		// Test seams.
		exports.groupComponents = groupComponents;
		exports.buildDisabledIDs = buildDisabledIDs;
		exports.deleteProject = deleteProject;
		exports.agctlDownloadUrl = agctlDownloadUrl;
		exports.PANEL_ID = PANEL_ID;
		return exports;
	},
});
