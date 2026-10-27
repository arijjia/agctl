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
	assert.deepEqual(exports.inject, ["configForms", "locale", "layout", "uiWorkspace"]);
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

test("role gate: user держит dev tools off, developer/admin открывают панель после готовности", async () => {
	async function check(role, initial) {
		const exports = await loadClientFactory();
		let enabled = initial;
		const listeners = new Set();
		const actions = [];
		const developerTools = {
			getSnapshot: () => enabled,
			subscribe: (fn) => { listeners.add(fn); return () => listeners.delete(fn); },
		};
		const developerToolsForm = {
			enabled: developerTools,
			setEnabled: async (next) => {
				actions.push(["enabled", next]);
				enabled = next;
				for (const listener of [...listeners]) listener();
			},
		};
		const layout = {
			openRightbar: (track, fullscreen) => actions.push(["open", track, fullscreen]),
			closeRightbar: () => actions.push(["close"]),
		};
		const ctx = {
			configForms: { developerTools: developerToolsForm },
			get: (name) => name === "layout" ? layout : undefined,
			effect: (register) => register(),
		};
		const oldFetch = globalThis.fetch;
		const oldDocument = globalThis.document;
		globalThis.document = { documentElement: { dataset: {} } };
		globalThis.fetch = async () => ({ ok: true, json: async () => ({ user: { role } }) });
		try {
			await exports.__testSyncRoleControls(ctx);
			return { enabled, developerToolsForm, actions, ready: globalThis.document.documentElement.dataset.agctlDeveloperToolsReady };
		} finally {
			if (oldFetch === undefined) delete globalThis.fetch;
			else globalThis.fetch = oldFetch;
			if (oldDocument === undefined) delete globalThis.document;
			else globalThis.document = oldDocument;
		}
	}

	const user = await check("user", true);
	assert.equal(user.enabled, false, "у user developer tools выключены; режим доступа задаёт role default");
	assert.deepEqual(user.actions, [["close"], ["enabled", false]], "у user правая панель схлопнута");
	assert.equal(user.ready, undefined, "для user developer-панель не раскрывается");
	await user.developerToolsForm.setEnabled(true); // попытка включить toggle в Settings
	assert.equal(user.enabled, false, "toggle user сразу возвращается в off");
	const developer = await check("developer", false);
	assert.equal(developer.enabled, true);
	assert.deepEqual(developer.actions, [["enabled", true]], "developer tools включены, нативный rightbar сам управляет видимостью содержимого");
	assert.equal(developer.ready, "1", "pre-paint схлопывание снимается после готовности панели");
	const admin = await check("admin", false);
	assert.equal(admin.enabled, true);
	assert.deepEqual(admin.actions, [["enabled", true]], "admin получает controls; пустой rightbar не открывается принудительно");
});

test("first empty chat starts one session in Default workspace", async () => {
	const exports = await loadClientFactory();
	const calls = [];
	const wsState = { phase: "ready", items: [{ workspaceId: "ws-default", path: "/workspace", title: "Default workspace" }], archivedSessionIds: [] };
	const sessionState = { phase: "ready", ids: [], byId: {} };
	const wsListeners = new Set();
	const sessionListeners = new Set();
	const uiWorkspace = {
		workspaces: { list: { getSnapshot: () => wsState, subscribe: (fn) => { wsListeners.add(fn); return () => wsListeners.delete(fn); } } },
		sessions: { list: { getSnapshot: () => sessionState, subscribe: (fn) => { sessionListeners.add(fn); return () => sessionListeners.delete(fn); } } },
		startSession: (id) => calls.push(id),
	};
	const ctx = { get: (name) => name === "uiWorkspace" ? uiWorkspace : undefined, effect: (register) => register() };
	exports.__testEnsureFirstWorkspaceSession(ctx);
	assert.deepEqual(calls, ["ws-default"], "пустой первый старт активирует штатную сессию Default workspace");
	for (const fn of [...wsListeners, ...sessionListeners]) fn();
	assert.deepEqual(calls, ["ws-default"], "на подписках не создаются дубли");
});

test("first-session bootstrap waits for readiness and skips non-empty/archived accounts", async () => {
	const exports = await loadClientFactory();
	const calls = [];
	const wsState = { phase: "loading", items: [], archivedSessionIds: [] };
	const sessionState = { phase: "loading", ids: [], byId: {} };
	const wsListeners = new Set();
	const sessionListeners = new Set();
	const uiWorkspace = {
		workspaces: { list: { getSnapshot: () => wsState, subscribe: (fn) => { wsListeners.add(fn); return () => wsListeners.delete(fn); } } },
		sessions: { list: { getSnapshot: () => sessionState, subscribe: (fn) => { sessionListeners.add(fn); return () => sessionListeners.delete(fn); } } },
		startSession: (id) => calls.push(id),
	};
	const ctx = { get: (name) => name === "uiWorkspace" ? uiWorkspace : undefined, effect: (register) => register() };
	exports.__testEnsureFirstWorkspaceSession(ctx);
	assert.deepEqual(calls, [], "до готовности списков ничего не создаётся");
	wsState.phase = "ready"; wsState.items = [{ workspaceId: "ws-default", path: "/workspace", title: "Default workspace" }];
	sessionState.phase = "ready";
	for (const fn of [...wsListeners, ...sessionListeners]) fn();
	assert.deepEqual(calls, ["ws-default"], "создание происходит после готовности обоих списков");

	for (const account of [
		{ phase: "ready", ids: ["existing"], byId: {} },
		{ phase: "ready", ids: [], byId: {} },
	]) {
		const skipped = [];
		const ctx2 = { get: (name) => name === "uiWorkspace" ? {
			workspaces: { list: { getSnapshot: () => ({ phase: "ready", items: [{ workspaceId: "ws-default", path: "/workspace", title: "Default workspace" }], archivedSessionIds: account.ids.length ? [] : ["archived"] }), subscribe: () => () => {} } },
			sessions: { list: { getSnapshot: () => account, subscribe: () => () => {} } },
			startSession: (id) => skipped.push(id),
		} : undefined, effect: (register) => register() };
		exports.__testEnsureFirstWorkspaceSession(ctx2);
		assert.deepEqual(skipped, [], "existing или archived session blocks auto-create");
	}
});
