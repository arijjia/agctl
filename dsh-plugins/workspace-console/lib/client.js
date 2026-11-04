// agctl workspace-console — консоль компонентов воркспейсов (сценарий 1c-vibe).
// Отдельный плагин вместо вкладки в диалоге создания: список проектов
// пользователя с его скиллами/инструментами/промптами и галочками
// включения. Состояние применяется на сервере (PUT /api/projects/<name>/
// components) и физически переносит выключенные компоненты в stash
// /workspace/<name>/.agctl/stash, невидимый агенту и git-autosave.
//
// Чип в нативном слоте sidebar.footer.action (второй ряд под чипом
// кабинета), виден только developer/admin: как и клонирование из GitHub,
// консоль — developer-возможность.
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

		// ── чистые функции (test seams) ────────────────────────────────────────

		// shouldShowChip — консоль видна роли developer и admin.
		function shouldShowChip(me) {
			return !!(me && ["developer", "admin"].includes(me.role));
		}

		// groupComponents — разбивка каталога по секциям консоли.
		function groupComponents(components) {
			const groups = { skills: [], tools: [], data: [], docs: [] };
			for (const comp of components || []) {
				if (comp.kind === "skill") groups.skills.push(comp);
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
			skills: "Процедуры работы агента (skills/*/SKILL.md).",
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
			sheet.appendChild(el("h2", "margin:2px 0 4px;font-size:18px;font-weight:650", "Воркспейсы и компоненты"));
			sheet.appendChild(el("p", "margin:0 0 18px;font-size:12px;color:var(--dsw-alias-label-tertiary,#888)",
				"Скиллы, инструменты и промпты каждого воркспейса. Выключенный компонент прячется на сервере и не виден агенту; репка не правится."));
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
			if (!comp.enabled) title.appendChild(el("span", "font-size:10px;padding:1px 7px;border-radius:999px;border:1px solid var(--dsw-alias-border-l1,#555);color:var(--dsw-alias-label-tertiary,#888)", "выключен"));
			if (comp.locked) title.appendChild(el("span", "font-size:10px;color:var(--dsw-alias-label-tertiary,#888)", "базовый"));
			const meta = [comp.path, comp.description].filter(Boolean).join(" — ");
			text.append(title, el("div", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888);overflow-wrap:anywhere", meta));
			row.append(box, text);
			return row;
		}

		function projectCard(api, project) {
			const card = el("div", "wsc-card");
			const head = el("div", "display:flex;align-items:center;gap:10px");
			head.appendChild(el("strong", "font-size:14px", project.name));
			head.appendChild(el("span", "font-size:11px;color:var(--dsw-alias-label-tertiary,#888)", project.directory));
			const toggle = el("button", "wsc-btn secondary;margin-left:auto", "Настроить");
			head.appendChild(toggle);
			card.appendChild(head);

			const body = el("div", null, null);
			body.style.display = "none";
			card.appendChild(body);
			const statusLine = el("p", "margin:8px 0 0;font-size:12px;color:var(--dsw-alias-label-tertiary,#888);min-height:16px", "");
			card.appendChild(statusLine);

			let loaded = false;
			toggle.addEventListener("click", async () => {
				if (loaded) {
					body.style.display = body.style.display === "none" ? "block" : "none";
					return;
				}
				toggle.disabled = true;
				statusLine.textContent = "Читаем компоненты…";
				try {
					const data = await api("/api/projects/" + encodeURIComponent(project.name) + "/components");
					const components = Array.isArray(data.components) ? data.components : [];
					const enabledByID = new Set(components.filter((c) => c.enabled).map((c) => c.id));
					const dirty = () => { save.disabled = false; statusLine.textContent = "Есть изменения — нажмите «Сохранить»."; };
					body.replaceChildren();
					const groups = groupComponents(components);
					for (const key of ["skills", "tools", "data", "docs"]) {
						if (!groups[key].length) continue;
						body.appendChild(el("h3", "margin:12px 0 2px;font-size:12px;font-weight:700", GROUP_TITLES[key]));
						body.appendChild(el("p", "margin:0 0 4px;font-size:11px;color:var(--dsw-alias-label-tertiary,#888)", GROUP_HINTS[key]));
						for (const comp of groups[key]) body.appendChild(componentRow(comp, enabledByID, dirty));
					}
					if (!components.length) {
						body.appendChild(el("p", "font-size:12px;color:var(--dsw-alias-label-tertiary,#888)", "Каталог пуст: в воркспейсе нет навыков и инструментов."));
					}
					const save = el("button", "wsc-btn", "Сохранить");
					save.disabled = true;
					save.addEventListener("click", async () => {
						save.disabled = true;
						statusLine.textContent = "Применяем…";
						try {
							const disabled = buildDisabledIDs(components, enabledByID);
							await api("/api/projects/" + encodeURIComponent(project.name) + "/components", "PUT", { disabled });
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
			});
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
						"Воркспейсов пока нет. Создайте их кнопкой «+» в списке Workspaces — пустым или клоном GitHub-репозитория (например, сценарий 1С-vibe)."));
					return;
				}
				for (const project of projects) list.appendChild(projectCard(api, project));
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
			chip.setAttribute("aria-label", "Консоль воркспейсов: навыки, инструменты и промпты");
			const icon = el("span", "width:24px;height:24px;border-radius:7px;flex:none;display:flex;align-items:center;justify-content:center;background:rgba(23,105,210,.18);color:#4d8de8;font-size:13px;font-weight:700", "⛁");
			const label = el("span", "flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap");
			label.dataset.agctlWscLabel = "1";
			label.textContent = "Воркспейсы и компоненты";
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
