const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync(require('node:path').join(__dirname, '../lib/client.js'), 'utf8');

function makeEl(tag) {
  const node = {
    tag, children: [], dataset: {}, style: {}, attributes: {}, value: '', textContent: '', disabled: false,
    appendChild(child) {
      if (child.parentElement) child.parentElement.removeChild(child);
      child.parentElement = this;
      this.children.push(child);
      return child;
    },
    prepend(child) { child.parentElement = this; this.children.unshift(child); },
    append(...kids) { for (const k of kids) { k.parentElement = this; this.children.push(k); } },
    removeChild(child) {
      const i = this.children.indexOf(child);
      if (i >= 0) { this.children.splice(i, 1); child.parentElement = null; }
    },
    remove() { if (this.parentElement) this.parentElement.removeChild(this); },
    addEventListener(type, fn) { (this.listeners ||= {})[type] = fn; },
    setAttribute(k, v) { this.attributes[k] = v; },
    getAttribute(k) { return this.attributes[k] ?? null; },
    contains(target) {
      for (let cur = target; cur; cur = cur.parentElement) if (cur === this) return true;
      return false;
    },
    querySelector(sel) { return sel === 'span' ? this.children.find((c) => c.tag === 'span') || null : null; },
    querySelectorAll(sel) { return sel === 'span' ? this.children.filter((c) => c.tag === 'span') : []; },
    isConnected: true,
    parentElement: null,
  };
  return node;
}

function harness({ me, unauthorized = false } = {}) {
  const calls = [];
  const registrations = [];
  const slots = {
    inject(key, register) {
      registrations.push({ key, entry: register() });
    },
    register(entry, component) {
      return { ...entry, children: component };
    },
  };
  const documentStub = {
    readyState: 'complete',
    body: makeEl('body'),
    querySelector() { return null; },
    querySelectorAll() { return []; },
    createElement: makeEl,
    addEventListener() {},
    removeEventListener() {},
    head: makeEl('head'),
  };
  let exports;
  const modules = { react: { createElement: (t, p) => ({ tag: t, props: p }), useRef: () => ({ current: null }), useEffect: () => {} } };
  const windowStub = { __ModuleLoader__: { load: (def) => { exports = def.factory((name) => modules[name]); } } };
  const ctx = {
    slots,
    get(name) {
      if (name === 'sessions') {
        return { list: { getSnapshot: () => ({ ids: ['s1'], byId: { s1: { updatedAt: Date.now() } } }) } };
      }
      return undefined;
    },
  };
  vm.runInNewContext(source, {
    window: windowStub,
    document: documentStub,
    fetch: async (path) => {
      calls.push(path);
      if (unauthorized && path === '/api/me') return { ok: false, status: 401, json: async () => ({}) };
      const data = { '/api/me': { user: { login: 'masha', role: me || 'user' } } }[path] || {};
      return { ok: true, status: 200, json: async () => data };
    },
  });
  exports.apply(ctx);
  return { calls, registrations, exports, slots };
}

test('чип регистрируется в нативном слоте sidebar.footer.action', async () => {
  const { registrations, exports } = harness({ me: 'admin' });
  assert.equal(registrations.length, 1, 'ровно одна слот-регистрация');
  assert.equal(registrations[0].key, 'sidebar.footer.action');
  assert.equal(registrations[0].entry.name, 'sidebar.footer.action');
  assert.equal(registrations[0].entry.id, 'agctl-profile-chip');
  assert.equal(typeof registrations[0].entry.children, 'function', 'слот получает React-компонент чипа');
  assert.equal(exports.inject.join(','), 'slots', 'плагин запрашивает только слоты');
});

