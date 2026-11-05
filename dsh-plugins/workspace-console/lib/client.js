// agctl workspace-console — консоль воркспейсов: единое место настройки
// возможностей воркспейса. Список проектов пользователя; для каждого —
// платформенный каталог пакетов («Возможности», например презентации по
// шаблону вуза), системный промт (AGENTS.md) и каталог компонентов
// (скиллы/инструменты/данные/промпты) с галочками включения. То же самое
// доступно при создании воркспейса (диалог chat-brand) — один и тот же
// серверный каталог, одно место правды.
//
// Состояние применяется на сервере: выключенные компоненты физически
// переносятся в stash /workspace/<name>/.agctl/stash (невидимы агенту и
// git-autosave), пакеты ставятся в .dsh/skills (dsh подхватывает их сам).
//
// Чип в нативном слоте sidebar.footer.action; виден всем ролям: обычному
// пользователю консоль нужна не меньше разработчика (возможности и промт
// его воркспейсов). Специально developer-возможности (клонирование из
// GitHub) живут в диалоге создания и в консоли не дублируются.
window.__ModuleLoader__.load({
	id: "@arijjia/agctl-workspace-console",
	factory: (require) => {
		var exports = {};
		const React = require("react");

		function ensureStyles() {
			if (document.querySelector("style[data-agctl-wsc-styles]")) return;
			const style = document.createElement("style");
			style.dataset.agctlWscStyles = "1";
			style.textContent = `
				[data-agctl-wsc-overlay] input[type=checkbox] { width:16px;height:16px;accent-color:#1769d2; }
				[data-agctl-wsc-overlay] .wsc-card {
					border:1px solid var(--dsw-alias-border-l1,#333);border-radius:12px;padding:12px 14px;margin:0 0 10px;
				}
				[data-agctl-wsc-overlay] .wsc-row {
					display:flex;align-items:flex-start;gap:9px;padding:6px 2px;
				}
				[data-agctl-wsc-overlay] .wsc-row:hover { background: var(--dsw-alias-interactive-bg-hover, rgba(255,255,255,.06)); border-radius:8px; }
				[data-agctl-wsc-overlay] .wsc-btn {
					padding:7px 13px;border:1px solid #1459b8;border-radius:8px;background:#1769d2;
					color:#fff;cursor:pointer;font:inherit;font-size:13px;font-weight:600;
				}
				[data-agctl-wsc-overlay] .wsc-btn.secondary { background:transparent;color:inherit;border-color:var(--dsw-alias-border-l1,#555);font-weight:400; }
				[data-agctl-wsc-overlay] .wsc-btn:disabled { opacity:.6; cursor:not-allowed; }
				[data-agctl-wsc-overlay] .wsc-prompt {
					width:100%;min-height:96px;box-sizing:border-box;resize:vertical;
					border:1px solid var(--dsw-alias-border-l1,#555);border-radius:8px;padding:8px;
					background:transparent;color:inherit;font:inherit;font-size:12.5px;
				}
				[data-agctl-wsc-overlay] .wsc-badge {
					font-size:10px;padding:1px 7px;border-radius:999px;border:1px solid var(--dsw-alias-border-l1,#555);color:var(--dsw-alias-label-tertiary,#888);
				}
			`;
			document.head.appendChild(style);
		}

		function el(tag, css, text) {
			const node = document.createElement(tag);
			if (css) node.style.cssText = css;
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

		// Ссылка на штатный сервис воркспейсов dsh: удаление снимает запись
		// реестра (chat-brand дополнительно удаляет каталог через agctl).
		let dshWorkspaces = null;

		// deleteProject — удалить воркспейс с диска и из списка. Порядок
		// детерминированный: сначала agctl-каталог (DELETE /api/projects/<имя>),
		// затем запись dsh-реестра, если она есть. Для сироты (каталог есть,
		// записи нет — например, после старого нативного удаления) достаточно
		// первого шага.
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

		// ── чистые функции (test seams) ────────────────────────────────────────

		// shouldShowChip — консоль видна всем ролям (у админа личных
		// воркспейсов нет, чип безвреден: список будет пустым).
		function shouldShowChip(me) {
			return !!(me && ["user", "developer", "admin"].includes(me.role));
		}

		// groupComponents — разбивка каталога по секциям консоли.
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

		// ── UI ────────────────────────────────────────────────────────────────

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

		function buildOverlay() {
			const overlay = el("div", "position:fixed;inset:0;z-index:10001;background:rgba(0,0,0,.55);display:flex;align-items:flex-start;justify-content:center;padding:48px 20px;overflow:auto");
			overlay.dataset.agctlWscOverlay = "1";
			const sheet = el("div", "width:min(760px,100%);background:var(--dsw-alias-bg-layer-1,#161616);color:var(--dsw-alias-label-primary,inherit);border-radius:16px;border:1px solid var(--dsw-alias-border-l1,#333);padding:26px 30px;box-shadow:0 24px 80px rgba(0,0,0,.5);box-sizing:border-box");
			const close = el("button", "float:right;border:none;background:transparent;color:inherit;font-size:22px;cursor:pointer;line-height:1", "×");
			close.setAttribute("aria-label", "Закрыть консоль воркспейсов");
			close.addEventListener("click", () => overlay.remove());
			sheet.appendChild(close);
			sheet.appendChild(el("h2", "margin:2px 0 4px;font-size:18px;font-weight:650", "Воркспейсы: возможности, промт, компоненты"));
			sheet.appendChild(el("p", "margin:0 0 18px;font-size:12px;color:var(--dsw-alias-label-tertiary,#888)",
				"Единое место настройки воркспейсов: возможности из каталога платформы, системный промт и компоненты. «Удалить» стирает воркспейс вместе с каталогом на диске. Выключенный компонент прячется на сервере и не виден агенту; репка не правится."));
			overlay.appendChild(sheet);
			overlay.addEventListener("click", (ev) => { if (ev.target === overlay) overlay.remove(); });
			document.body.appendChild(overlay);
			return sheet;
		}

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

		// catalogRow — строка пакета «Возможностей»: название, описание и
		// кнопка Установить/Убрать.
		function catalogRow(api, pkg, installed, refresh) {
			const row = el("div", "wsc-row");
			const text = el("div", "min-width:0;flex:1");
			const title = el("div", "font-size:13px;font-weight:600;display:flex;gap:8px;align-items:baseline");
			title.appendChild(el("span", null, pkg.title || pkg.id));
			if (installed) title.appendChild(el("span", "wsc-badge", "установлен"));
			text.append(title, el("div", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888);overflow-wrap:anywhere", pkg.description || ""));
			const btn = el("button", "wsc-btn secondary;flex:none", installed ? "Убрать" : "Установить");
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

		// promptSection — редактор системного промта воркспейса (AGENTS.md).
		function promptSection(api, project, statusLine) {
			const wrap = el("div", null, null);
			const head = el("div", "margin:12px 0 2px;font-size:12px;font-weight:700", "Системный промт воркспейса (AGENTS.md)");
			const hint = el("p", "margin:0 0 6px;font-size:11px;color:var(--dsw-alias-label-tertiary,#888)",
				"Один промт на воркспейс: агент читает его в каждой сессии этого воркспейса.");
			const ta = el("textarea", "wsc-prompt");
			ta.placeholder = "Например: презентации собирать по шаблону вуза, отчёты — по ГОСТ…";
			const save = el("button", "wsc-btn secondary;margin-top:6px", "Сохранить промт");
			save.disabled = true;
			ta.addEventListener("input", () => { save.disabled = false; });
			save.addEventListener("click", async () => {
				save.disabled = true;
				statusLine.textContent = "Сохраняем промт…";
				try {
					await api("/api/projects/" + encodeURIComponent(project.name) + "/prompt", "PUT", { prompt: ta.value });
					statusLine.textContent = "Промт сохранён.";
				} catch (err) {
					statusLine.textContent = String(err.message || err);
					save.disabled = false;
				}
			});
			api("/api/projects/" + encodeURIComponent(project.name) + "/prompt")
				.then((d) => { ta.value = (d && d.prompt) || ""; })
				.catch((err) => { ta.placeholder = "Не удалось загрузить: " + (err.message || err); });
			wrap.append(head, hint, ta, save);
			return wrap;
		}

		function projectCard(api, project, onDeleted) {
			const card = el("div", "wsc-card");
			const head = el("div", "display:flex;align-items:center;gap:10px");
			head.appendChild(el("strong", "font-size:14px", project.name));
			head.appendChild(el("span", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888)", project.directory));
			const headRight = el("div", "margin-left:auto;display:flex;gap:6px");
			const remove = el("button", "wsc-btn secondary", "Удалить");
			remove.style.color = "#e06666";
			remove.title = "Удалить воркспейс и его каталог на диске (безвозвратно)";
			headRight.appendChild(remove);
			const toggle = el("button", "wsc-btn secondary", "Настроить");
			headRight.appendChild(toggle);
			head.appendChild(headRight);
			card.appendChild(head);

			const body = el("div", null, null);
			body.style.display = "none";
			card.appendChild(body);
			const statusLine = el("p", "margin:8px 0 0;font-size:12px;color:var(--dsw-alias-label-tertiary,#888);min-height:16px", "");
			card.appendChild(statusLine);

			remove.addEventListener("click", async () => {
				const question = "Удалить воркспейс «" + project.name + "»?\n\n"
					+ "Каталог проекта на диске (" + project.directory + ") будет удалён вместе со всеми файлами — безвозвратно. "
					+ "История сессий в чате останется.";
				if (!window.confirm(question)) return;
				remove.disabled = true;
				statusLine.textContent = "Удаляем воркспейс и каталог…";
				try {
					await deleteProject(api, project);
					statusLine.textContent = "";
					if (onDeleted) onDeleted(card);
				} catch (err) {
					statusLine.textContent = String(err.message || err);
					remove.disabled = false;
				}
			});

			let loaded = false;
			toggle.addEventListener("click", async () => {
				if (loaded) {
					body.style.display = body.style.display === "none" ? "block" : "none";
					return;
				}
				await loadBody();
			});

			async function loadBody() {
				toggle.disabled = true;
				statusLine.textContent = "Читаем настройки воркспейса…";
				try {
					const encName = encodeURIComponent(project.name);
					const [catData, compData] = await Promise.all([
						api("/api/projects/" + encName + "/catalog"),
						api("/api/projects/" + encName + "/components"),
					]);
					const packages = Array.isArray(catData.packages) ? catData.packages : [];
					const components = Array.isArray(compData.components) ? compData.components : [];
					const enabledByID = new Set(components.filter((c) => c.enabled).map((c) => c.id));
					const dirty = () => { save.disabled = false; statusLine.textContent = "Есть изменения — нажмите «Сохранить»."; };
					body.replaceChildren();

					// ── Возможности (платформенный каталог) ────────────────────
					body.appendChild(el("h3", "margin:12px 0 2px;font-size:12px;font-weight:700", "Возможности"));
					body.appendChild(el("p", "margin:0 0 4px;font-size:11px;color:var(--dsw-alias-label-tertiary,#888)",
						"Пакеты платформы: навык + шаблоны/скрипты одним кликом. Ставятся в .dsh/skills и видны агенту в этом воркспейсе."));
					for (const pkg of packages) {
						pkg._project = project.name;
						body.appendChild(catalogRow(api, pkg, !!pkg.installed, () => loadBody()));
					}
					if (!packages.length) {
						body.appendChild(el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888)", "Каталог платформы пуст."));
					}

					// ── Системный промт ────────────────────────────────────────
					body.appendChild(promptSection(api, project, statusLine));

					// ── Компоненты ─────────────────────────────────────────────
					const groups = groupComponents(components);
					for (const key of ["skills", "tools", "data", "docs"]) {
						if (!groups[key].length) continue;
						body.appendChild(el("h3", "margin:12px 0 2px;font-size:12px;font-weight:700", GROUP_TITLES[key]));
						body.appendChild(el("p", "margin:0 0 4px;font-size:11px;color:var(--dsw-alias-label-tertiary,#888)", GROUP_HINTS[key]));
						for (const comp of groups[key]) body.appendChild(componentRow(comp, enabledByID, dirty));
					}
					if (!components.length) {
						body.appendChild(el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888)", "В воркспейсе нет скиллов и инструментов."));
					}

					const save = el("button", "wsc-btn", "Сохранить");
					save.disabled = true;
					save.addEventListener("click", async () => {
						save.disabled = true;
						statusLine.textContent = "Применяем…";
						try {
							const disabled = buildDisabledIDs(components, enabledByID);
							await api("/api/projects/" + encName + "/components", "PUT", { disabled });
							// Следующее раскрытие перечитает каталог с сервера
							// (истина о состоянии — рабочее дерево проекта).
							loaded = false;
							body.style.display = "none";
							statusLine.textContent = disabled.length
								? "Применено: выключено " + disabled.length + "."
								: "Применено: все компоненты включены.";
						} catch (err) {
							statusLine.textContent = String(err.message || err);
							save.disabled = false;
						}
					});
					body.appendChild(save);
					loaded = true;
					body.style.display = "block";
					statusLine.textContent = "";
				} catch (err) {
					statusLine.textContent = String(err.message || err);
				} finally {
					toggle.disabled = false;
				}
			}
			return card;
		}

		async function openConsole(api) {
			const sheet = buildOverlay();
			const list = el("div", null, null);
			sheet.appendChild(list);
			list.appendChild(el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888)", "Читаем воркспейсы…"));
			try {
				const data = await api("/api/projects");
				const projects = Array.isArray(data.projects) ? data.projects : [];
				list.replaceChildren();
				if (!projects.length) {
					list.appendChild(el("p", "font-size:13px;color:var(--dsw-alias-label-tertiary,#888)",
						"Воркспейсов пока нет. Создайте первый кнопкой «+» в списке Workspaces — там же можно сразу выбрать возможности (например, «Презентации по шаблону вуза»)."));
					return;
				}
				for (const project of projects) {
					list.appendChild(projectCard(api, project, (card) => card.remove()));
				}
			} catch (err) {
				list.replaceChildren(el("p", "font-size:13px;color:#e06666", "Не удалось получить список воркспейсов: " + (err.message || err)));
			}
		}

		// ── чип в sidebar.footer.action (ряд под кабинетом, до Settings) ───────
		let chipNode = null;

		function applyRowMetrics(node, wide) {
			node.style.display = "flex";
			node.style.alignItems = "center";
			node.style.flex = "none";
			node.style.minWidth = "0";
			if (wide) {
				node.style.width = "calc(100% + 4px)";
				node.style.margin = "4px -2px";
			} else {
				node.style.width = "36px";
				node.style.margin = "0 0 8px";
			}
		}

		function applyChipMetrics(node, wide) {
			node.style.display = "flex";
			node.style.alignItems = "center";
			node.style.boxSizing = "border-box";
			node.style.borderRadius = "var(--dsw-radius-md, 10px)";
			node.style.cursor = "pointer";
			node.style.color = "inherit";
			node.style.background = "transparent";
			node.style.border = "none";
			node.style.fontFamily = "inherit";
			node.style.fontSize = "13px";
			node.style.lineHeight = "22px";
			node.style.overflow = "hidden";
			node.style.textAlign = "left";
			if (wide) {
				node.style.height = "36px";
				node.style.flex = "1 1 auto";
				node.style.gap = "8px";
				node.style.padding = "0 10px";
			} else {
				node.style.width = "36px";
				node.style.height = "36px";
				node.style.flex = "none";
				node.style.justifyContent = "center";
				node.style.gap = "0";
				node.style.padding = "0";
			}
			const label = node.querySelector("[data-agctl-wsc-label]");
			if (label) label.style.display = wide ? "block" : "none";
		}

		function buildChipNode() {
			const chip = el("button", null, null);
			chip.type = "button";
			chip.dataset.agctlWscChip = "1";
			chip.setAttribute("aria-label", "Воркспейсы: возможности, промт и компоненты");
			const icon = el("span", "width:24px;height:24px;border-radius:7px;flex:none;display:flex;align-items:center;justify-content:center;background:rgba(23,105,210,.18);color:#4d8de8;font-size:13px;font-weight:700", "⛁");
			const label = el("span", "flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap");
			label.dataset.agctlWscLabel = "1";
			label.textContent = "Воркспейсы: возможности";
			chip.append(icon, label);
			const api = apiFactory();
			chip.addEventListener("click", (ev) => {
				ev.stopPropagation();
				void openConsole(api);
			});
			return chip;
		}

		function renderChipInto(container, wide) {
			applyRowMetrics(container, wide);
			if (!chipNode) chipNode = buildChipNode();
			if (chipNode.parentElement !== container) container.appendChild(chipNode);
			applyChipMetrics(chipNode, wide);
			return chipNode;
		}

		function ConsoleChip(props) {
			const wide = props.wide !== false;
			const hostRef = React.useRef(null);
			React.useEffect(() => {
				let alive = true;
				const host = hostRef.current;
				if (!host) return undefined;
				fetch("/api/me", { credentials: "include", headers: { Accept: "application/json" } })
					.then((res) => (res.ok ? res.json() : null))
					.then((data) => {
						if (alive && host.isConnected && shouldShowChip(data && data.user)) renderChipInto(host, wide);
					})
					.catch(() => {});
				return () => { alive = false; };
			}, []);
			React.useEffect(() => {
				const host = hostRef.current;
				if (!host) return;
				applyRowMetrics(host, wide);
				if (chipNode) applyChipMetrics(chipNode, wide);
			}, [wide]);
			return React.createElement("div", { ref: hostRef, "data-agctl-wsc-chip-host": "1" });
		}

		function apply(ctx) {
			ensureStyles();
			// Штатный сервис воркспейсов dsh — для снятия записи реестра при
			// удалении (каталог удаляет agctl, см. deleteProject).
			try { dshWorkspaces = ctx.get && ctx.get("workspaces"); } catch (e) { dshWorkspaces = null; }
			ctx.slots.inject("sidebar.footer.action", () => ctx.slots.register({
				name: "sidebar.footer.action",
				id: "agctl-workspace-console-chip",
				order: 11,
			}, ConsoleChip));
		}

		exports.inject = ["slots"];
		exports.apply = apply;
		// Test seams.
		exports.shouldShowChip = shouldShowChip;
		exports.groupComponents = groupComponents;
		exports.buildDisabledIDs = buildDisabledIDs;
		return exports;
	},
});
