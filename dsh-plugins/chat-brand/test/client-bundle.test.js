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
	// Дерево-аккордеон вместо чипсов.
	assert.match(src, /data-agctl-folders-tree/);
	assert.match(src, /agctl-folder-section/);
	assert.match(src, /Без папки/);
	// Чипсов больше нет: создание чипс-контейнера не должно вернуться.
	assert.doesNotMatch(src, /chips\.dataset\.agctlFolders/);
});

test("серверный контракт папок не расширен, синк инструкций на месте", async () => {
	const src = await readFile(clientPath, "utf8");
	assert.match(src, /"agctl-chat-folders\.v1"/, "ключ state не меняется — это контракт миграции");
	assert.match(src, /api\/chat\/folders/, "GET/PUT синк с сервером на месте");
	assert.match(src, /folder-instructions/, "PATCH инструкций AGENTS.local.md на месте");
	assert.match(src, /pendingFolder/, "автопривязка новых сессий на месте");
	// UI-состояние дерева живёт отдельно в localStorage.
	assert.match(src, /agctl-chat-folders-ui\.v1/);
});
