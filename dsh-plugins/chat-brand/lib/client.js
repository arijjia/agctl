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
					removeDictionary = locale.register("conversation", "ru", COMPOSER_RU);
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
				if (!/Preview Notice|still in preview/i.test(text)) continue;
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
				if (/Into the Unknown/i.test(t)) el.textContent = t.replace(/Into the Unknown/gi, BRAND_TAGLINE);
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

		function fixAll() {
			try { closePreviewNotice(); } catch (e) {}
			try { applyBrand(); } catch (e) {}
			try { fixPlaceholder(); } catch (e) {}
			try { fixModels(); } catch (e) {}
		}

		// ── создание воркспейса через родную кнопку ─────────────────────────────
		// Родная кнопка "Add workspace" в шапке Workspaces открывает наш диалог:
		// «Пустой» или «Из GitHub» (POST /api/projects), затем воркспейс
		// регистрируется штатным workspaces.create({path}) и появляется в родном
		// списке. Встроенный DirectoryPicker контейнерных каталогов не используем.
		let brandCtx = null;
		let roleCanUseDevTools = false;
		let devToolsStop = null;

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

		async function openWorkspaceCreateDialog() {
			if (document.querySelector('[data-agctl-workspace-create]')) return;
			const meResponse = await fetch('/api/me', { credentials: 'include', headers: { Accept: 'application/json' } });
			if (!meResponse.ok) throw new Error('Сессия agctl недоступна (HTTP ' + meResponse.status + ')');
			const meData = await meResponse.json();
			const canClone = !!(meData && meData.user && ['developer', 'admin'].includes(meData.user.role));
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
			card.append(heading, name);
			if (git) card.appendChild(git);
			card.append(status, actions);
			overlay.appendChild(card);
			document.body.appendChild(overlay);
			const close = () => overlay.remove();
			cancel.addEventListener('click', close);
			overlay.addEventListener('click', (ev) => { if (ev.target === overlay) close(); });

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
				const projectName = name.querySelector('input').value.trim();
				const cloneUrl = git ? git.querySelector('input').value.trim() : "";
				if (!projectName) { status.textContent = 'Укажите имя воркспейса.'; return; }
				submit.disabled = true;
				status.textContent = 'Создаём…';
				try {
					const body = { name: projectName };
					if (cloneUrl) body.cloneUrl = cloneUrl;
					const res = await fetch('/api/projects', {
						method: 'POST', credentials: 'include',
						headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
						body: JSON.stringify(body),
					});
					const data = await res.json().catch(() => ({}));
					if (!res.ok) throw new Error((data && data.error) || 'HTTP ' + res.status);
					const directory = data && data.project && data.project.directory;
					if (!directory) throw new Error('agctl не вернул каталог воркспейса');
					const workspaces = workspaceSvc();
					if (!workspaces || typeof workspaces.create !== 'function') throw new Error('DSH Workspace service недоступен');
					await workspaces.create({ path: directory });
					close();
				} catch (error) {
					status.textContent = String(error.message || error);
					submit.disabled = false;
				}
			});
		}

		async function syncRoleControls(ctx) {
			const developerTools = ctx.configForms && ctx.configForms.developerTools && ctx.configForms.developerTools.enabled;
			if (!developerTools) return;
			const response = await fetch("/api/me", { credentials: "include", headers: { Accept: "application/json" } });
			if (!response.ok) return;
			const data = await response.json();
			roleCanUseDevTools = !!(data && data.user && ["developer", "admin"].includes(data.user.role));
			// Правой панелью управляет штатный DSH Layout API. Обычному user
			// закрываем её без переходного кадра (pre-paint CSS в gateway); dev/admin
			// получаем полный нативный rightbar.
			const layout = (ctx.get && ctx.get("layout")) || ctx.layout;
			if (layout) {
				if (roleCanUseDevTools && typeof layout.openRightbar === "function") layout.openRightbar(true, false);
				if (!roleCanUseDevTools && typeof layout.closeRightbar === "function") layout.closeRightbar();
			}
			// Пользователь может увидеть этот нативный toggle в Settings → General.
			// Держим его выключенным при любых последующих изменениях, чтобы PTC/
			// Minimal и правые developer-поверхности оставались только developer/admin.
			if (!devToolsStop && typeof developerTools.subscribe === "function") {
				devToolsStop = developerTools.subscribe(() => {
					if (!roleCanUseDevTools && developerTools.getSnapshot()) {
						void developerTools.setEnabled(false).catch((e) => console.warn("[agctl] developer tools gate:", e));
					}
				});
				ctx.effect(() => () => {
					devToolsStop?.();
					devToolsStop = null;
				}, "agctl-chat-brand.role-gate");
			}
			const enabled = !!developerTools.getSnapshot();
			if (roleCanUseDevTools !== enabled) {
				await developerTools.setEnabled(roleCanUseDevTools);
			}
		}

		function apply(ctx) {
			brandCtx = ctx;
			bindWorkspaceCreate();
			// Русский composer hint — штатным locale-контрактом dsh: словарь для
			// namespace conversation + язык ru с fallback en. Прямая подмена
			// data-placeholder перезаписывалась React'ом; словарь — устойчив.
			try { installComposerLocale(ctx); } catch (e) { console.warn("[agctl] composer locale:", e); }
			// Developer Tools и правая панель доступны только developer/admin.
			void syncRoleControls(ctx).catch((e) => console.warn("[agctl] role settings:", e));
			if (window.__agctlChatBrandTimer) return;
			fixAll();
			setTimeout(fixAll, 1000);
			setTimeout(fixAll, 4000);
			window.__agctlChatBrandTimer = setInterval(fixAll, 1500);
		}

		exports.inject = ["configForms", "locale", "layout"];
		exports.apply = apply;
		// Unit-test seam: role policy is independent of the UI renderer.
		exports.__testSyncRoleControls = syncRoleControls;
		return module.exports;
	},
});
