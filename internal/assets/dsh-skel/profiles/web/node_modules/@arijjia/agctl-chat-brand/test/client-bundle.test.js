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
	assert.deepEqual(exports.inject, ["configForms", "locale", "layout"]);
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

test("role gate: user фиксирует Standard, developer/admin включают native presets", async () => {
	async function check(role, initial) {
		const exports = await loadClientFactory();
		let enabled = initial;
		const listeners = new Set();
		const developerTools = {
			getSnapshot: () => enabled,
			setEnabled: async (next) => {
				enabled = next;
				for (const listener of [...listeners]) listener();
			},
			subscribe: (fn) => { listeners.add(fn); return () => listeners.delete(fn); },
		};
		const layoutCalls = [];
		const layout = {
			openRightbar: (track, fullscreen) => layoutCalls.push(["open", track, fullscreen]),
			closeRightbar: () => layoutCalls.push(["close"]),
		};
		const ctx = {
			configForms: { developerTools: { enabled: developerTools } },
			get: (name) => name === "layout" ? layout : undefined,
			effect: (register) => register(),
		};
		const oldFetch = globalThis.fetch;
		globalThis.fetch = async () => ({ ok: true, json: async () => ({ user: { role } }) });
		try {
			await exports.__testSyncRoleControls(ctx);
			return { enabled, developerTools, layoutCalls };
		} finally {
			if (oldFetch === undefined) delete globalThis.fetch;
			else globalThis.fetch = oldFetch;
		}
	}

	const user = await check("user", true);
	assert.equal(user.enabled, false, "user получает только Standard");
	assert.deepEqual(user.layoutCalls, [["close"]], "user right pane закрывается");
	await user.developerTools.setEnabled(true); // попытка включить toggle в Settings
	assert.equal(user.enabled, false, "toggle user сразу возвращается в off");
	const developer = await check("developer", false);
	assert.equal(developer.enabled, true);
	assert.deepEqual(developer.layoutCalls, [["open", true, false]], "developer получает полный right pane");
	const admin = await check("admin", false);
	assert.equal(admin.enabled, true);
	assert.deepEqual(admin.layoutCalls, [["open", true, false]], "admin получает полный right pane");
});
