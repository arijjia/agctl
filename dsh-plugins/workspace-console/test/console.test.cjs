const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync(require('node:path').join(__dirname, '../lib/client.js'), 'utf8');

// Загружаем клиентский бандл в песочнице: ModuleLoader не выполняем, чистые
// функции достаём из factory-замыкания с фейковыми react/slots.
function loadModule({ slots } = {}) {
	const registrations = [];
	const sandbox = {
		window: { __ModuleLoader__: { load(def) { sandbox.__def = def; } } },
		document: {
			querySelector: () => null,
			createElement: () => ({ style: {}, dataset: {}, appendChild() {}, head: null }),
			head: { appendChild() {} },
			body: { appendChild() {} },
			addEventListener() {},
		},
		fetch: async () => ({ ok: true, json: async () => ({}) }),
		require(name) {
			if (name === 'react') {
				return { useRef: () => ({}), useEffect: () => {}, createElement: () => null };
			}
			throw new Error('unexpected require: ' + name);
		},
	};
	vm.createContext(sandbox);
	vm.runInContext(source, sandbox);
	const exports = sandbox.__def.factory(sandbox.require);
	return { exports, registrations };
}

test('консоль — панель в списке панелей и вкладка main под одним ключом', () => {
	const registrations = [];
	const slots = {
		inject(slot, register) { register(); },
		register(entry) { registrations.push(entry); return entry; },
	};
	const { exports } = loadModule();
	const ctx = {
		slots,
		get(name) { return name === 'workspaces' ? { marker: 1 } : null; },
	};
	exports.apply(ctx);
	assert.equal(registrations.length, 3, 'панель + страница + кнопка скачивания');

	const row = registrations.find((r) => r.name === 'sidebar.panellist');
	const main = registrations.find((r) => r.name === 'main');
	const download = registrations.find((r) => r.name === 'deliverables.file.actions');
	assert.ok(row && main && download, 'регистрируются все три слота');
	assert.equal(row.id, exports.PANEL_ID);
	assert.equal(row.order, 20, 'после штатных Plugins(0)/Automation tasks(10), перед More');
	assert.equal(typeof row.label(), 'string');
	assert.equal(main.key, exports.PANEL_ID, 'ключ страницы = id панели');
	const injected = main.inject();
	assert.ok(injected && Object.keys(injected).length === 0, 'страница без внешних пропсов');
	assert.equal(download.id, 'agctl-workspace-console-download');
});

test('agctlDownloadUrl — путь карточки в URL скачивания, чужие пути мимо', () => {
	const { exports } = loadModule();
	const url = exports.agctlDownloadUrl;
	assert.equal(
		url('/workspace/Презенталыч/presentation/Pro_sebya.pptx'),
		'/api/projects/' + encodeURIComponent('Презенталыч') + '/file?path=' + encodeURIComponent('presentation/Pro_sebya.pptx'),
	);
	assert.equal(url('/workspace/1/plan.json'), '/api/projects/1/file?path=plan.json');
	// вложенный файл воркспейса — валиден (b/c внутри воркспейса a)
	assert.equal(url('/workspace/a/b/c'), '/api/projects/a/file?path=' + encodeURIComponent('b/c'));
	// дефолтный /workspace и чужие корни — не agctl-проекты
	assert.equal(url('/workspace'), null);
	assert.equal(url('/other/x'), null);
	assert.equal(url(undefined), null);
});

test('каталог группируется по секциям', () => {
	const { exports } = loadModule();
	const groups = exports.groupComponents([
		{ id: 'tool:lint.py', kind: 'tool' },
		{ id: 'skill:1c-vibe-demo', kind: 'skill' },
		{ id: 'dshskill:presentations', kind: 'dshskill' },
		{ id: 'data:demo-project', kind: 'data' },
		{ id: 'docs:AGENTS.md', kind: 'docs' },
	]);
	assert.deepEqual([...groups.skills.map((c) => c.id)], ['skill:1c-vibe-demo', 'dshskill:presentations']);
	assert.deepEqual([...groups.tools.map((c) => c.id)], ['tool:lint.py']);
	assert.deepEqual([...groups.data.map((c) => c.id)], ['data:demo-project']);
	assert.deepEqual([...groups.docs.map((c) => c.id)], ['docs:AGENTS.md']);
	assert.deepEqual([...exports.groupComponents(null).skills], []);
});

test('buildDisabledIDs отдаёт только изменения и пропускает locked', () => {
	const { exports } = loadModule();
	const components = [
		{ id: 'skill:1c-vibe-demo', kind: 'skill', enabled: true },
		{ id: 'skill:backlog', kind: 'skill', enabled: false },
		{ id: 'tool:lint.py', kind: 'tool', enabled: true },
		{ id: 'docs:AGENTS.md', kind: 'docs', enabled: true, locked: true },
	];
	// Всё как на диске — пустой список.
	assert.deepEqual([...exports.buildDisabledIDs(components, new Set(['skill:1c-vibe-demo', 'tool:lint.py', 'docs:AGENTS.md']))], []);
	// Выключили 1c-vibe, включили backlog — выключен остаётся только новый.
	const changed = new Set(['tool:lint.py', 'skill:backlog', 'docs:AGENTS.md']);
	assert.deepEqual([...exports.buildDisabledIDs(components, changed)], ['skill:1c-vibe-demo']);
	// Залоченный в списке выключенных не появляется никогда.
	const off = new Set(['docs:AGENTS.md', 'tool:lint.py', 'skill:1c-vibe-demo']);
	assert.equal([...exports.buildDisabledIDs(components, off)].includes('docs:AGENTS.md'), false);
});

test('deleteProject: сначала каталог (agctl), затем запись реестра dsh', async () => {
	const { exports } = loadModule();
	const calls = [];
	const api = async (path, method) => { calls.push(method + ' ' + path); return { ok: true }; };
	const project = { name: 'vibe', directory: '/workspace/vibe' };

	// сирота: записи реестра нет — только agctl-ручка
	await exports.deleteProject(api, project);
	assert.deepEqual(calls, ['DELETE /api/projects/vibe']);

	// запись есть — снимается штатным workspaces.delete тем же вызовом
	// (dshWorkspaces прокидывается через apply-контекст)
	const slots = {
		inject(slot, register) { register(); },
		register(entry) { return entry; },
	};
	const ctx = {
		slots,
		get(name) {
			return name === 'workspaces'
				? { list: { getSnapshot: () => ({ items: [{ workspaceId: 'w1', path: '/workspace/vibe' }] }) }, delete: async (id) => { calls.push('dsh.delete ' + id); } }
				: null;
		},
	};
	exports.apply(ctx);
	calls.length = 0; // часть 1 (сирота) уже проверена
	await exports.deleteProject(api, project);
	assert.deepEqual(calls, ['DELETE /api/projects/vibe', 'dsh.delete w1']);
});
