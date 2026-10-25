const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync(require('node:path').join(__dirname, '../lib/client.js'), 'utf8');
function harness(role, port = 2222, unauthorized = false) {
  let cursor = 0, page, effect, exports;
  const states = [], calls = [], registrations = [];
  const React = {
    Fragment: 'fragment',
    createElement: (type, props, ...children) => ({ type, props: props || {}, children: children.flat(Infinity) }),
    useState(initial) {
      const index = cursor++;
      if (!(index in states)) states[index] = typeof initial === 'function' ? initial() : initial;
      return [states[index], value => { states[index] = value; }];
    },
    useEffect(fn) { if (!effect) effect = fn; },
  };
  vm.runInNewContext(source, {
    window: { __ModuleLoader__: { load: mod => { exports = mod.factory(() => React); } } },
    document: { querySelector: () => true, readyState: 'loading', addEventListener() {} },
    localStorage: { getItem: () => '', setItem() {} },
    fetch: async (path, opts) => {
      calls.push({ path, ...opts });
      let data = {};
      if (path === '/api/me') data = { user: { login: 'alice', role } };
      if (path === '/api/role-requests') data = { role, requests: [{ id: 'req/1', userId: 'u1', login: 'alice', motivation: 'Build tools', status: role === 'user' ? 'rejected' : 'pending', createdAt: 'today' }] };
      if (path === '/api/projects') data = { projects: [{ name: 'demo', git: '/git/demo.git' }] };
      if (path === '/api/me/ssh') data = { pubKey: 'ssh-ed25519 public', sshPort: port };
      return { ok: !unauthorized, status: unauthorized ? 401 : 200, json: async () => data };
    },
  });
  exports.apply({ slots: { inject: (_, fn) => fn(), register: (meta, component) => { registrations.push(meta); if (meta.name === 'main') page = component; } } });
  const render = () => { cursor = 0; return page(); };
  const settle = async () => { for (let i = 0; i < 40; i++) await Promise.resolve(); };
  const nodes = tree => tree && typeof tree === 'object' ? [tree, ...tree.children.flatMap(nodes)] : [];
  return { calls, registrations, render, settle, nodes, start: async () => { render(); effect(); await settle(); return render(); } };
}
for (const role of ['user', 'developer', 'admin']) test(`v3 ${role} controls and API contracts`, async () => {
  const app = harness(role);
  let tree = await app.start();
  const all = () => app.nodes(tree);
  const text = () => JSON.stringify(tree);
  assert.match(text(), /demo/);
  assert.ok(all().some(n => n.type === 'a' && n.props.href === '/chat/'));
  assert.equal(app.calls.some(c => /workspaces|workspace-requests|local-backend|kits|config/.test(c.path)), false);
  assert.equal(app.calls.some(c => c.path === '/api/me/ssh'), role !== 'user');
  assert.equal(all().some(n => n.type === 'textarea' && n.props.required), role === 'user');
  assert.equal(all().some(n => n.type === 'button' && n.children.includes('Одобрить')), role === 'admin');
  const projectInput = all().find(n => n.props['aria-label'] === 'Имя проекта');
  projectInput.props.onChange({ target: { value: 'new-project' } }); tree = app.render();
  all().find(n => n.type === 'form' && n.children.some(c => c && c.type === 'input' && c.props['aria-label'] === 'Имя проекта')).props.onSubmit({ preventDefault() {} });
  await app.settle(); tree = app.render();
  assert.ok(app.calls.some(c => c.path === '/api/projects' && c.method === 'POST' && JSON.parse(c.body).name === 'new-project'));
  if (role === 'user') {
    all().find(n => n.type === 'textarea').props.onChange({ target: { value: 'I build tools' } }); tree = app.render();
    all().find(n => n.type === 'form' && n.children.some(c => c && c.type === 'button' && c.children.includes('Отправить заявку'))).props.onSubmit({ preventDefault() {} });
    await app.settle();
    assert.ok(app.calls.some(c => c.path === '/api/role-requests' && c.method === 'POST' && JSON.parse(c.body).motivation === 'I build tools'));
  } else {
    assert.match(text(), /ssh root@127.0.0.1 -p 2222/);
    all().find(n => n.type === 'textarea').props.onChange({ target: { value: '' } }); tree = app.render();
    all().find(n => n.type === 'form' && n.children.some(c => c && c.type === 'button' && c.children.includes('Сохранить ключ'))).props.onSubmit({ preventDefault() {} });
    await app.settle(); tree = app.render();
    assert.ok(app.calls.some(c => c.path === '/api/me/ssh' && c.method === 'PUT' && JSON.parse(c.body).pubKey === ''));
  }
  if (role === 'admin') {
    for (const [label, decision] of [['Одобрить', 'approve'], ['Отклонить', 'reject']]) {
      all().find(n => n.type === 'button' && n.children.includes(label)).props.onClick(); await app.settle(); tree = app.render();
      assert.ok(app.calls.some(c => c.path === '/api/role-requests/req%2F1/' + decision && c.method === 'POST'));
    }
  }
  assert.ok(app.calls.every(c => c.credentials === 'include'));
  assert.equal(app.registrations.length, 2);
});
test('sleeping runtime and expired session', async () => {
  const sleeping = harness('developer', 0);
  assert.match(JSON.stringify(await sleeping.start()), /Runtime спит/);
  const expired = harness('user', 0, true);
  const tree = await expired.start();
  assert.ok(expired.nodes(tree).some(n => n.type === 'input' && n.props.type === 'password'));
});
test('no legacy navigation or mode heading', () => {
  assert.doesNotMatch(source, /\/w\/|РЕЖИМЫ|\/api\/workspaces|\/api\/workspace-requests|\/api\/local-backend/);
});
