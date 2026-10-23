// agctl chat-brand — client half (hand-written Lazy-CJS bundle).
// Режим 1 «Чат»: брендинг ИИ-мастерская ТюмГУ, свой логотип, плейсхолдер
// первого сообщения, плоский список сессий без Workspaces/файлов/терминала,
// автозакрытие диалога "Preview Notice". Регистрируется только в chat-домашках
// (SeedDshHome chatUI=true); dev-workspaces получают чистый скелет.
window.__ModuleLoader__.load({
	id: "@arijjia/agctl-chat-brand",
	factory: (require) => {
		var module = { exports: {} };
		var exports = module.exports;
		Object.defineProperty(exports, Symbol.toStringTag, { value: "Module" });
		const LOGO = "data:image/png;base64,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";
		const BRAND_TEXT = "ИИ-мастерская ТюмГУ";
		const PLACEHOLDER = "Тюмень на правильном пути";

		function closePreviewNotice() {
			for (const dlg of document.querySelectorAll('[class*=_dialog_]')) {
				const text = dlg.textContent || '';
				if (!/Preview Notice|still in preview/i.test(text)) continue;
				const buttons = [...dlg.querySelectorAll('button')].filter((b) => !b.disabled);
				const target = buttons[buttons.length - 1];
				if (target) target.click();
			}
		}

		function makeLogo(height) {
			const img = document.createElement('img');
			img.src = LOGO;
			img.alt = '';
			img.dataset.agctlLogo = '1';
			img.style.height = `${height}px`;
			img.style.width = 'auto';
			img.style.objectFit = 'contain';
			img.style.display = 'block';
			return img;
		}

		function ensureBrandStyles() {
			if (document.querySelector("style[data-agctl-brand-styles]")) return;
			const style = document.createElement("style");
			style.dataset.agctlBrandStyles = "1";
			// Дубль правила чат-шелла: работает и без рестарта шлюза.
			style.textContent = 'div[style*="grid-template-columns"][class*="_frame"]{grid-template-columns:auto minmax(0px,1fr) minmax(0px,0px)!important}';
			document.head.appendChild(style);
		}

		function applyBrand() {
			// Replace every DeepSeek whale mark (sidebar and empty-state) by the
			// owner-provided Tyumen State University mark.
			for (const svg of document.querySelectorAll('svg[viewBox="0 0 23.16 17.04"]')) {
				const h = Math.max(18, Math.round(svg.getBoundingClientRect().height || 24));
				svg.replaceWith(makeLogo(h));
			}
			// Replace the complete DSH identity row: the old wordmark text is not
			// always represented as visible DOM text in the current web build.
			const identity = document.querySelector('[class*=brandIdentity]');
			if (identity && !identity.querySelector('[data-agctl-brand-title]')) {
				const mark = document.createElement('span');
				mark.dataset.slot = 'sidebar.brand.mark';
				mark.style.display = 'inline-flex';
				mark.appendChild(makeLogo(28));
				const title = document.createElement('span');
				title.dataset.agctlBrandTitle = '1';
				title.textContent = BRAND_TEXT;
				title.style.fontSize = '14px';
				title.style.fontWeight = '600';
				title.style.lineHeight = '1.1';
				title.style.whiteSpace = 'nowrap';
				identity.replaceChildren(mark, title);
				identity.style.display = 'inline-flex';
				identity.style.alignItems = 'center';
				identity.style.gap = '8px';
				identity.style.overflow = 'visible';
				identity.setAttribute('aria-hidden', 'false');
			}
			// Инъекция чат-шелла прячет строку идентичности до подмены (CSS
			// visibility:hidden) — бренд применён, показываем.
			if (identity) identity.style.setProperty('visibility', 'visible', 'important');
		}

		function fixPlaceholder() {
			for (const el of queryAllDeep('span,div,h1,h2,h3,p')) {
				if (el.children.length) continue;
				const t = el.textContent || '';
				if (/Into the Unknown/i.test(t)) el.textContent = t.replace(/Into the Unknown/gi, PLACEHOLDER);
				// Пустой композер: англо-хинт dsh про «что построить» — русским.
				if (/Describe what you want to build/i.test(t)) el.textContent = 'Опиши задачу — / команды, @ файлы и сессии';
				// Плейсхолдер открытой сессии — тоже русским.
				if (/Message or run a task/i.test(t)) el.textContent = 'Напишите сообщение…';
			}
			for (const group of queryAllDeep('[class*="pXSMma_titleGroup"]')) {
				group.style.setProperty('visibility', 'visible', 'important');
			}
			for (const el of queryAllDeep('[data-composer-input="true"]')) {
				el.setAttribute('data-placeholder', 'Напишите сообщение…');
				el.setAttribute('placeholder', 'Напишите сообщение…');
				el.setAttribute('aria-label', 'Напишите сообщение');
			}
			for (const el of queryAllDeep('[data-placeholder],[placeholder]')) {
				for (const attr of ['data-placeholder', 'placeholder']) {
					const value = el.getAttribute(attr) || '';
					if (/message or run a task|commands, @ files/i.test(value)) el.setAttribute(attr, 'Напишите сообщение…');
				}
			}
		}

		// fixModels — группа DeepSeek в выборщике моделей не существует: только
		// квинсы платформенного провайдера agctl. Страховка инъекции чат-шелла
		// (та работает с первого кадра, эта — при прямом заходе в dsh мимо шлюза).
		function fixModels() {
			for (const heading of queryAllDeep('[class*="_heading_"]')) {
				if ((heading.textContent || '').trim() !== 'DeepSeek') continue;
				const group = heading.parentElement;
				if (group && group.parentElement && group.parentElement.matches('[class*="_7KE1Ra_groups"]')) {
					group.style.setProperty('display', 'none', 'important');
				} else {
					heading.style.setProperty('display', 'none', 'important');
				}
			}
		}

		function flattenSessions() {
			const label = [...document.querySelectorAll('[class*=sectionLabel]')].find((e) =>
				/^(workspaces|сессии)$/i.test((e.textContent || '').trim()),
			);
			if (!label) return;
			label.textContent = 'Сессии';
			const root = label.closest('[class*=root]') || label.parentElement?.parentElement;
			const header = label.closest('[class*=sectionHeader]');
			if (header) {
				for (const button of header.querySelectorAll('button')) {
					const a = button.getAttribute('aria-label') || '';
					if (/^(view options|add workspace)$/i.test(a)) button.style.setProperty('display', 'none', 'important');
				}
			}
			const tree = root?.querySelector('[role=tree]');
			if (!tree) return;
			tree.setAttribute('aria-label', 'Сессии');
			// Workspace rows are only internal directory labels. Session rows are
			// siblings in the tree, so hiding these leaves a flat Chat-like history.
			for (const row of tree.querySelectorAll('.YDXeBa_projectRow')) row.style.setProperty('display', 'none', 'important');
			for (const action of root.querySelectorAll('button')) {
				const a = action.getAttribute('aria-label') || '';
				if (/^new session in /i.test(a) || /^workspace actions for /i.test(a)) action.style.setProperty('display', 'none', 'important');
			}
			for (const group of tree.querySelectorAll('[class*=groupSection]')) group.style.setProperty('--dsh-workspace-indent', '0px');
			for (const row of tree.querySelectorAll('.YDXeBa_sessionRow')) {
				row.style.marginLeft = '0';
				row.style.paddingLeft = '12px';
			}
		}

		function queryAllDeep(selector) {
			const out = [];
			const visit = (root) => {
				if (!root || !root.querySelectorAll) return;
				for (const element of root.querySelectorAll(selector)) out.push(element);
				for (const element of root.querySelectorAll('*')) if (element.shadowRoot) visit(element.shadowRoot);
			};
			visit(document);
			return out;
		}

		function hideDevPanels() {
			// DSH Layout may mount right-sidebar controls inside a shadow root.
			// Сначала штатно схлопываем панель (dsh запомнит закрытое состояние и
			// отдаст main-области всю ширину): кнопка скрыта CSS инъекцией,
			// но программный click по display:none работает.
			const rightButtons = queryAllDeep('button[aria-label*="right sidebar"]');
			const collapse = rightButtons.find((b) => /collapse/i.test(b.getAttribute('aria-label') || ''));
			if (collapse && !collapse.dataset.agctlCollapsed) {
				collapse.dataset.agctlCollapsed = '1';
				collapse.click();
			}
			for (const button of rightButtons) {
				button.style.setProperty('display', 'none', 'important');
				button.setAttribute('aria-hidden', 'true');
				button.setAttribute('tabindex', '-1');
				button.dataset.agctlChatHidden = '1';
			}
			for (const row of queryAllDeep('[class*=panelRow]')) {
				const t = (row.textContent || '').trim();
				if (/^(terminal|files|терминал|файлы)$/i.test(t)) row.style.setProperty('display', 'none', 'important');
			}
			// trajectory НЕ прячем: это вкладка шагов агента, владелец ею пользуется.
			for (const control of queryAllDeep('button,[role=tab]')) {
				const label = `${control.getAttribute('aria-label') || ''} ${control.getAttribute('title') || ''} ${control.textContent || ''}`.trim();
				if (/workspace files|new terminal|choose workspace|choose shell|access mode|add files or run commands/i.test(label)) {
					control.style.setProperty('display', 'none', 'important');
					control.setAttribute('aria-hidden', 'true');
					control.setAttribute('tabindex', '-1');
				}
			}
		}

		// ── виртуальные папки сессий (режим 1, цвет = университетский синий) ────
		const FOLDERS_KEY = "agctl-chat-folders.v1";
		const FOLDERS_MIGRATED_KEY = "agctl-chat-folders.server-sync.v1";
		let folderServerAvailable = false;
		let folderStateLoadPromise = null;

		function foldersState() {
			try {
				const s = JSON.parse(localStorage.getItem(FOLDERS_KEY));
				if (s && Array.isArray(s.folders) && s.assign) {
					if (!s.settings || typeof s.settings !== "object") s.settings = {};
					if (typeof s.active !== "string") s.active = "";
					return s;
				}
			} catch (e) {}
			return { folders: [], assign: {}, settings: {}, active: "" };
		}

		async function putFoldersState(state) {
			const res = await fetch("/api/chat/folders", {
				method: "PUT",
				credentials: "include",
				headers: { Accept: "application/json", "Content-Type": "application/json" },
				body: JSON.stringify(state),
			});
			if (!res.ok) throw new Error("HTTP " + res.status);
		}

		function saveFoldersState(state) {
			try { localStorage.setItem(FOLDERS_KEY, JSON.stringify(state)); } catch (e) {}
			if (!folderServerAvailable) return;
			void putFoldersState(state).catch((e) => console.warn("[agctl] folders were not synced to server:", e));
		}

		async function syncAssignedFolderInstructions(state) {
			for (const [session, name] of Object.entries(state.assign || {})) syncFolderInstructions(session, name, state);
		}

		async function hydrateFolderState() {
			if (folderStateLoadPromise) return folderStateLoadPromise;
			folderStateLoadPromise = (async () => {
				try {
					const res = await fetch("/api/chat/folders", { credentials: "include", headers: { Accept: "application/json" } });
					if (!res.ok) return;
					folderServerAvailable = true;
					const data = await res.json();
					const remote = data && data.folders;
					if (!remote || !Array.isArray(remote.folders)) return;
					const local = foldersState();
					const migrated = localStorage.getItem(FOLDERS_MIGRATED_KEY) === "1";
					if (!migrated && local.folders.length && remote.folders.length === 0) {
						localStorage.setItem(FOLDERS_MIGRATED_KEY, "1");
						await putFoldersState(local);
						await syncAssignedFolderInstructions(local);
						return;
					}
					localStorage.setItem(FOLDERS_KEY, JSON.stringify(remote));
					localStorage.setItem(FOLDERS_MIGRATED_KEY, "1");
					await syncAssignedFolderInstructions(remote);
				} catch (e) {
					folderServerAvailable = false; // older agctl server: keep browser-local mode
				}
			})();
			return folderStateLoadPromise;
		}

		function folderColor(name) {
			let hash = 0;
			for (let i = 0; i < name.length; i++) hash = (hash * 31 + name.charCodeAt(i)) >>> 0;
			return `hsl(${hash % 360}, 68%, 42%)`;
		}

		function sessionId(row) {
			const key = row && (row.getAttribute("data-row-key") || row.dataset.sessionId || "");
			if (key.startsWith("session:")) return key.slice("session:".length);
			return key.startsWith("session-") ? key : "";
		}

		function sessionKey(row) {
			return sessionId(row) || (row.textContent || "").trim().slice(0, 120);
		}

		let folderMenu = null;

		function closeFolderMenu() {
			if (folderMenu) { folderMenu.remove(); folderMenu = null; }
		}

		function syncFolderInstructions(session, folderName, state) {
			if (!session || !session.startsWith("session-")) return;
			const instructions = folderName ? ((state.settings[folderName] || {}).instructions || "") : "";
			void fetch("/api/chat/sessions/" + encodeURIComponent(session) + "/folder-instructions", {
				method: "PATCH",
				credentials: "include",
				headers: { Accept: "application/json", "Content-Type": "application/json" },
				body: JSON.stringify({ folder: folderName || "", instructions }),
			}).then(async (res) => {
				if (!res.ok) console.warn("[agctl] folder instructions were not applied (HTTP " + res.status + ")");
			}).catch((e) => console.warn("[agctl] folder instructions were not applied:", e));
		}

		function editFolderSettings(oldName, allowRename = true) {
			const state = foldersState();
			const current = (state.settings[oldName] || {}).instructions || "";
			const overlay = document.createElement("div");
			overlay.dataset.agctlFolderSettings = "1";
			overlay.style.cssText = "position:fixed;inset:0;z-index:10000;background:rgba(0,0,0,.46);display:flex;align-items:center;justify-content:center;padding:20px";
			const card = document.createElement("form");
			card.style.cssText = "width:min(560px,100%);max-height:min(84vh,760px);overflow:auto;background:var(--dsw-alias-bg-layer-1,#fff);color:var(--dsw-alias-label-primary,#222);border:1px solid var(--dsw-alias-border-l1,#ccc);border-radius:14px;padding:22px;box-shadow:0 20px 60px rgba(0,0,0,.32);display:flex;flex-direction:column;gap:14px";
			const heading = document.createElement("h2");
			heading.textContent = "Настройки папки проекта";
			heading.style.cssText = "margin:0;font-size:18px;font-weight:650";
			card.appendChild(heading);
			const nameLabel = document.createElement("label");
			nameLabel.style.cssText = "display:flex;flex-direction:column;gap:6px;font-size:13px;font-weight:600";
			nameLabel.appendChild(document.createTextNode("Название"));
			const nameInput = document.createElement("input");
			nameInput.value = oldName;
			nameInput.disabled = !allowRename;
			nameInput.maxLength = 80;
			nameInput.style.cssText = "box-sizing:border-box;width:100%;min-height:40px;padding:9px 11px;border:1px solid var(--dsw-alias-border-l1,#bbb);border-radius:8px;background:transparent;color:inherit;font:inherit;font-weight:400";
			nameLabel.appendChild(nameInput);
			card.appendChild(nameLabel);
			const instructionsLabel = document.createElement("label");
			instructionsLabel.style.cssText = "display:flex;flex-direction:column;gap:6px;font-size:13px;font-weight:600";
			instructionsLabel.appendChild(document.createTextNode("Инструкции проекта"));
			const instructions = document.createElement("textarea");
			instructions.value = current;
			instructions.rows = 7;
			instructions.maxLength = 16000;
			instructions.placeholder = "Например: для этой папки сначала уточняй требования, затем предлагай план и только после согласования редактируй файлы.";
			instructions.style.cssText = "box-sizing:border-box;width:100%;min-height:150px;resize:vertical;padding:10px 11px;border:1px solid var(--dsw-alias-border-l1,#bbb);border-radius:8px;background:transparent;color:inherit;font:inherit;font-weight:400";
			instructionsLabel.appendChild(instructions);
			card.appendChild(instructionsLabel);
			const note = document.createElement("p");
			note.textContent = "Инструкции сохраняются в AGENTS.local.md назначенных сессий и подхватываются при следующем чтении файлов или переоткрытии сессии.";
			note.style.cssText = "margin:0;font-size:12px;line-height:1.5;color:var(--dsw-alias-label-tertiary,#777)";
			card.appendChild(note);
			const status = document.createElement("p");
			status.style.cssText = "margin:0;min-height:1em;font-size:12px;color:var(--dsw-alias-state-error-primary,#c33)";
			card.appendChild(status);
			const actions = document.createElement("div");
			actions.style.cssText = "display:flex;justify-content:space-between;align-items:center;gap:8px;flex-wrap:wrap";
			const left = document.createElement("div");
			left.style.cssText = "display:flex;gap:8px;flex-wrap:wrap";
			const remove = document.createElement("button");
			remove.type = "button";
			remove.textContent = "Удалить папку";
			remove.style.cssText = "padding:8px 12px;border:1px solid #c33;border-radius:8px;background:transparent;color:#c33;cursor:pointer;font:inherit";
			remove.addEventListener("click", () => {
				if (!window.confirm("Удалить папку «" + oldName + "»? Сессии останутся в чате без папки.")) return;
				for (const [session, assigned] of Object.entries(state.assign)) {
					if (assigned === oldName) { delete state.assign[session]; syncFolderInstructions(session, "", state); }
				}
				state.folders = state.folders.filter((name) => name !== oldName);
				delete state.settings[oldName];
				if (state.active === oldName) state.active = "";
				saveFoldersState(state);
				overlay.remove();
				fixAll();
			});
			left.appendChild(remove);
			const cancel = document.createElement("button");
			cancel.type = "button";
			cancel.textContent = "Отмена";
			cancel.style.cssText = "padding:8px 12px;border:1px solid var(--dsw-alias-border-l1,#bbb);border-radius:8px;background:transparent;color:inherit;cursor:pointer;font:inherit";
			cancel.addEventListener("click", () => overlay.remove());
			left.appendChild(cancel);
			actions.appendChild(left);
			const save = document.createElement("button");
			save.type = "submit";
			save.textContent = "Сохранить";
			save.style.cssText = "padding:8px 14px;border:1px solid #1459b8;border-radius:8px;background:#1769d2;color:#fff;cursor:pointer;font:inherit;font-weight:650";
			actions.appendChild(save);
			card.appendChild(actions);
			card.addEventListener("submit", (ev) => {
				ev.preventDefault();
				const nextName = allowRename ? nameInput.value.trim() : oldName;
				if (!nextName) { status.textContent = "Название не может быть пустым."; return; }
				if (nextName !== oldName && state.folders.includes(nextName)) { status.textContent = "Папка с таким именем уже существует."; return; }
				const cleanInstructions = instructions.value.trim();
				state.folders = state.folders.map((name) => name === oldName ? nextName : name);
				for (const [session, assigned] of Object.entries(state.assign)) if (assigned === oldName) state.assign[session] = nextName;
				delete state.settings[oldName];
				state.settings[nextName] = { instructions: cleanInstructions };
				saveFoldersState(state);
				for (const [session, assigned] of Object.entries(state.assign)) if (assigned === nextName) syncFolderInstructions(session, assigned, state);
				overlay.remove();
				fixAll();
			});
			overlay.addEventListener("click", (ev) => { if (ev.target === overlay) overlay.remove(); });
			overlay.appendChild(card);
			document.body.appendChild(overlay);
			nameInput.focus();
		}

		function openFolderMenu(x, y, row, key) {
			closeFolderMenu();
			const state = foldersState();
			const assigned = state.assign[key] || "";
			const items = [
				...state.folders.map((name) => ({ label: (assigned === name ? "✓ " : "") + name, color: folderColor(name), act: () => { state.assign[key] = name; saveFoldersState(state); syncFolderInstructions(key, name, state); } })),
				{ label: "＋ Новая папка…", act: () => {
					const name = (window.prompt("Имя папки сессий:") || "").trim();
					if (!name) return;
					if (!state.folders.includes(name)) state.folders.push(name);
					state.assign[key] = name;
					state.settings[name] = state.settings[name] || { instructions: "" };
					saveFoldersState(state);
					syncFolderInstructions(key, name, state);
					setTimeout(() => editFolderSettings(name, false), 0);
				} },
			];
			if (assigned) items.push({ label: "Убрать из папки", act: () => { delete state.assign[key]; saveFoldersState(state); syncFolderInstructions(key, "", state); } });
			folderMenu = document.createElement("div");
			folderMenu.dataset.agctlFolderContextMenu = "1";
			folderMenu.style.cssText = "position:fixed;z-index:9999;min-width:180px;padding:6px;border-radius:10px;background:var(--dsw-specific-menu,#fff);box-shadow:0 8px 24px rgba(0,0,0,.25);border:1px solid var(--dsw-alias-border-l1,#ddd);display:flex;flex-direction:column;gap:2px";
			for (const item of items) {
				const btn = document.createElement("button");
				btn.type = "button";
				btn.textContent = item.label;
				btn.style.cssText = "display:flex;align-items:center;gap:8px;border:none;background:transparent;color:inherit;font:inherit;font-size:13px;padding:7px 10px;border-radius:8px;cursor:pointer;text-align:left";
				btn.addEventListener("mouseenter", () => { btn.style.background = "var(--dsw-alias-interactive-bg-hover,rgba(0,0,0,.07))"; });
				btn.addEventListener("mouseleave", () => { btn.style.background = "transparent"; });
				btn.addEventListener("click", () => { item.act(); closeFolderMenu(); fixAll(); });
				folderMenu.appendChild(btn);
			}
			document.body.appendChild(folderMenu);
			const rect = folderMenu.getBoundingClientRect();
			folderMenu.style.left = Math.min(x, window.innerWidth - rect.width - 8) + "px";
			folderMenu.style.top = Math.min(y, window.innerHeight - rect.height - 8) + "px";
		}

		function bindFolderNewSession() {
			if (window.__agctlFolderNewSessionBound) return;
			window.__agctlFolderNewSessionBound = true;
			document.addEventListener("click", (ev) => {
				const button = ev.target && ev.target.closest && ev.target.closest('button[aria-label]');
				if (!button || !/^new session$/i.test((button.getAttribute("aria-label") || "").trim())) return;
				const state = foldersState();
				if (!state.active) return;
				const rows = [...document.querySelectorAll('.YDXeBa_sessionRow')];
				const blank = rows.find((row) => sessionId(row) && (row.textContent || "").trim().toLowerCase().startsWith("new session"));
				if (blank) {
					const id = sessionId(blank);
					state.assign[id] = state.active;
					state.settings[state.active] = state.settings[state.active] || { instructions: "" };
					syncFolderInstructions(id, state.active, state);
				}
				const known = rows.map(sessionId).filter(Boolean);
				state.pendingFolder = { name: state.active, known, at: Date.now() };
				saveFoldersState(state);
				setTimeout(() => fixAll(), 80);
			}, true);
		}

		function applyFolders() {
			bindFolderNewSession();
			const label = [...document.querySelectorAll('[class*=sectionLabel]')].find((e) =>
				/^(сессии|workspaces)$/i.test((e.textContent || "").trim()),
			);
			if (!label) return;
			const root = label.closest('[class*=root]') || label.parentElement?.parentElement;
			if (!root) return;
			const tree = root.querySelector('[role=tree]');
			if (!tree) return;
			const state = foldersState();
			const sessionRows = [...tree.querySelectorAll('.YDXeBa_sessionRow')];
			const pending = state.pendingFolder;
			if (pending && Date.now() - pending.at < 2 * 60 * 1000) {
				const fresh = sessionRows.find((row) => {
					const id = sessionId(row);
					return id && !pending.known.includes(id);
				});
				if (fresh) {
					const id = sessionId(fresh);
					state.assign[id] = pending.name;
					state.settings[pending.name] = state.settings[pending.name] || { instructions: "" };
					delete state.pendingFolder;
					saveFoldersState(state);
					syncFolderInstructions(id, pending.name, state);
				}
			} else if (pending) {
				delete state.pendingFolder;
				saveFoldersState(state);
			}

			// Чипсы видны сразу, даже когда папок ещё нет: это точка входа в создание.
			let chips = root.querySelector('[data-agctl-folders]');
			if (!chips) {
				chips = document.createElement("div");
				chips.dataset.agctlFolders = "1";
				chips.style.cssText = "display:flex;flex-wrap:wrap;gap:6px;padding:2px 10px 8px";
				const header = label.closest('[class*=sectionHeader]');
				(header || label).parentElement.insertBefore(chips, header ? header.nextSibling : label.nextSibling);
			}
			chips.replaceChildren();
			const chip = (name, isActive, onClick) => {
				const b = document.createElement("button");
				b.type = "button";
				b.textContent = name;
				b.style.cssText = `display:inline-flex;align-items:center;gap:6px;border-radius:999px;padding:3px 10px;font-size:12px;cursor:pointer;border:1px solid ${isActive ? folderColor(name === "Все" ? "#1676df" : name) : "var(--dsw-alias-border-l1,#ccc)"};background:${isActive ? "var(--dsw-alias-interactive-bg-hover,rgba(0,0,0,.07))" : "transparent"};color:inherit`;
				if (name !== "Все") {
					const dot = document.createElement("span");
					dot.style.cssText = `width:8px;height:8px;border-radius:999px;background:${folderColor(name)}`;
					b.prepend(dot);
				}
				b.addEventListener("click", onClick);
				return b;
			};
			chips.appendChild(chip("Все", !state.active, () => { state.active = ""; saveFoldersState(state); fixAll(); }));
			for (const name of state.folders) {
				const folderChip = chip(name, state.active === name, () => { state.active = state.active === name ? "" : name; saveFoldersState(state); fixAll(); });
				folderChip.addEventListener("contextmenu", (ev) => { ev.preventDefault(); editFolderSettings(name); });
				chips.appendChild(folderChip);
				const settings = document.createElement("button");
				settings.type = "button";
				settings.textContent = "⚙";
				settings.title = "Настройки папки «" + name + "»";
				settings.setAttribute("aria-label", settings.title);
				settings.style.cssText = "border:0;background:transparent;color:var(--dsw-alias-label-tertiary,#888);padding:2px 4px;border-radius:6px;cursor:pointer;font-size:12px";
				settings.addEventListener("click", () => editFolderSettings(name));
				chips.appendChild(settings);
			}
			const add = document.createElement("button");
			add.type = "button";
			add.textContent = "＋";
			add.title = "Новая папка";
			add.style.cssText = "border:1px dashed var(--dsw-alias-border-l1,#bbb);background:transparent;color:inherit;border-radius:999px;width:24px;height:24px;cursor:pointer;font-size:13px;line-height:1";
			add.addEventListener("click", () => {
				const name = (window.prompt("Имя папки сессий:") || "").trim();
				if (!name) return;
				const fresh = foldersState();
				if (!fresh.folders.includes(name)) fresh.folders.push(name);
				fresh.settings[name] = fresh.settings[name] || { instructions: "" };
				fresh.active = name;
				saveFoldersState(fresh);
				fixAll();
				setTimeout(() => editFolderSettings(name, false), 0);
			});
			chips.appendChild(add);

			// разметка строк: цветная точка папки + фильтр активной папки + контекстное меню
			for (const row of sessionRows) {
				const session = sessionId(row);
				const title = (row.textContent || "").trim().slice(0, 120);
				const key = session || title;
				const isPseudo = /^new session$/i.test(title);
				// Upgrade the pre-W3 title-key mapping to stable DSH session IDs.
				if (session && !state.assign[session] && state.assign[title]) {
					state.assign[session] = state.assign[title];
					delete state.assign[title];
					saveFoldersState(state);
					syncFolderInstructions(session, state.assign[session], state);
				}
				const assigned = state.assign[key] || "";
				if (session && !row.dataset.agctlFolderContextBound) {
					row.dataset.agctlFolderContextBound = "1";
					row.addEventListener("contextmenu", (ev) => { ev.preventDefault(); openFolderMenu(ev.clientX, ev.clientY, row, sessionId(row)); });
				}
				if (assigned) {
					let dot = row.querySelector('[data-agctl-folder-dot]');
					if (!dot) {
						dot = document.createElement("span");
						dot.dataset.agctlFolderDot = "1";
						dot.style.cssText = "width:8px;height:8px;border-radius:999px;flex:none;display:inline-block;margin-right:6px";
						row.insertBefore(dot, row.firstChild);
					}
					dot.style.background = folderColor(assigned);
				} else {
					const dot = row.querySelector('[data-agctl-folder-dot]');
					if (dot) dot.remove();
				}
				const hidden = !isPseudo && state.active && assigned !== state.active;
				row.style.setProperty("display", hidden ? "none" : "", "important");
			}
		}

		document.addEventListener("click", (ev) => {
			if (folderMenu && !folderMenu.contains(ev.target)) closeFolderMenu();
		});

		function fixAll() {
			try { closePreviewNotice(); } catch (e) {}
			try { applyBrand(); } catch (e) {}
			try { fixPlaceholder(); } catch (e) {}
			try { fixModels(); } catch (e) {}
			try { flattenSessions(); } catch (e) {}
			try { applyFolders(); } catch (e) {}
			try { hideDevPanels(); } catch (e) {}
		}

		function apply(ctx) {
			// Developer Tools в chat-home включаем штатной настройкой DSH: иначе
			// встроенный dsh-client-ui-conversation полностью исключает view=trajectory.
			try {
				const developerTools = ctx.configForms && ctx.configForms.developerTools && ctx.configForms.developerTools.enabled;
				if (developerTools && !developerTools.getSnapshot()) {
					void developerTools.setEnabled(true).catch((e) => console.warn("[agctl] не удалось включить Trajectory:", e));
				}
			} catch (e) {}
			ensureBrandStyles();
			void hydrateFolderState().then(() => fixAll());
			if (window.__agctlChatBrandTimer) return;
			fixAll();
			setTimeout(fixAll, 1000);
			setTimeout(fixAll, 4000);
			window.__agctlChatBrandTimer = setInterval(fixAll, 1500);
		}

		exports.inject = ["configForms"];
		exports.apply = apply;
		return module.exports;
	},
});
