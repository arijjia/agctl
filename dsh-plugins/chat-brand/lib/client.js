// agctl chat-brand — client half (hand-written Lazy-CJS bundle).
// Брендинг платформы из профиля организации (window.__AGCTL_BRAND__ инжектит
// шлюз; без него — нейтральный agctl), русский composer hint через штатный
// locale dsh, автозакрытие Preview Notice; dev-панель гейтится на gateway по
// роли. Сайдбар — родной Workspaces browser; кастомного дерева нет.
window.__ModuleLoader__.load({
	id: "@arijjia/agctl-chat-brand",
	factory: (require) => {
		var module = { exports: {} };
		var exports = module.exports;
		Object.defineProperty(exports, Symbol.toStringTag, { value: "Module" });
		// Бренд приходит из профиля организации (internal/profile): шлюз кладёт
		// window.__AGCTL_BRAND__ в <head> до загрузки плагинов. Фолбэки —
		// нейтральный agctl (прямой заход в dsh мимо шлюза).
		const BRAND_CFG = (typeof window !== "undefined" && window.__AGCTL_BRAND__) || {};
		const LOGO = BRAND_CFG.logo || "";
		const BRAND_TEXT = BRAND_CFG.name || "agctl";
		const BRAND_TAGLINE = BRAND_CFG.tagline || "агентские рабочие места";
		const BRAND_LETTER = BRAND_CFG.letter || "a";
		const COMPOSER_RU = {
			"placeholder.default": "Напишите сообщение или задайте задачу, / команды, @ файлы или сессии",
			"placeholder.unavailable": "Сессия недоступна",
			"placeholder.parentOffline": "Родительская сессия отключена; отправка недоступна, но запуск можно остановить",
			"placeholder.hero": "Опишите, что нужно сделать, / команды, @ файлы или сессии",
			"placeholder.workspace": "Выберите воркспейс",
			"placeholder.steerQueue": "Cmd/Ctrl+Enter — дополнить все сообщения в очереди",
			"placeholder.plan": "Продолжить план…",
		};

		function installComposerLocale(ctx) {
			const locale = ctx.locale;
			if (!locale || typeof locale.addLanguage !== "function" || typeof locale.register !== "function") return;
			let explicitPreference;
			try { explicitPreference = ctx.configForms && ctx.configForms.get("locale") && ctx.configForms.get("locale").getSnapshot().value && ctx.configForms.get("locale").getSnapshot().value.preference; }
			catch (e) { explicitPreference = undefined; }
			ctx.effect(() => {
				let removeLanguage = () => {};
				let removeDictionary = () => {};
				try {
					const known = (locale.getLocale().locales || []).some((item) => String(item.id).toLowerCase() === "ru");
					if (!known) removeLanguage = locale.addLanguage({ id: "ru", label: "Русский", fallback: "en" });
					// С dsh 0.2.2 русский словарь conversation штатный — свой
					// регистрируем только как дополнение к dsh без ru.
					try { removeDictionary = locale.register("conversation", "ru", COMPOSER_RU); }
					catch (error) {
						if (!/already has locale/.test(String(error && error.message))) throw error;
						// dsh 0.2.2+: штатный ru — подсказки композера платформы
						// поверх него (про задачу, а не «что построить»).
						removeDictionary = overrideLocaleEntries(locale, "conversation", "ru", COMPOSER_RU) || (() => {});
					}
					// Новому пользователю — русский chat-hint; явный language preference
					// (например, выбранный English в настройках) не перетираем. Namespace
					// без русского словаря честно падают в dsh-English fallback.
					if (explicitPreference == null) locale.setLocale("ru");
				} catch (error) {
					removeDictionary(); removeLanguage();
					console.warn("[agctl] composer locale registration failed:", error);
				}
				return () => { removeDictionary(); removeLanguage(); };
			}, "agctl-chat-brand.composer-locale");
		}

		function closePreviewNotice() {
			for (const dlg of document.querySelectorAll('[class*=_dialog_]')) {
				const text = dlg.textContent || '';
				if (!/Preview Notice|still in preview|предварительн[а-я]* (просмотр|верси)/i.test(text)) continue;
				const buttons = [...dlg.querySelectorAll('button')].filter((b) => !b.disabled);
				const target = buttons[buttons.length - 1];
				if (target) target.click();
			}
		}

		function makeMark(height) {
			if (LOGO) {
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
			const mark = document.createElement('span');
			mark.dataset.agctlMark = '1';
			mark.textContent = BRAND_LETTER;
			mark.style.cssText = `display:inline-flex;align-items:center;justify-content:center;height:${height}px;color:#08a8e0;font-size:${height - 6}px;font-weight:800;line-height:1`;
			return mark;
		}

		function applyBrand() {
			// Replace every DeepSeek whale mark (sidebar and empty-state) by the
			// profile-provided platform mark (logo or letter).
			for (const svg of document.querySelectorAll('svg[viewBox="0 0 23.16 17.04"]')) {
				const h = Math.max(18, Math.round(svg.getBoundingClientRect().height || 24));
				svg.replaceWith(makeMark(h));
			}
			// Replace the complete DSH identity row: the old wordmark text is not
			// always represented as visible DOM text in the current web build.
			const identity = document.querySelector('[class*=brandIdentity]');
			if (identity && !identity.querySelector('[data-agctl-brand-title]')) {
				const mark = document.createElement('span');
				mark.dataset.slot = 'sidebar.brand.mark';
				mark.style.display = 'inline-flex';
				mark.appendChild(makeMark(28));
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
			// Hero-таглайн dsh — статичный текст без locale-ключа: русим один раз
			// при каждом тике (React может перерисовать текст после смены локали).
			for (const el of queryAllDeep('span,div,h1,h2,h3,p')) {
				if (el.children.length) continue;
				const t = el.textContent || '';
				// dsh 0.2.2 переводит слоган сам («В неизвестность»).
				if (/Into the Unknown|^В неизвестность$/i.test(t.trim())) el.textContent = BRAND_TAGLINE;
			}
		}

		function fixPlaceholder() {
			// Русские тексты composer теперь живут в locale-словаре dsh
			// (installComposerLocale); здесь остаётся только показ hero-группы,
			// спрятанной инъекцией чат-шелла до применения бренда.
			for (const group of queryAllDeep('[class*="pXSMma_titleGroup"]')) {
				group.style.setProperty('visibility', 'visible', 'important');
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

		function fixAll(ctx) {
			try { closePreviewNotice(); } catch (e) {}
			try { applyBrand(); } catch (e) {}
			try { fixPlaceholder(); } catch (e) {}
			try { fixModels(); } catch (e) {}
			// Лечение D6 (застрявший выбор воркспейса) — каждый тик, как и бренд:
			// состояние видно только по DOM, других наблюдателей у плагина нет.
			if (ctx) { try { healStuckWorkspacePick(ctx); } catch (e) {} }
		}


		// ── создание воркспейса через родную кнопку ─────────────────────────────
		// Родная кнопка "Add workspace" в шапке Workspaces открывает наш диалог:
		// «Пустой» или «Из GitHub» (POST /api/projects), затем воркспейс
		// регистрируется штатным workspaces.create({path}) и появляется в родном
		// списке. Встроенный DirectoryPicker контейнерных каталогов не используем.
		let brandCtx = null;
		let roleCanUseDevTools = false;
		let roleKnown = false;
		let activeRole = null;
		let devToolsStop = null;
		let stuckPickTicks = 0;


		function workspaceSvc() {
			try { return (brandCtx && brandCtx.get && brandCtx.get("workspaces")) || null; }
			catch (e) { return null; }
		}

		function bindWorkspaceCreate() {
			if (window.__agctlWorkspaceCreateBound) return;
			window.__agctlWorkspaceCreateBound = true;
			document.addEventListener('click', (ev) => {
				const button = ev.target && ev.target.closest && ev.target.closest('button[aria-label], button[title]');
				if (!button) return;
				const labels = [button.getAttribute('aria-label'), button.getAttribute('title')].filter(Boolean);
				if (!labels.some((label) => /^add workspace$/i.test(label.trim()))) return;
				ev.preventDefault();
				ev.stopImmediatePropagation();
				void openWorkspaceCreateDialog().catch((error) => window.alert("Не удалось открыть создание воркспейса: " + error.message));
			}, true);
		}

		// repoNameFromURL — имя воркспейса из GitHub-URL (последний сегмент
		// пути без .git). Чистое имя не отдаем, если это не owner/repo путь.
		function repoNameFromURL(raw) {
			if (!raw) return "";
			let parts;
			try { parts = new URL(raw).pathname.split('/').filter(Boolean); }
			catch (e) { return ""; }
			if (parts.length !== 2) return "";
			const name = parts[1].replace(/\.git$/i, "");
			return /^[\p{L}\p{N}][\p{L}\p{N}._-]*$/u.test(name) ? name : "";
		}

		// fetchScenarioPresets — готовые сценарии платформы (GET /api/config).
		async function fetchScenarioPresets() {
			try {
				const res = await fetch('/api/config', { credentials: 'include', headers: { Accept: 'application/json' } });
				const data = await res.json().catch(() => null);
				return Array.isArray(data && data.presets) ? data.presets : [];
			} catch (e) { return []; }
		}

		// fetchCatalogPackages — «возможности воркспейса» из платформенного
		// каталога (GET /api/catalog): навык+шаблоны одним флагом. Один и тот
		// же каталог обслуживает консоль воркспейсов — единое место правды.
		async function fetchCatalogPackages() {
			try {
				const res = await fetch('/api/catalog', { credentials: 'include', headers: { Accept: 'application/json' } });
				const data = await res.json().catch(() => null);
				return Array.isArray(data && data.packages) ? data.packages : [];
			} catch (e) { return []; }
		}

		// packagesRow — компактный выбор возможностей при создании: один
		// чекбокс на пакет с подписью-описанием (без россыпи настроек — всё
		// остальное настраивается потом в консоли воркспейсов).
		function packagesRow(packages) {
			const row = document.createElement('div');
			row.style.cssText = 'display:flex;flex-direction:column;gap:6px';
			row.appendChild(Object.assign(document.createElement('span'), {
				textContent: 'Возможности (можно добавить и потом — в «Воркспейсы: возможности»)',
				style: 'font-size:12px;font-weight:600',
			}));
			for (const pkg of packages) {
				const label = document.createElement('label');
				label.style.cssText = 'display:flex;align-items:flex-start;gap:8px;border:1px solid var(--dsw-alias-border-l1,#bbb);border-radius:8px;padding:7px 9px;cursor:pointer;font:inherit;font-size:12px';
				const box = document.createElement('input');
				box.type = 'checkbox';
				box.dataset.agctlWscPackage = pkg.id;
				box.style.cssText = 'width:15px;height:15px;margin:1px 0 0;accent-color:#1769d2';
				const text = document.createElement('div');
				text.style.cssText = 'min-width:0';
				const title = document.createElement('div');
				title.style.cssText = 'font-weight:600';
				title.textContent = pkg.title || pkg.id;
				const desc = document.createElement('div');
				desc.style.cssText = 'color:var(--dsw-alias-label-tertiary,#888);overflow-wrap:anywhere';
				desc.textContent = pkg.description || '';
				text.append(title, desc);
				label.append(box, text);
				row.appendChild(label);
			}
			return row;
		}

		function selectedPackages(root) {
			return [...root.querySelectorAll('input[data-agctl-wsc-package]')]
				.filter((b) => b.checked)
				.map((b) => b.dataset.agctlWscPackage);
		}

		// presetsRow — ряд кнопок «готовый сценарий»: один клик заполняет URL
		// и имя; никакой россыпи настроек при создании воркспейса.
		function presetsRow(presets, card, name, git) {
			const row = document.createElement('div');
			row.style.cssText = 'display:flex;flex-direction:column;gap:6px';
			row.appendChild(Object.assign(document.createElement('span'), {
				textContent: 'Готовые сценарии', style: 'font-size:12px;font-weight:600',
			}));
			const buttons = document.createElement('div');
			buttons.style.cssText = 'display:flex;gap:6px;flex-wrap:wrap';
			for (const preset of presets) {
				const btn = document.createElement('button');
				btn.type = 'button';
				btn.textContent = preset.title || preset.id;
				btn.title = preset.description || '';
				btn.style.cssText = 'padding:6px 10px;border:1px solid var(--dsw-alias-border-l1,#bbb);border-radius:999px;background:transparent;color:inherit;cursor:pointer;font:inherit;font-size:12px;text-align:left';
				btn.addEventListener('click', () => {
					const urlInput = git && git.querySelector('input');
					const nameInput = name.querySelector('input');
					if (urlInput && preset.cloneUrl) {
						urlInput.value = preset.cloneUrl;
						// Пресет = осознанный выбор сценария: имя подставляем всегда.
						const derived = repoNameFromURL(preset.cloneUrl);
						if (derived) nameInput.value = derived;
					}
					status.textContent = preset.description || '';
				});
				buttons.appendChild(btn);
			}
			row.appendChild(buttons);
			return row;
		}

		async function openWorkspaceCreateDialog() {
			if (document.querySelector('[data-agctl-workspace-create]')) return;
			const meResponse = await fetch('/api/me', { credentials: 'include', headers: { Accept: 'application/json' } });
			if (!meResponse.ok) throw new Error('Сессия agctl недоступна (HTTP ' + meResponse.status + ')');
			const meData = await meResponse.json();
			const canClone = !!(meData && meData.user && ['developer', 'admin'].includes(meData.user.role));
			const presets = canClone ? await fetchScenarioPresets() : [];
			const packages = await fetchCatalogPackages();
			const overlay = document.createElement('div');
			overlay.dataset.agctlWorkspaceCreate = '1';
			overlay.style.cssText = 'position:fixed;inset:0;z-index:10000;background:rgba(0,0,0,.46);display:flex;align-items:center;justify-content:center;padding:20px';
			const card = document.createElement('form');
			card.style.cssText = 'width:min(480px,100%);background:var(--dsw-alias-bg-layer-1,#fff);color:var(--dsw-alias-label-primary,#222);border:1px solid var(--dsw-alias-border-l1,#ccc);border-radius:14px;padding:22px;box-shadow:0 20px 60px rgba(0,0,0,.32);display:flex;flex-direction:column;gap:12px';
			const heading = document.createElement('h2');
			heading.textContent = 'Новый воркспейс';
			heading.style.cssText = 'margin:0;font-size:18px;font-weight:650';
			const name = labeled('Имя', input('Имя воркспейса'));
			const git = canClone ? labeled('Публичный GitHub HTTPS URL (необязательно)', input('https://github.com/user/repo.git')) : null;
			const status = document.createElement('p');
			status.style.cssText = 'margin:0;min-height:18px;font-size:12px;color:var(--dsw-alias-label-tertiary,#888)';
			const actions = document.createElement('div');
			actions.style.cssText = 'display:flex;gap:8px;justify-content:flex-end';
			const cancel = document.createElement('button');
			cancel.type = 'button';
			cancel.textContent = 'Отмена';
			cancel.style.cssText = 'padding:8px 12px;border:1px solid var(--dsw-alias-border-l1,#bbb);border-radius:8px;background:transparent;color:inherit;cursor:pointer;font:inherit;font-size:13px;line-height:18px';
			const submit = document.createElement('button');
			submit.type = 'submit';
			submit.textContent = 'Создать';
			submit.style.cssText = 'padding:8px 14px;border:1px solid #1459b8;border-radius:8px;background:#1769d2;color:#fff;cursor:pointer;font:inherit;font-size:13px;line-height:18px;font-weight:600';
			actions.append(cancel, submit);
			card.append(heading);
			// Готовые сценарии — по одной кнопке на пресет: без россыпи настроек
			// при создании (сценарий настраивается потом, в консоли воркспейсов).
			if (presets.length) card.appendChild(presetsRow(presets, card, name, git));
			// Возможности — те же пакеты, что и в консоли воркспейсов: один
			// серверный каталог, единое место правды о настройках воркспейса.
			if (packages.length) card.appendChild(packagesRow(packages));
			card.append(name);
			if (git) card.appendChild(git);
			card.append(status, actions);
			overlay.appendChild(card);
			document.body.appendChild(overlay);
			const close = () => overlay.remove();
			cancel.addEventListener('click', close);
			overlay.addEventListener('click', (ev) => { if (ev.target === overlay) close(); });

			const nameInput = name.querySelector('input');
			// Автоподстановка имени: клонировали репу — имя подставилось само
			// (последний сегмент пути без .git). Пустое имя сервер выводит тоже,
			// но живой префилл даёт пользователю контроль до отправки.
			if (git) {
				const urlInput = git.querySelector('input');
				urlInput.addEventListener('input', () => {
					const derived = repoNameFromURL(urlInput.value.trim());
					if (derived) nameInput.value = derived;
				});
			}

			function input(placeholder) {
				const el = document.createElement('input');
				el.style.cssText = 'box-sizing:border-box;width:100%;min-height:38px;padding:8px 11px;border:1px solid var(--dsw-alias-border-l1,#bbb);border-radius:8px;background:transparent;color:inherit;font:inherit';
				el.placeholder = placeholder;
				return el;
			}
			function labeled(text, node) {
				const wrap = document.createElement('label');
				wrap.style.cssText = 'display:flex;flex-direction:column;gap:6px;font-size:12px;font-weight:600';
				wrap.append(document.createTextNode(text), node);
				return wrap;
			}

			card.addEventListener('submit', async (ev) => {
				ev.preventDefault();
				const cloneUrl = git ? git.querySelector('input').value.trim() : "";
				// Имя можно оставить пустым при клонировании: agctl выведет его из URL.
				const projectName = nameInput.value.trim();
				if (!projectName && !cloneUrl) { status.textContent = 'Укажите имя воркспейса или GitHub-URL.'; return; }
				submit.disabled = true;
				status.textContent = 'Создаём…';
				try {
					const body = { name: projectName };
					if (cloneUrl) body.cloneUrl = cloneUrl;
					const wanted = selectedPackages(card);
					if (wanted.length) body.packages = wanted;
					const res = await fetch('/api/projects', {
						method: 'POST', credentials: 'include',
						headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
						body: JSON.stringify(body),
					});
					const data = await res.json().catch(() => ({}));
					if (!res.ok) throw new Error((data && data.error) || 'HTTP ' + res.status);
					const directory = data && data.project && data.project.directory;
					if (!directory) throw new Error('agctl не вернул каталог воркспейса');
					const failed = Array.isArray(data.packagesFailed) ? data.packagesFailed : [];
					const workspaces = workspaceSvc();
					if (!workspaces || typeof workspaces.create !== 'function') throw new Error('DSH Workspace service недоступен');
					await workspaces.create({ path: directory });
					close();
					if (failed.length) {
						window.alert('Воркспейс создан, но пакеты не установились: ' + failed.join(', ') + '. Установите их позже через «Воркспейсы: возможности».');
					}
				} catch (error) {
					status.textContent = String(error.message || error);
					submit.disabled = false;
				}
			});
		}

		async function syncRoleControls(ctx) {
			// dsh 0.2.1-rc.2: ctx.configForms.developerTools — DeveloperToolsPreference
			// с полем enabled (observable) и методом setEnabled на самой preference.
			// Прошлый код звал setEnabled на observable — TypeError в подписке.
			const preference = ctx.configForms && ctx.configForms.developerTools;
			const enabledStore = preference && preference.enabled;
			if (!preference || !enabledStore || typeof enabledStore.getSnapshot !== "function") return;
			const response = await fetch("/api/me", { credentials: "include", headers: { Accept: "application/json" } });
			if (!response.ok) return; // fail closed: pre-paint CSS уже схлопнул dev-поверхности
			const data = await response.json();
			// Правая панель теперь у всех ролей: в ней живёт превью deliverables
			// (файлы, которые агент предъявил в чате — кейс презентаций). Кейс
			// «юзер не может открыть презентацию» ловился именно здесь.
			roleCanUseDevTools = !!(data && data.user && data.user.role);
			roleKnown = !!(data && data.user && data.user.role);
			const role = data && data.user && data.user.role;
			activeRole = roleKnown ? role : null;
			// Роли user в правой панели скрываем «двери» терминала и workspace-
			// файлов (гайд пустой панели), оставляя превью файлов: терминал —
			// developer-поверхность. Гардиной служит CSS по data-атрибуту
			// гайд-капсул (dsh ставит data-sidebar-right-guide-entry = kind).
			ensureRoleSurfaceStyles(role);
			const layout = (ctx.get && ctx.get("layout")) || ctx.layout;
			if (layout && typeof layout.closeRightbar === "function" && !roleKnown) layout.closeRightbar();
			// Нативный toggle в Settings → General остаётся штатным: пользователь
			// управляет видимостью правой колонки сам.
			if (!devToolsStop && typeof enabledStore.subscribe === "function") {
				devToolsStop = enabledStore.subscribe(() => { /* toggle свободен */ });
				ctx.effect(() => () => {
					devToolsStop?.();
					devToolsStop = null;
				}, "agctl-chat-brand.role-gate");
			}
			const enabled = !!enabledStore.getSnapshot();
			if (roleCanUseDevTools !== enabled) {
				await preference.setEnabled(roleCanUseDevTools);
			}
			if (roleCanUseDevTools) {
				// D8: снимаем pre-paint схлопывание правой колонки после принятия
				// роли и включения controls. Дальше видимость решает нативный DSH
				// rightbar — пустую колонку плагин сам не открывает.
				await new Promise((resolve) => typeof requestAnimationFrame === "function" ? requestAnimationFrame(() => resolve()) : setTimeout(resolve, 0));
				if (typeof document !== "undefined" && document.documentElement) document.documentElement.dataset.agctlDeveloperToolsReady = "1";
			}
		}

		// ensureRoleSurfaceStyles — ролевой гард поверхностей правой панели.
		// user: без терминала и workspace-файлов (их «двери» в гайде пустой
		// панели скрыты); developer/admin — всё штатное.
		function ensureRoleSurfaceStyles(role) {
			if (typeof document === "undefined") return;
			const id = "agctl-role-surfaces";
			let style = document.getElementById(id);
			if (role !== "user") {
				if (style) style.remove();
				return;
			}
			if (!style) {
				style = document.createElement("style");
				style.id = id;
				document.head.appendChild(style);
			}
			style.textContent = `
				[data-sidebar-right-guide-entry="terminal"],
				[data-sidebar-right-guide-entry="files"] { display: none !important; }
			`;
		}

		// ── первая сессия и застрявший выбор воркспейса (дефект D6) ────────────
		// dsh 0.2.1-rc.2 в двух местах может оставить пользователя на пунктирном
		// composer'е «Выберите воркспейс»: (1) холодный старт без сессий —
		// восстановление выбора не создаёт первую сессию, если бэкенд ещё греется;
		// (2) выбор воркспейса в hero-picker'е: навигация отменяется соседней
		// (layout.selectPanel/beginNavigation) или создание сессии падает молча —
		// чип уже показывает папку, а сессия не открыта и композер остаётся
		// неактивным навсегда. Штатный выход — кнопка «New session» у папки;
		// оба авто-лечебника делают ровно тот же штатный uiWorkspace.startSession.
		//
		// D7: режим разрешений роли user (danger-full-access) задаёт сервер —
		// permission.defaultPreset в dsh-home на каждом старте контейнера
		// (provision.EnsurePermissionDefaults). Явный agentPreset плагин не
		// передаёт: с dsh 0.2.2 это пресет агента, а не разрешений, и
		// «danger-full-access» там неизвестен — создание сессии падало.

		function uiWorkspaceSvc(ctx) {
			try { return (ctx.get && ctx.get("uiWorkspace")) || ctx.uiWorkspace || null; }
			catch (e) { return null; }
		}

		function heroState() {
			if (typeof document === "undefined") return null;
			const composer = document.querySelector('[data-composer-input]');
			const content = document.querySelector('[data-conversation-content]');
			if (!composer || !content) return null;
			const session = content.getAttribute('data-conversation-session') || '';
			const chip = [...document.querySelectorAll('button[aria-haspopup="menu"]')]
				.find((b) => /workspace|воркспейс/i.test(b.getAttribute('aria-label') || ''));
			if (!chip) return null;
			return {
				composerEditable: composer.getAttribute('contenteditable') === 'true',
				session: session,
				chipText: (chip.textContent || '').trim(),
				chipOpen: chip.getAttribute('aria-expanded') === 'true',
				menuVisible: !!document.querySelector('[role="menu"]'),
			};
		}

		// ensureFirstWorkspaceSession: холодный старт (списки готовы, сессий нет —
		// ни активных, ни архивных) → открыть первую сессию Default workspace тем же
		// переходом, что и кнопка «New session». Одна попытка на загрузку страницы.
		function ensureFirstWorkspaceSession(ctx) {
			const uiWorkspace = uiWorkspaceSvc(ctx);
			const workspaceList = uiWorkspace && uiWorkspace.workspaces && uiWorkspace.workspaces.list;
			const sessionList = uiWorkspace && uiWorkspace.sessions && uiWorkspace.sessions.list;
			if (!uiWorkspace || typeof uiWorkspace.startSession !== "function" || !workspaceList || !sessionList) return;
			ctx.effect(() => {
				let started = false;
				const tryStart = () => {
					if (started) return;
					const workspaces = workspaceList.getSnapshot && workspaceList.getSnapshot();
					const sessions = sessionList.getSnapshot && sessionList.getSnapshot();
					if (!workspaces || !sessions || workspaces.phase !== "ready" || sessions.phase !== "ready") return;
					if ((sessions.ids || []).length > 0 || (workspaces.archivedSessionIds || []).length > 0) return;
					const workspace = (workspaces.items || []).find((item) => item.path === "/workspace" || item.title === "Default workspace");
					if (!workspace) return;
					started = true;
					uiWorkspace.startSession(workspace.workspaceId);
				};
				const stops = [];
				if (typeof workspaceList.subscribe === "function") stops.push(workspaceList.subscribe(tryStart));
				if (typeof sessionList.subscribe === "function") stops.push(sessionList.subscribe(tryStart));
				tryStart();
				return () => stops.forEach((stop) => stop());
			}, "agctl-chat-brand.first-workspace-session");
		}

		// healStuckWorkspacePick: hero без сессии, но чип уже показывает выбранную
		// папку — выбор не доехал до сессии (отменённая навигация/тихий отказ RPC).
		// Состояние должно продержаться два подряд тика (~3 с), чтобы не воевать с
		// штатной навигацией, которая в полёте; меню выбора должно быть закрыто.
		function healStuckWorkspacePick(ctx) {
			const uiWorkspace = uiWorkspaceSvc(ctx);
			if (!uiWorkspace || typeof uiWorkspace.startSession !== "function") return;
			const state = heroState();
			if (!state || state.composerEditable || state.session || state.chipOpen || state.menuVisible) {
				stuckPickTicks = 0;
				return;
			}
			if (!state.chipText || /^choose workspace$|^выберите воркспейс$/i.test(state.chipText)) {
				stuckPickTicks = 0; // выбор ещё не сделан — это штатный hero, не трогаем
				return;
			}
				stuckPickTicks += 1;
			if (stuckPickTicks < 2) return;
			stuckPickTicks = 0;
			// без workspaceId = недавний воркспейс: тот же путь, что у «+» у папки;
			// user получает agentPreset явно (D7: reuse чужих blank'ов сохраняет их пресет)
			uiWorkspace.startSession(undefined);
		}

		// ── удаление воркспейса удаляет каталог (agctl-семантика) ───────────────
		// Нативное удаление dsh снимает только запись реестра ("The folder and
		// session logs will be kept") — каталог на диске оставался, и повторное
		// создание воркспейса с тем же именем падало «уже существует».
		// Оборачиваем штатный workspaces.delete: сначала agctl удаляет каталог
		// (DELETE /api/projects/<имя>), затем снимается запись реестра. Один и
		// тот же сервис используют и нативный сайдбар, и консоль воркспейсов.

		// projectNameFromWorkspacePath — имя agctl-проекта из пути воркспейса.
		// "/workspace/<имя>" → "<имя>"; сам дефолтный "/workspace" и любые
		// вложенные/чужие пути → "" (agctl-проекты всегда один сегмент).
		function projectNameFromWorkspacePath(path) {
			if (typeof path !== "string") return "";
			if (!path.startsWith("/workspace/")) return "";
			const name = path.slice("/workspace/".length);
			if (!name || name.includes("/")) return "";
			return name;
		}

		function installWorkspaceDeleteHook(ctx) {
			const svc = workspaceSvc();
			if (!svc || typeof svc.delete !== "function" || svc.__agctlDeleteHooked) return;
			const original = svc.delete.bind(svc);
			svc.delete = async (workspaceId) => {
				let path = "";
				try {
					const snap = svc.list && svc.list.getSnapshot ? svc.list.getSnapshot() : null;
					const item = snap && Array.isArray(snap.items)
						? snap.items.find((w) => w && w.workspaceId === workspaceId)
						: null;
					path = item && item.path;
				} catch (e) { path = ""; }
				const name = projectNameFromWorkspacePath(path);
				if (name) {
					const res = await fetch("/api/projects/" + encodeURIComponent(name), {
						method: "DELETE", credentials: "include", headers: { Accept: "application/json" },
					});
					if (!res.ok) {
						let msg = "HTTP " + res.status;
						try {
							const data = await res.json();
							if (data && data.error) msg = data.error;
						} catch (e) { /* тело не JSON */ }
						throw new Error("agctl не удалил каталог воркспейса: " + msg);
					}
				}
				return original(workspaceId);
			};
			svc.__agctlDeleteHooked = true;
		}

		// installWorkspaceDeleteLocale — честный русский текст нативного диалога
		// удаления (namespace "workspace" принадлежит dsh-client-ui-workspace;
		// dsh регистрирует zh/en, ru свободен). Частичный словарь: недостающие
		// ключи падают в en-fallback как раньше.
		// overrideLocaleEntries — подмена ключей уже зарегистрированного словаря
		// (LocaleRuntime.dicts: ns → locale → entries). Возвращает функцию отката
		// или null, если внутренняя форма рантайма не та.
		function overrideLocaleEntries(locale, ns, lang, entries) {
			const byNs = locale && locale.dicts;
			const byLang = byNs && typeof byNs.get === "function" ? byNs.get(ns) : null;
			const dict = byLang && typeof byLang.get === "function" ? byLang.get(lang) : null;
			if (!dict || typeof dict !== "object" || Object.isFrozen(dict)) return null;
			const prev = {};
			for (const key of Object.keys(entries)) { prev[key] = dict[key]; dict[key] = entries[key]; }
			try { if (typeof locale.publish === "function") locale.publish(locale.snapshot && locale.snapshot.active, false); } catch (e) {}
			return () => {
				for (const key of Object.keys(prev)) {
					if (prev[key] === undefined) delete dict[key]; else dict[key] = prev[key];
				}
			};
		}

		function installWorkspaceDeleteLocale(ctx) {
			const locale = ctx.locale;
			if (!locale || typeof locale.register !== "function") return;
			const entries = {
				"delete.workspace": "Удалить воркспейс",
				"delete.desc": "Воркспейс «{name}» будет удалён из списка, а его каталог на диске — стёрт вместе с файлами проекта. История сессий в чате останется.",
				"delete.pending": "Удаляем воркспейс и каталог…",
				"cancel": "Отмена",
			};
			let removeDictionary = () => {};
			try {
				removeDictionary = locale.register("workspace", "ru", entries);
			} catch (error) {
				// dsh 0.2.2+ несёт свой ru для workspace, и его delete.desc обещает
				// «папка сохранится» — у agctl удаление стирает каталог. Второй
				// словарь register не принимает: точечно подменяем наши ключи в
				// штатном и возвращаем их при выгрузке плагина.
				removeDictionary = overrideLocaleEntries(locale, "workspace", "ru", entries);
				if (!removeDictionary) {
					console.warn("[agctl] workspace delete locale:", error);
					return;
				}
			}
			ctx.effect(() => removeDictionary, "agctl-chat-brand.workspace-delete-locale");
		}

		function apply(ctx) {
			brandCtx = ctx;
			bindWorkspaceCreate();
			// Удаление воркспейса = удаление каталога (agctl-семантика).
			try { installWorkspaceDeleteHook(ctx); } catch (e) { console.warn("[agctl] delete hook:", e); }
			try { installWorkspaceDeleteLocale(ctx); } catch (e) { console.warn("[agctl] delete locale:", e); }
			// D6 (1): холодный старт без сессий — открыть первую сессию Default
			// workspace штатным uiWorkspace.startSession (тот же переход, что у
			// кнопки «New session» у папки). Сервис приходит инъекцией; если
			// недоступен — функция мягко выходит, композер живёт как раньше.
			try { ensureFirstWorkspaceSession(ctx); } catch (e) { console.warn("[agctl] first workspace session:", e); }
			// Русский composer hint — штатным locale-контрактом dsh: словарь для
			// namespace conversation + язык ru с fallback en. Прямая подмена
			// data-placeholder перезаписывалась React'ом; словарь — устойчив.
			try { installComposerLocale(ctx); } catch (e) { console.warn("[agctl] composer locale:", e); }
			// Developer Tools и правая панель доступны только developer/admin.
			void syncRoleControls(ctx).catch((e) => console.warn("[agctl] role settings:", e));
			if (window.__agctlChatBrandTimer) return;
			fixAll(ctx);
			setTimeout(() => fixAll(ctx), 1000);
			setTimeout(() => fixAll(ctx), 4000);
			window.__agctlChatBrandTimer = setInterval(() => fixAll(ctx), 1500);
		}

		exports.inject = ["configForms", "locale", "layout", "uiWorkspace"];
		exports.apply = apply;
		// Unit-test seam: role policy is independent of the UI renderer.
		exports.__testSyncRoleControls = syncRoleControls;
		exports.__testHealStuckWorkspacePick = healStuckWorkspacePick;
		exports.__testEnsureFirstWorkspaceSession = ensureFirstWorkspaceSession;
		exports.__testHeroState = heroState;
		exports.__testProjectNameFromWorkspacePath = projectNameFromWorkspacePath;
		return module.exports;
	},
});
