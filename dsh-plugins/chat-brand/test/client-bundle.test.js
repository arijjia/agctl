// Smoke-тест клиентского бандла chat-brand: бандл загружается через стаб
// ModuleLoader и сохраняет контракты дерева папок.
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
	assert.deepEqual(exports.inject, ["configForms"]);
});

test("открытие сессии идёт через uiWorkspace, нативный список только скрывается", async () => {
	const src = await readFile(clientPath, "utf8");
	// Прямой официальный механизм клиента dsh: ровно тот же метод, что у родной
	// строки; резолв сервиса — мягкий, через ctx.get в момент вызова.
	assert.match(src, /svc\("uiWorkspace"\)/, "сервис uiWorkspace резолвится через ctx.get");
	assert.match(src, /\.openSession\(id\)/, "openSessionRow обязан звать uiWorkspace.openSession");
	assert.match(src, /\.startSession\(/, "«＋» на папке обязан создавать сессию штатным startSession");
	// Нативное дерево прячется, но не удаляется (фолбэк-клик по скрытой строке).
	assert.match(src, /function hideNativeSessionList/);
	assert.doesNotMatch(src, /\.remove\(\)[^\n]*YDXeBa_sessionRow|YDXeBa_sessionRow[^\n]*\.remove\(\)/);
	// Плоский режим dsh рендерит [role=list] — без скрытия строки протекают.
	assert.match(src, /\[role=tree\], \[role=list\]/, "прячется и плоский нативный список");
	// Дерево-аккордеон вместо чипсов.
	assert.match(src, /data-agctl-folders-tree/);
	assert.match(src, /agctl-folder-section/);
	assert.match(src, /Без папки/);
	// Чипсов больше нет: создание чипс-контейнера не должно вернуться.
	assert.doesNotMatch(src, /chips\.dataset\.agctlFolders/);
});

test("сайдбар выглядит как родные Workspaces dsh: иконки папок, статусы, свёрнутая панель", async () => {
	const src = await readFile(clientPath, "utf8");
	assert.match(src, /AGCTL_FOLDER_PATH/, "папки — родная иконка папки, не цветные точки");
	assert.doesNotMatch(src, /agctl-folder-dot/, "цветные точки папок убраны");
	assert.doesNotMatch(src, /folderColor/, "раскраска папок хэшем больше не нужна");
	assert.match(src, /agctl-row-status/, "у сессии статус-индикатор слева от названия");
	assert.match(src, /agctl-row-status.*agctl-running/s, "во время работы — спиннер");
	assert.match(src, /@keyframes agctl-spin/, "спиннер анимирован как в dsh");
	// Сессии — без иконок: статус + название + время.
	assert.match(src, /status\.className = "agctl-row-status"/);
	// Свёрнутая панель: дерево прячется целиком, не ломая рельсу.
	assert.match(src, /getBoundingClientRect\(\)\.width < 120/, "свёрнутая рельса скрывает дерево");
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

test("папка соответствует реальному project Workspace и сохраняет инструкции в каталоге", async () => {
	const src = await readFile(clientPath, "utf8");
	assert.match(src, /"agctl-chat-folders\.v1"/, "серверное состояние папок сохраняется совместимым");
	assert.match(src, /api\/chat\/folders/, "GET/PUT синк папок на месте");
	assert.match(src, /api\/projects/, "папка создаёт/находит реальный проектный каталог");
	assert.match(src, /workspaces\.create\(\{ path: project\.directory \}\)/, "проект регистрируется настоящим DSH Workspace");
	assert.match(src, /ui\.startSession\(project\.workspaceId\)/, "новая сессия стартует в выбранном каталоге проекта");
	assert.match(src, /api\/projects\/.*instructions/, "AGENTS.local.md сохраняется в корне проекта");
	assert.match(src, /pendingFolder/, "автопривязка сессии к проектной папке на месте");
	assert.match(src, /hiddenProjects/, "скрытие проекта не теряет каталог и Git-историю");
});