test('чип повторяет метрики нативного Settings-ряда', async () => {
  const { exports } = harness({ me: 'admin' });
  const user = await exports.ensureAgctlUser();
  assert.equal(user.login, 'masha');
  const container = makeEl('div');
  const chip = exports.renderChipInto(container, user, true);

  // Ряд = VOzbGW_triggerRow: полная ширина панели, блик -2px — левая граница
  // совпадает с кнопкой Settings, а не правее неё.
  assert.equal(container.style.width, 'calc(100% + 4px)');
  assert.equal(container.style.margin, '4px -2px');

  // Кнопка = VOzbGW_trigger: flex-ряд (иначе button рендерит детей в столбик),
  // высота 42px, паддинг 0 10px 0 8px, 14/22.
  assert.equal(chip.style.display, 'flex');
  assert.equal(chip.style.alignItems, 'center');
  assert.equal(chip.style.height, '42px');
  assert.equal(chip.style.padding, '0 10px 0 8px');
  assert.equal(chip.style.fontSize, '14px');
  assert.equal(chip.style.borderRadius, 'var(--dsw-radius-md, 10px)');

  const spans = chip.querySelectorAll('span');
  assert.equal(spans[0].textContent, 'MA');
  assert.equal(spans[1].textContent, 'masha');
  assert.equal(spans[1].style.display, 'block');

  // Рельса = VOzbGW_railRow/rail: 36px ряд и 36×36 кнопка, имя скрыто.
  exports.renderChipInto(container, user, false);
  assert.equal(container.style.width, '36px');
  assert.equal(container.style.margin, '8px 0 10px');
  assert.equal(chip.style.width, '36px');
  assert.equal(chip.style.height, '36px');
  assert.equal(spans[1].style.display, 'none');
});

test('повторный рендер переиспользует один DOM-узел чипа', async () => {
  const { exports } = harness({ me: 'user' });
  const user = await exports.ensureAgctlUser();
  const first = makeEl('div');
  const second = makeEl('div');
  const a = exports.renderChipInto(first, user, true);
  const b = exports.renderChipInto(second, user, true);
  assert.equal(a, b, 'chip node переиспользуется между монтированиями');
  assert.equal(first.children.includes(a), false, 'узел переехал из первого контейнера');
  assert.equal(second.children.includes(b), true);
});

test('без авторизации пользователь не резолвится и чип не рендерится', async () => {
  const { exports } = harness({ unauthorized: true });
  const user = await exports.ensureAgctlUser();
  assert.equal(user, null, '401 не даёт пользователя');
});

test('нет панели «Кабинет», управления рантаймом и списка воркспейсов', () => {
  assert.doesNotMatch(source, /sidebar\.panellist|renderSlot\(|slots\.inject\("sidebar\.settings"\)/, 'панель Кабинета и settings-затенение отсутствуют');
  assert.match(source, /slots\.inject\("sidebar\.footer\.action"/, 'чип живёт в штатном footer.action слоте');
  assert.doesNotMatch(source, /\/api\/me\/runtime\/(start|stop|logs)/, 'кнопки управления рантаймом удалены');
  assert.doesNotMatch(source, /Добавить воркспейс|Имя воркспейса/, 'создание воркспейсов переехало в родную кнопку сайдбара');
  assert.match(source, /api\/me\/runtime/, 'только read-only статус в профиле');
  assert.match(source, /projectionValues[\s\S]*tokenUsage/, 'статистика берёт provider-reported DSH token projections');
  for (const key of ['uncachedInputTokens', 'cacheReadTokens', 'cacheWriteTokens', 'outputTokens']) {
    assert.match(source, new RegExp(key), `профиль показывает ${key}`);
  }
  assert.match(source, /Активность сессий за год/);
  assert.match(source, /не токены\/день/, 'heatmap не выдаётся за токены по дням');
  assert.match(source, /Права доступа/);
  assert.match(source, /roleDescription/, 'роль сопровождается описанием прав');
  assert.match(source, /Администратор/);
  assert.match(source, /Разработчик/);
  assert.match(source, /Пользователь/);
  assert.match(source, /Профиль/, 'пункт профиля в плашке');
  assert.match(source, /Справка/, 'справка на месте');
  assert.doesNotMatch(source, /item\("Настройки"/, 'Settings остаётся отдельной нативной кнопкой, не в popup');
});
