// Smoke-тест клиентского бандла chat-brand: бандл загружается через стаб
// ModuleLoader; сайдбар — нативный Workspaces dsh, кастомного рендера нет.
// Запуск: node --test "dsh-plugins/chat-brand/test/*.test.js"
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const clientPath = path.join(here, "..", "lib", "client.js");

async function loadClientFactory() {
	const src = await readFile(clientPath, "utf8");
	let captured = null;
	const loader = { load: (def) => { captured = def; } };
	// Бандл при загрузке регистрирует outside-click слушатель — нужен стаб document
	// и при вызове фабрики (слушатель вешается в её теле).
	const stubDocument = { addEventListener: () => {} };
	const oldDocument = globalThis.document;
	globalThis.document = stubDocument;
	let exports;
	try {
		// Бандл — один вызов window.__ModuleLoader__.load({...}).
		const run = new Function("window", src);
		run({ __ModuleLoader__: loader });
		assert.ok(captured, "ModuleLoader.load должен быть вызван при загрузке бандла");
		exports = captured.factory(() => ({}));
	} finally {
		if (oldDocument === undefined) delete globalThis.document;
		else globalThis.document = oldDocument;
	}
	assert.equal(captured.id, "@arijjia/agctl-chat-brand");
	return exports;
}

test("клиентский бандл загружается и объявляет apply с мягким inject", async () => {
	const exports = await loadClientFactory();
	assert.equal(typeof exports.apply, "function");
	// uiWorkspace — штатный сервис dsh (ui-workspace): через него идут
	// автостарт первой сессии и лечение застрявшего выбора воркспейса (D6).
	assert.deepEqual(exports.inject, ["configForms", "locale", "layout", "uiWorkspace"]);
	const pkg = JSON.parse(await readFile(path.join(here, "..", "package.json"), "utf8"));
	assert.ok(
		pkg.dsh.client.inject.includes("@deepseek-ai/dsh-client-ui-workspace"),
		"boot-граф обязан доставлять бандл ui-workspace до материализации плагина",
	);
});

