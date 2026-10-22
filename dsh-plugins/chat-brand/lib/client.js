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
			// Close it if open, then remove both affordances in Chat mode.
			const rightButtons = queryAllDeep('button[aria-label*="right sidebar"]');
			const collapse = rightButtons.find((b) => /collapse/i.test(b.getAttribute('aria-label') || ''));
			if (collapse && collapse.offsetParent !== null && !collapse.dataset.agctlClosed) {
				collapse.dataset.agctlClosed = '1';
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
			for (const control of queryAllDeep('button,[role=tab]')) {
				const label = `${control.getAttribute('aria-label') || ''} ${control.getAttribute('title') || ''} ${control.textContent || ''}`.trim();
				if (/workspace files|new terminal|choose workspace|choose shell|access mode|add files or run commands|trajectory/i.test(label)) {
					control.style.setProperty('display', 'none', 'important');
					control.setAttribute('aria-hidden', 'true');
					control.setAttribute('tabindex', '-1');
				}
			}
		}

		function fixAll() {
			try { closePreviewNotice(); } catch (e) {}
			try { applyBrand(); } catch (e) {}
			try { fixPlaceholder(); } catch (e) {}
			try { fixModels(); } catch (e) {}
			try { flattenSessions(); } catch (e) {}
			try { hideDevPanels(); } catch (e) {}
		}

		function apply() {
			if (window.__agctlChatBrandTimer) return;
			fixAll();
			setTimeout(fixAll, 1000);
			setTimeout(fixAll, 4000);
			window.__agctlChatBrandTimer = setInterval(fixAll, 1500);
		}

		exports.inject = [];
		exports.apply = apply;
		return module.exports;
	},
});
