import { chromium } from 'playwright';

// v3 product flow, browser-level: register a plain user -> request developer
// role -> admin approves (second browser context) -> developer saves SSH
// pubkey -> admin restarts the personal runtime (the key applies at the next
// container start, as the UI says) -> sshd comes up with a loopback port ->
// create a project folder via the native "Add workspace" dialog -> project
// row appears in the sidebar -> session starts inside the project workspace.
// The real SSH login is verified by the wrapper (bin/e2e-developer-flow.sh).

const required = [
  'PLATFORM_URL', 'E2E_LOGIN', 'E2E_PASSWORD',
  'E2E_ADMIN_LOGIN', 'E2E_ADMIN_PASSWORD',
  'E2E_SSH_PUBKEY', 'E2E_PROJECT', 'E2E_LLM_BASE_URL',
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

let devPage = null;
try {
  // ── developer: register through the SPA (second account => role user) ─────
  // The SPA lands on /chat and the platform boots the personal runtime right
  // away — real product behavior. The SSH key applies at the next start, so
  // the flow restarts the runtime after saving the key (admin action below).
  const devContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const dev = await devContext.newPage();
  devPage = dev;
  dev.on('pageerror', (error) => errors.push('dev: ' + error.message));
  await dev.goto(process.env.PLATFORM_URL, { waitUntil: 'domcontentloaded' });
  await dev.getByRole('button', { name: 'Регистрация' }).waitFor({ state: 'visible', timeout: 15000 });
  await dev.getByRole('button', { name: 'Регистрация' }).click();
  await dev.getByLabel('Логин').fill(process.env.E2E_LOGIN);
  await dev.getByLabel('Пароль').fill(process.env.E2E_PASSWORD);
  await dev.getByRole('button', { name: 'Создать аккаунт' }).click();
  await dev.waitForURL((url) => url.pathname.startsWith('/chat'), { timeout: 15000 });
  console.log('PASS: developer registers via SPA and lands in /chat');
  // Do not boot the personal runtime yet: the SSH key must be saved first.

  const me = await api(dev, '/api/me');
  if (me.user.role !== 'user') throw new Error(`expected role user, got ${me.user.role}`);

  const request = await api(dev, '/api/role-requests', 'POST', { motivation: 'e2e: нужен SSH и проекты' });
  if (request.request.status !== 'pending') throw new Error('role request is not pending');
  console.log('PASS: developer submits developer-role request');

  // ── admin (first account, created by the wrapper): approves in own context ─
  const adminContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const admin = await adminContext.newPage();
  admin.on('pageerror', (error) => errors.push('admin: ' + error.message));
  await admin.goto(process.env.PLATFORM_URL, { waitUntil: 'domcontentloaded' });
  await admin.getByLabel('Логин').fill(process.env.E2E_ADMIN_LOGIN);
  await admin.getByLabel('Пароль').fill(process.env.E2E_ADMIN_PASSWORD);
  await admin.getByRole('button', { name: 'Войти' }).click();
  // Admin lands on the admin panel (/admin) rather than the chat.
  await admin.waitForURL((url) => url.pathname.startsWith('/admin') || url.pathname.startsWith('/chat'), { timeout: 15000 });
  const queue = await api(admin, '/api/role-requests');
  const pending = queue.requests.filter((r) => r.login === process.env.E2E_LOGIN && r.status === 'pending');
  if (pending.length !== 1) throw new Error(`expected 1 pending request from ${process.env.E2E_LOGIN}, got ${pending.length}`);
  await api(admin, `/api/role-requests/${pending[0].id}/approve`, 'POST');
  console.log('PASS: admin approves the developer-role request');

  // ── developer: save SSH pubkey; it applies at the next container start ────
  const saved = await api(dev, '/api/me/ssh', 'PUT', { pubKey: process.env.E2E_SSH_PUBKEY });
  if (!saved.ok) throw new Error('PUT /api/me/ssh did not return ok');
  console.log('PASS: developer saves SSH pubkey (applies at next runtime start)');

  // ── admin: restart the developer runtime so sshd starts with the key ──────
  const users = await api(admin, '/api/admin/users/details');
  const devUser = (users.users || []).find((u) => u.login === process.env.E2E_LOGIN);
  if (!devUser) throw new Error('developer account not found in admin users list');
  if (!devUser.container || !devUser.container.name) throw new Error('developer chat container is not running');
  await api(admin, `/api/admin/containers/${encodeURIComponent(devUser.container.name)}/restart`, 'POST');
  console.log('PASS: admin restarts the developer runtime with the saved SSH key');

  // ── developer: reopen the personal chat (fresh runtime with sshd+key) ─────
  // The cabinet entry is the agctl profile chip in the sidebar footer.
  const chip = dev.locator('[data-agctl-cabinet-chip]');
  let dshReady = false;
  for (let i = 0; i < 45; i++) {
    if (await chip.isVisible().catch(() => false)) { dshReady = true; break; }
    await dev.goto(`${process.env.PLATFORM_URL.replace(/\/$/, '')}/chat/`, { waitUntil: 'domcontentloaded' }).catch(() => {});
    await dev.waitForTimeout(500);
  }
  if (!dshReady) throw new Error('personal DSH chat did not become ready at /chat/');
  // The registration-era page still holds websockets to the pre-restart
  // runtime; a fresh load reconnects everything to the restarted container.
  await dev.reload({ waitUntil: 'domcontentloaded' });
  await chip.waitFor({ state: 'visible', timeout: 20000 });

  // dsh 0.2.2 first-run preview notice (RU/EN variants) blocks the UI.
  for (const name of ['Продолжить', 'Continue']) {
    const button = dev.getByRole('button', { name, exact: true }).first();
    if (await button.isVisible().catch(() => false)) {
      await button.click();
      await button.waitFor({ state: 'hidden', timeout: 10000 }).catch(() => {});
    }
  }
  const continueButton = dev.getByRole('button', { name: 'Continue' });
  if (await continueButton.count()) await continueButton.dispatchEvent('click');
  const apiKeyInput = dev.getByPlaceholder('Enter your API key');
  try {
    await apiKeyInput.waitFor({ state: 'visible', timeout: 2500 });
    await apiKeyInput.fill('sk-playwright-dummy-key');
    await dev.getByRole('button', { name: 'Save and continue' }).dispatchEvent('click');
    await dev.getByRole('button', { name: 'Configure later' }).waitFor({ state: 'hidden', timeout: 10000 });
  } catch { /* isolated dsh-home may already be configured */ }

  // Developer role turns on the native Workspaces sidebar.
  await dev.getByText('Рабочие пространства', { exact: true }).first().waitFor({ state: 'visible', timeout: 15000 });
  console.log('PASS: developer chat boots with workspace sidebar visible');

  // ── cabinet profile chip → popup → SSH-доступ (developer-only item) ───────
  await chip.click();
  const sshItem = dev.getByRole('button', { name: 'SSH-доступ', exact: true });
  await sshItem.waitFor({ state: 'visible', timeout: 10000 });
  await sshItem.click();
  await dev.getByRole('heading', { name: 'SSH-доступ' }).waitFor({ state: 'visible', timeout: 10000 });
  const area = dev.getByPlaceholder('ssh-ed25519 …');
  await area.waitFor({ state: 'visible', timeout: 10000 });
  // The textarea is filled asynchronously after GET /api/me/ssh resolves.
  await dev.waitForFunction(
    (expected) => document.querySelector('textarea[placeholder^="ssh-ed25519"]')?.value.trim() === expected,
    process.env.E2E_SSH_PUBKEY.trim(),
    { timeout: 10000 },
  );
  const shownKey = (await area.inputValue()).trim();
  if (shownKey !== process.env.E2E_SSH_PUBKEY.trim()) throw new Error('cabinet SSH tab does not show the saved pubkey');
  // The runtime is up (chat booted), so the connect line must carry a port.
  let port = 0;
  for (let i = 0; i < 60; i++) {
    const ssh = await api(dev, '/api/me/ssh');
    if (ssh.sshPort > 0) { port = ssh.sshPort; break; }
    await dev.waitForTimeout(1000);
  }
  if (!port) throw new Error('sshPort stayed 0 while the runtime is up');
  const portLine = await dev.getByText(/ssh root@.*-p \d+/, { exact: false }).first().textContent();
  if (!portLine.includes(`-p ${port}`)) throw new Error(`cabinet SSH line does not advertise live port ${port}: ${portLine}`);
  console.log(`PASS: cabinet SSH overlay shows the key and live port ${port}`);

  // Back to the chat and create the project folder via the native dialog.
  await dev.locator('button[aria-label="Закрыть"]').first().click();
  await dev.getByText('Рабочие пространства', { exact: true }).first().waitFor({ state: 'visible', timeout: 15000 });
  await dev.locator('button[aria-label="Добавить рабочую область"]').first().click();
  const dialog = dev.locator('[data-agctl-workspace-create]');
  await dialog.waitFor({ state: 'visible', timeout: 10000 });
  // Developer role unlocks the GitHub-clone field in the dialog.
  await dialog.getByText('Публичный GitHub HTTPS URL', { exact: false }).waitFor({ state: 'visible', timeout: 5000 });
  await dialog.getByPlaceholder('Имя воркспейса').fill(process.env.E2E_PROJECT);
  await dialog.getByRole('button', { name: 'Создать' }).click();
  await dialog.waitFor({ state: 'hidden', timeout: 30000 });

  // The project is a real DSH Workspace: a visible row in the native sidebar.
  const projectRow = dev.getByText(process.env.E2E_PROJECT, { exact: true }).first();
  await projectRow.waitFor({ state: 'visible', timeout: 30000 });
  console.log('PASS: project folder appears as a workspace row in the sidebar');

  // Selecting the project row moves new sessions into /workspace/<project>.
  await projectRow.click();
  await dev.locator('button[aria-label="Новая сессия"]').first().click();
  const composer = dev.locator('[data-composer-input="true"]');
  await composer.waitFor({ state: 'visible', timeout: 15000 });
  await dev.waitForFunction(
    () => document.querySelector('[data-composer-input="true"]')?.getAttribute('contenteditable') === 'true',
    { timeout: 10000 },
  );
  await composer.fill(`работаем в ${process.env.E2E_PROJECT}`);
  await composer.press('Enter');
  await dev.getByText('FAKE-LLM-REPLY', { exact: false }).last().waitFor({ state: 'visible', timeout: 20000 });
  console.log('PASS: session created in project workspace and prompt round-trips');

  if (errors.length) throw new Error(`browser page errors: ${errors.join('; ')}`);
  await screenshot(dev);
} catch (error) {
  await screenshot(devPage);
  throw error;
} finally {
  await browser.close();
}