test("нативный Workspaces-браузер dsh остаётся единственным обозревателем сессий", async () => {
	const src = await readFile(clientPath, "utf8");
	// Кастомное дерево, статусы и скрытие нативного списка демонтированы.
	assert.doesNotMatch(src, /hideNativeSessionList|renderFolderTree|folderTreeRow|folderSectionView|applyFolders|bindFolderNewSession|collectSessions|ensureFolderStyles/, "кастомный рендер сайдбара удалён");
	assert.doesNotMatch(src, /agctl-row-status|agctl-folder-section|agctl-add-folder|agctl-folders-tree/, "кастомные статусы/секции/кнопка новой папки отсутствуют");
	assert.doesNotMatch(src, /label\.textContent = 'Сессии'/, "нативная подпись Workspaces не переименовывается");
	assert.doesNotMatch(src, /style\.setProperty\(['"]display['"], ['"]none['"].*add workspace/i, "нативная кнопка Add workspace не скрывается");
	// Состояние виртуальных папок (localStorage + серверный синк) больше не читается.
	assert.doesNotMatch(src, /agctl-chat-folders\.v1|api\/chat\/folders|pendingFolder|hiddenProjects/, "устаревшее состояние папок убрано");
	// Создание воркспейса — через родную кнопку: диалог «Пустой / Из GitHub».
	assert.match(src, /bindWorkspaceCreate/, "перехват родной кнопки Add workspace");
	assert.match(src, /openWorkspaceCreateDialog/, "диалог создания на месте");
	assert.match(src, /workspaces\.create\(\{ path: directory \}\)/, "созданный каталог регистрируется штатным workspaces.create");
	assert.match(src, /if \(cloneUrl\) body\.cloneUrl = cloneUrl/, "clone URL из диалога уходит в projects API");
	assert.match(src, /\['developer', 'admin'\]\.includes\(meData\.user\.role\)/, "GitHub clone доступен только developer/admin");
	assert.match(src, /fetch\('\/api\/projects'/, "каталог создаётся серверным API");
	assert.match(src, /font-size:13px;line-height:18px/, "кнопки в create-workspace диалоге уменьшены");
	// Composer hint остаётся на штатном locale-контракте.
	assert.match(src, /installComposerLocale/, "русский hint через locale dsh на месте");
	// Бренд/модели живут в fixAll; developerTools гейтится по GET /api/me.
	assert.match(src, /applyBrand/, "бренд на месте");
	assert.match(src, /fixModels/, "скрытие группы DeepSeek на месте");
	assert.match(src, /syncRoleControls/, "developerTools включается только developer/admin");
	assert.doesNotMatch(src, /hideDevPanels/, "клиент больше не схлопывает правую панель вслепую");
});

test("бренд приходит из профиля организации, не зашит в бандл", async () => {
	const src = await readFile(clientPath, "utf8");
	// Бренд приходит из профиля организации, не зашит в бандл; литералы
	// организаций в ядре ловит Go-guard (internal/profile/guard_test.go).
	assert.match(src, /window\.__AGCTL_BRAND__/, "бренд читается из window.__AGCTL_BRAND__");
	assert.match(src, /BRAND_CFG\.name \|\| "agctl"/, "фолбэк имени — нейтральный agctl");
	assert.match(src, /BRAND_CFG\.logo \|\| ""/, "логотип — из профиля, не из бандла");
	assert.match(src, /makeMark/, "марка строится из логотипа или буквы");
	assert.doesNotMatch(src, /data:image\/png;base64,iVBOR/, "base64-логотип не вшит в бандл");
});

test("composer hint — штатный locale dsh, не DOM-патч", async () => {
	const src = await readFile(clientPath, "utf8");
	assert.match(src, /installComposerLocale/, "локаль ставится отдельной функцией");
	assert.match(src, /addLanguage\(\{ id: "ru", label: "Русский", fallback: "en" \}\)/, "ru добавлен как язык dsh с fallback en");
	assert.match(src, /locale\.register\("conversation", "ru", COMPOSER_RU\)/, "словарь conversation зарегистрирован для ru");
	assert.match(src, /"placeholder\.default": "Напишите сообщение/, "открытая сессия: русский placeholder.default");
	assert.match(src, /"placeholder\.hero": "Опишите/, "пустой чат: русский placeholder.hero");
	assert.match(src, /explicitPreference == null\)\s*locale\.setLocale\("ru"\)/s, "явный выбор языка пользователя не перетирается");
	assert.doesNotMatch(src, /setAttribute\('data-placeholder'|setAttribute\('placeholder'/, "прямые DOM-патчи placeholder убраны");
	assert.doesNotMatch(src, /Message or run a task/, "английский текст открытой сессии больше не патчится");
});

test("role gate: user фиксирует Standard, developer/admin оставляют нативный режим", async () => {
	async function check(role, initial) {
		const exports = await loadClientFactory();
		let enabled = initial;
		const listeners = new Set();
		// Форма dsh 0.2.1-rc.2: setEnabled живёт на самой preference,
		// а .enabled — только observable (getSnapshot/subscribe).
		const enabledStore = {
			getSnapshot: () => enabled,
			subscribe: (fn) => { listeners.add(fn); return () => listeners.delete(fn); },
		};
		let setEnabledCalls = [];
		const preference = {
			enabled: enabledStore,
			setEnabled: async (next) => {
				enabled = next;
				setEnabledCalls.push(next);
				for (const listener of [...listeners]) listener();
			},
		};
		const layoutCalls = [];
		const layout = {
			openRightbar: (track, fullscreen) => layoutCalls.push(["open", track, fullscreen]),
			closeRightbar: () => layoutCalls.push(["close"]),
		};
		const ctx = {
			configForms: { developerTools: preference },
			get: (name) => name === "layout" ? layout : undefined,
			effect: (register) => register(),
		};
		const oldFetch = globalThis.fetch;
		globalThis.fetch = async () => ({ ok: true, json: async () => ({ user: { role } }) });
		// D8: syncRoleControls снимает pre-paint схлопывание правой колонки
		// флагом на documentElement — стабилизируем DOM-часть.
		const html = { dataset: {} };
		const oldDocument = globalThis.document;
		globalThis.document = { documentElement: html };
		const oldRAF = globalThis.requestAnimationFrame;
		globalThis.requestAnimationFrame = (fn) => fn();
		try {
			await exports.__testSyncRoleControls(ctx);
			return { enabled, preference, layoutCalls, setEnabledCalls, readyFlag: html.dataset.agctlDeveloperToolsReady === "1" };
		} finally {
			if (oldFetch === undefined) delete globalThis.fetch;
			else globalThis.fetch = oldFetch;
			if (oldDocument === undefined) delete globalThis.document;
			else globalThis.document = oldDocument;
			if (oldRAF === undefined) delete globalThis.requestAnimationFrame;
			else globalThis.requestAnimationFrame = oldRAF;
		}
	}

	const user = await check("user", true);
	assert.equal(user.enabled, false, "user получает только Standard");
	assert.deepEqual(user.layoutCalls, [["close"]], "user right pane закрывается");
	assert.deepEqual(user.setEnabledCalls, [false], "setEnabled зовётся на preference, не на observable");
	assert.equal(user.readyFlag, false, "user не снимает pre-paint схлопывание (панели у него нет)");
	await user.preference.setEnabled(true); // попытка включить toggle в Settings
	assert.equal(user.enabled, false, "toggle user сразу возвращается в off");
	const developer = await check("developer", false);
	assert.equal(developer.enabled, true);
	assert.equal(developer.readyFlag, true, "developer снимает pre-paint схлопывание флагом готовности");
	// D8: панель developer не открываем принудительно — видимость решает
	// нативный DSH rightbar, иначе пустая колонка мигает до готовности.
	assert.deepEqual(developer.layoutCalls, [], "developer right pane не трогается из плагина");
	const admin = await check("admin", false);
	assert.equal(admin.enabled, true);
	assert.deepEqual(admin.layoutCalls, [], "admin right pane не трогается из плагина");
});

// ── D6: пунктирный composer «Выберите воркспейс» ────────────────────────────

function stubHeroDom({ editable, session, chipText, chipOpen, menu }) {
	const composer = {
		attrs: { "data-composer-input": "true", contenteditable: editable ? "true" : "false" },
		getAttribute: (name) => composer.attrs[name] ?? null,
	};
	const content = {
		attrs: { "data-conversation-content": "", "data-conversation-session": session || "" },
		getAttribute: (name) => content.attrs[name] ?? null,
	};
	const chip = {
		labels: ["Choose workspace".replace("Choose workspace", chipText || "Choose workspace")],
		textContent: chipText || "Choose workspace",
		expanded: !!chipOpen,
		getAttribute: (name) => (name === "aria-label" ? "Choose workspace" : name === "aria-expanded" ? (chip.expanded ? "true" : "false") : name === "aria-haspopup" ? "menu" : null),
	};
	const oldDocument = globalThis.document;
	globalThis.document = {
		querySelector: (selector) => (selector === "[data-composer-input]" ? composer : selector === "[data-conversation-content]" ? content : null),
		querySelectorAll: (selector) => (selector === 'button[aria-haspopup="menu"]' ? [chip] : selector === '[role="menu"]' ? (menu ? [{}] : []) : []),
	};
	return () => {
		if (oldDocument === undefined) delete globalThis.document;
		else globalThis.document = oldDocument;
	};
}

test("D6: застрявший выбор воркспейса доезжает до startSession на втором тике", async () => {
	const exports = await loadClientFactory();
	let startSessionCalls = 0;
	const ctx = {
		get: (name) => (name === "uiWorkspace" ? { startSession: () => { startSessionCalls += 1; } } : undefined),
		effect: (register) => register(),
	};
	const restoreDom = stubHeroDom({ editable: false, session: "", chipText: "1", chipOpen: false, menu: false });
	try {
		exports.__testHealStuckWorkspacePick(ctx);
		assert.equal(startSessionCalls, 0, "первый тик — наблюдение, не воюем с навигацией в полёте");
		exports.__testHealStuckWorkspacePick(ctx);
		assert.equal(startSessionCalls, 1, "второй подряд тик — штатный startSession(недавний воркспейс)");
	} finally {
		restoreDom();
	}
});

test("D6: живые состояния композера лечению не подлежат", async () => {
	const exports = await loadClientFactory();
	let startSessionCalls = 0;
	const ctx = {
		get: (name) => (name === "uiWorkspace" ? { startSession: () => { startSessionCalls += 1; } } : undefined),
		effect: (register) => register(),
	};
	const cases = [
		{ why: "композер активен (сессия открыта)", dom: { editable: true, session: "s1", chipText: "1", chipOpen: false, menu: false } },
		{ why: "hero без выбора — штатный пустой чат", dom: { editable: false, session: "", chipText: "Choose workspace", chipOpen: false, menu: false } },
		{ why: "меню выбора открыто — пользователь выбирает", dom: { editable: false, session: "", chipText: "1", chipOpen: true, menu: true } },
	];
	for (const { why, dom } of cases) {
		const restoreDom = stubHeroDom(dom);
		try {
			for (let tick = 0; tick < 3; tick++) exports.__testHealStuckWorkspacePick(ctx);
			assert.equal(startSessionCalls, 0, why);
		} finally {
			restoreDom();
		}
	}
});

test("D6: автостарт первой сессии объявлен и звёт штатный startSession", async () => {
	const src = await readFile(clientPath, "utf8");
	assert.match(src, /ensureFirstWorkspaceSession/, "автостарт первой сессии на месте");
	assert.match(src, /healStuckWorkspacePick/, "лечение застрявшего выбора на месте");
	assert.match(src, /item\.path === "\/workspace" \|\| item\.title === "Default workspace"/, "первая сессия — Default workspace платформы");
	assert.match(src, /workspaces\.phase !== "ready" \|\| sessions\.phase !== "ready"/, "автостарт ждёт готовности списков");
	assert.match(src, /\(sessions\.ids \|\| \[\]\)\.length > 0 \|\| \(workspaces\.archivedSessionIds \|\| \[\]\)\.length > 0/, "автостарт не срабатывает, когда сессии уже есть");
	assert.match(src, /uiWorkspace\.startSession\(\); \/\/ без аргумента/, "лечение идёт тем же штатным переходом, что у «+» у папки");
});
