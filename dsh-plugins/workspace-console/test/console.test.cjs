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

test('консоль видна всем ролям (единое место настройки воркспейсов)', () => {
	const { exports } = loadModule();
	assert.equal(exports.shouldShowChip({ role: 'developer' }), true);
	assert.equal(exports.shouldShowChip({ role: 'admin' }), true);
	assert.equal(exports.shouldShowChip({ role: 'user' }), true);
	assert.equal(exports.shouldShowChip(null), false);
});

test('каталог группируется по секциям', () => {
	const { exports } = loadModule();
	const groups = exports.groupComponents([
		{ id: 'tool:lint.py', kind: 'tool' },
		{ id: 'skill:1c-vibe-demo', kind: 'skill' },
		{ id: 'data:demo-project', kind: 'data' },
		{ id: 'docs:AGENTS.md', kind: 'docs' },
		{ id: 'skill:backlog', kind: 'skill' },
	]);
	// Массивы приходят из vm-реалма: сравниваем локальные копии.
	assert.deepEqual([...groups.skills.map((c) => c.id)], ['skill:1c-vibe-demo', 'skill:backlog']);
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

test('чип регистрируется в штатном слоте сайдбара', () => {
	const registrations = [];
	const slots = {
		inject(slot, register) { register(); },
		register(entry) { registrations.push(entry); return entry; },
	};
	const { exports } = loadModule();
	const ctx = { slots };
	exports.apply({ slots: ctx.slots });
	assert.equal(registrations.length, 1);
	assert.equal(registrations[0].name, 'sidebar.footer.action');
	assert.equal(registrations[0].id, 'agctl-workspace-console-chip');
	assert.equal(registrations[0].order, 11);
});
