import { chromium } from 'playwright';

// v3 cabinet UX, browser-level: register a plain user -> personal DSH chat
// (branding from the active profile, dev surfaces hidden for the user role)
// -> profile chip popup (Профиль/Справка/Выйти, no SSH for user) -> developer
// role upgrade -> the same popup now offers SSH-доступ -> Профиль overlay and
// SSH overlay (saved-key field + live port line).
//
// Brand assertions are profile-driven: expected strings come from
// window.__AGCTL_BRAND__ injected by the gateway, so the scenario runs on the
// neutral agctl profile and on organization profiles alike (no hardcoded
// «ТюмГУ…»). Workspace-request flow is gone in v3 (handlers answer 410) —
// the developer path is covered by developer-flow-ui.mjs.

const required = [
  'PLATFORM_URL', 'E2E_LOGIN', 'E2E_PASSWORD',
  'E2E_ADMIN_LOGIN', 'E2E_ADMIN_PASSWORD', 'E2E_LLM_BASE_URL',
];
for (const name of required) {
  if (!process.env[name]) throw new Error(`missing environment variable ${name}`);
}

// Same-origin API call from the page: keeps cookies of the logged-in context.
async function api(page, path, method = 'GET', body = null) {
  return page.evaluate(async ({ path, method, body }) => {
    const res = await fetch(path, {
      method,
      credentials: 'include',
      headers: { Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
      body: body ? JSON.stringify(body) : undefined,
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(`${method} ${path}: HTTP ${res.status} ${data.error || ''}`);
    return data;
  }, { path, method, body });
}

const browser = await chromium.launch({ headless: true });
const errors = [];
const screenshot = async (page) => {
  if (process.env.E2E_SCREENSHOT) await page.screenshot({ path: process.env.E2E_SCREENSHOT, fullPage: true }).catch(() => {});
};

let userPage = null;
try {
  // ── user: register through the SPA (second account => role user) ──────────
  const userContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const user = await userContext.newPage();
  userPage = user;
  user.on('pageerror', (error) => errors.push('user: ' + error.message));
  await user.goto(process.env.PLATFORM_URL, { waitUntil: 'domcontentloaded' });
  await user.getByRole('button', { name: 'Регистрация' }).waitFor({ state: 'visible', timeout: 15000 });
  await user.getByRole('button', { name: 'Регистрация' }).click();
  await user.getByLabel('Логин').fill(process.env.E2E_LOGIN);
  await user.getByLabel('Пароль').fill(process.env.E2E_PASSWORD);
  await user.getByRole('button', { name: 'Создать аккаунт' }).click();
  await user.waitForURL((url) => url.pathname === '/chat/', { timeout: 15000 });
  console.log('PASS: registration on platform root redirects straight to /chat/');

  // The personal DSH runtime boots lazily; the cabinet chip appears when the
  // web shell is ready. The chip is the v3 cabinet entry (no «Кабинет» panel).
  const chip = user.locator('[data-agctl-cabinet-chip]');
  let dshReady = false;
  for (let i = 0; i < 45; i++) {
    if (await chip.isVisible().catch(() => false)) { dshReady = true; break; }
    await user.goto(`${process.env.PLATFORM_URL.replace(/\/$/, '')}/chat/`, { waitUntil: 'domcontentloaded' }).catch(() => {});
    await user.waitForTimeout(500);
  }
  if (!dshReady) throw new Error('personal DSH chat did not become ready at /chat/');

  // dsh first-run dialogs are local to the persistent dsh-home: preview
  // notice (dsh 0.2.2, RU/EN) and the API-key modal (server LLM defaults
  // normally suppress it; the guard stays for isolated re-used homes).
  for (const name of ['Продолжить', 'Continue']) {
    const button = user.getByRole('button', { name, exact: true }).first();
    if (await button.isVisible().catch(() => false)) {
      await button.click();
      await button.waitFor({ state: 'hidden', timeout: 10000 }).catch(() => {});
    }
  }
  const continueButton = user.getByRole('button', { name: 'Continue' });
  if (await continueButton.count()) await continueButton.dispatchEvent('click');
  const apiKeyInput = user.getByPlaceholder('Enter your API key');
  try {
    await apiKeyInput.waitFor({ state: 'visible', timeout: 2500 });
    await apiKeyInput.fill('sk-playwright-dummy-key');
    await user.getByRole('button', { name: 'Save and continue' }).dispatchEvent('click');
    await user.getByRole('button', { name: 'Configure later' }).waitFor({ state: 'hidden', timeout: 10000 });
  } catch { /* isolated test dsh-home may already be configured */ }
  if (await user.getByText('Add an API key to get started').isVisible().catch(() => false)) {
    throw new Error('platform default LLM did not suppress the first-run API-key modal');
  }

  // ── Chat-mode branding, driven by the active profile ──────────────────────
  const brand = await user.evaluate(() => window.__AGCTL_BRAND__ || {});
  const brandName = brand.name || 'agctl';
  const tagline = brand.tagline || 'агентские рабочие места';
  await user.locator('[data-agctl-brand-title]').first().waitFor({ state: 'visible', timeout: 15000 });
  const shownTitle = (await user.locator('[data-agctl-brand-title]').first().textContent()).trim();
  if (shownTitle !== brandName) throw new Error(`brand title shows "${shownTitle}", profile says "${brandName}"`);
  if (await user.locator('svg[viewBox="0 0 23.16 17.04"]').count() !== 0) {
    throw new Error('DeepSeek whale mark remains in Chat mode');
  }
  if (brand.logo) {
    if (await user.locator('img[data-agctl-logo]').count() === 0) throw new Error('profile logo image missing');
  } else {
    const mark = user.locator('span[data-agctl-mark]').first();
    await mark.waitFor({ state: 'visible', timeout: 10000 });
    const letter = (await mark.textContent()).trim();
    if (letter !== (brand.letter || brandName[0])) throw new Error(`brand mark letter "${letter}" != profile letter`);
  }
  await user.getByText(tagline, { exact: true }).first().waitFor({ state: 'visible', timeout: 10000 });
  console.log(`PASS: Chat mode branding follows the profile (name="${brandName}", tagline="${tagline}")`);

  // ── user role: no dev surfaces, chip popup without SSH ────────────────────
  const me = await api(user, '/api/me');
  if (me.user.role !== 'user') throw new Error(`expected role user, got ${me.user.role}`);

  const visibleDevButtons = await user.evaluate(() => {
    const devLabel = /^(new terminal|новый терминал|access mode|режим доступа|workspace files|файлы воркспейса)/i;
    return [...document.querySelectorAll('button[aria-label]')]
      .filter((b) => devLabel.test(b.getAttribute('aria-label')) && !!b.offsetParent)
      .map((b) => b.getAttribute('aria-label'));
  });
  if (visibleDevButtons.length) throw new Error(`user chat exposes dev controls: ${visibleDevButtons.join(', ')}`);
  const guideDoors = await user.evaluate(() => {
    const doors = document.querySelectorAll('[data-sidebar-right-guide-entry]');
    return [...doors].filter((d) => !!d.offsetParent).map((d) => d.dataset.sidebarRightGuideEntry);
  });
  if (guideDoors.some((kind) => kind === 'terminal' || kind === 'files')) {
    throw new Error(`user chat shows right-panel dev doors: ${guideDoors.join(', ')}`);
  }
  console.log('PASS: user role hides development controls in Chat mode');

  await chip.click();
  const popup = user.locator('[data-agctl-cabinet-popup]');
  await popup.waitFor({ state: 'visible', timeout: 10000 });
  const itemLabels = await popup.getByRole('button').evaluateAll((buttons) => buttons.map((b) => b.textContent.trim()));
  for (const label of ['Профиль', 'Справка', 'Выйти']) {
    if (!itemLabels.includes(label)) throw new Error(`chip popup is missing the "${label}" item: ${itemLabels.join(', ')}`);
  }
  if (itemLabels.includes('SSH-доступ')) throw new Error('chip popup offers SSH-доступ to the user role');
  await user.mouse.click(720, 200); // dismiss outside the popup
  await popup.waitFor({ state: 'hidden', timeout: 5000 });
  console.log('PASS: chip popup for user role: Профиль/Справка/Выйти and no SSH item');

  // ── Chat mode: session + prompt round-trip through the fake LLM ───────────
  const newSession = user.locator('button[aria-label="Новая сессия"], button[aria-label="New session"]').first();
  await newSession.click();
  const chatComposer = user.locator('[data-composer-input="true"]');
  await chatComposer.waitFor({ state: 'visible', timeout: 10000 });
  await user.waitForFunction(() => document.querySelector('[data-composer-input="true"]')?.getAttribute('contenteditable') === 'true', { timeout: 10000 });
  await chatComposer.fill('Проверь чат платформы');
  await chatComposer.press('Enter');
  await user.getByText('FAKE-LLM-REPLY: всё работает', { exact: false }).last().waitFor({ state: 'visible', timeout: 20000 });
  console.log('PASS: Chat mode New Session and prompt round-trip work');

  // ── developer upgrade: role request -> admin approves ─────────────────────
  const request = await api(user, '/api/role-requests', 'POST', { motivation: 'e2e: нужен SSH-доступ в кабинете' });
  if (request.request.status !== 'pending') throw new Error('role request is not pending');

  const adminContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const admin = await adminContext.newPage();
  admin.on('pageerror', (error) => errors.push('admin: ' + error.message));
  await admin.goto(process.env.PLATFORM_URL, { waitUntil: 'domcontentloaded' });
  await admin.getByLabel('Логин').fill(process.env.E2E_ADMIN_LOGIN);
  await admin.getByLabel('Пароль').fill(process.env.E2E_ADMIN_PASSWORD);
  await admin.getByRole('button', { name: 'Войти' }).click();
  await admin.waitForURL((url) => url.pathname.startsWith('/admin') || url.pathname.startsWith('/chat'), { timeout: 15000 });
  const queue = await api(admin, '/api/role-requests');
  const pending = queue.requests.filter((r) => r.login === process.env.E2E_LOGIN && r.status === 'pending');
  if (pending.length !== 1) throw new Error(`expected 1 pending role request, got ${pending.length}`);
  await api(admin, `/api/role-requests/${pending[0].id}/approve`, 'POST');
  console.log('PASS: admin approves the developer-role request');

  // ── developer: popup gains SSH-доступ; Профиль overlay ────────────────────
  // The chip caches /api/me per page load, and the chat shell is injected by
  // role — a fresh load turns the account into a developer everywhere.
  await user.reload({ waitUntil: 'domcontentloaded' });
  await chip.waitFor({ state: 'visible', timeout: 30000 });
  const meDev = await api(user, '/api/me');
  if (meDev.user.role !== 'developer') throw new Error(`expected role developer after approval, got ${meDev.user.role}`);

  await chip.click();
  const sshItem = user.getByRole('button', { name: 'SSH-доступ', exact: true });
  await sshItem.waitFor({ state: 'visible', timeout: 10000 });
  console.log('PASS: chip popup for developer shows the SSH-доступ item');

  await user.getByRole('button', { name: 'Профиль', exact: true }).click();
  await user.getByText(`@${process.env.E2E_LOGIN}`, { exact: true }).waitFor({ state: 'visible', timeout: 10000 });
  await user.getByText('Разработчик', { exact: true }).first().waitFor({ state: 'visible', timeout: 10000 });
  await user.getByText('Использование', { exact: true }).first().waitFor({ state: 'visible', timeout: 10000 });
  await user.locator('button[aria-label="Закрыть"]').first().click();
  console.log('PASS: Профиль overlay shows the developer role badge and usage stats');

  // ── developer: SSH overlay with key field and live port line ──────────────
  // The runtime is up (the chat booted), so sshd's loopback port must appear.
  let port = 0;
  for (let i = 0; i < 60; i++) {
    const ssh = await api(user, '/api/me/ssh');
    if (ssh.sshPort > 0) { port = ssh.sshPort; break; }
    await user.waitForTimeout(1000);
  }
  if (!port) throw new Error('sshPort stayed 0 while the runtime is up');
  await chip.click();
  await sshItem.click();
  await user.getByRole('heading', { name: 'SSH-доступ' }).waitFor({ state: 'visible', timeout: 10000 });
  await user.getByPlaceholder('ssh-ed25519 …').waitFor({ state: 'visible', timeout: 10000 });
  const portLine = await user.getByText(/ssh root@.*-p \d+/, { exact: false }).first().textContent();
  if (!portLine.includes(`-p ${port}`)) throw new Error(`cabinet SSH line does not advertise live port ${port}: ${portLine}`);
  console.log(`PASS: SSH overlay shows the key field and live port ${port}`);

  if (errors.length) throw new Error(`browser page errors: ${errors.join('; ')}`);
  await screenshot(user);
} catch (error) {
  await screenshot(userPage);
  throw error;
} finally {
  await browser.close();
}
