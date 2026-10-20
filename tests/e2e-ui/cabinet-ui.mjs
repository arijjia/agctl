import { chromium } from 'playwright';

const required = ['PLATFORM_URL', 'E2E_LOGIN', 'E2E_PASSWORD', 'E2E_LLM_BASE_URL'];
for (const name of required) {
  if (!process.env[name]) throw new Error(`missing environment variable ${name}`);
}

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const pageErrors = [];
page.on('pageerror', (error) => pageErrors.push(error.message));

try {
  // The real product entry: auth SPA -> root redirect -> DSH chat. This is the
  // behavior the owner asked for, not direct navigation to a workspace URL.
  await page.goto(process.env.PLATFORM_URL, { waitUntil: 'domcontentloaded' });
  await page.getByRole('button', { name: 'Регистрация' }).waitFor({ state: 'visible', timeout: 15000 });
  await page.getByRole('button', { name: 'Регистрация' }).click();
  await page.getByLabel('Логин').fill(process.env.E2E_LOGIN);
  await page.getByLabel('Пароль').fill(process.env.E2E_PASSWORD);
  await page.getByRole('button', { name: 'Создать аккаунт' }).click();
  await page.waitForURL((url) => url.pathname === '/chat/', { timeout: 15000 });
  console.log('PASS: registration on platform root redirects straight to /chat/');

  // The per-user DSH runtime is created lazily on first chat request. Retry the
  // gateway while the container and dsh web finish booting.
  const cabinetEntry = page.getByText('Кабинет', { exact: true }).first();
  let dshReady = false;
  for (let i = 0; i < 35; i++) {
    if (await cabinetEntry.isVisible().catch(() => false)) {
      dshReady = true;
      break;
    }
    await page.goto(`${process.env.PLATFORM_URL.replace(/\/$/, '')}/chat/`, { waitUntil: 'domcontentloaded' }).catch(() => {});
    await page.waitForTimeout(500);
  }
  if (!dshReady) throw new Error('personal DSH chat did not become ready at /chat/');

  // DSH first-run dialogs are local to the persistent user dsh-home. Save a
  // fake key for this isolated test server, or continue if already configured.
  const continueButton = page.getByRole('button', { name: 'Continue' });
  if (await continueButton.count()) await continueButton.dispatchEvent('click');
  const apiKeyInput = page.getByPlaceholder('Enter your API key');
  try {
    await apiKeyInput.waitFor({ state: 'visible', timeout: 2500 });
    await apiKeyInput.fill('sk-playwright-dummy-key');
    await page.getByRole('button', { name: 'Save and continue' }).dispatchEvent('click');
    await page.getByRole('button', { name: 'Configure later' }).waitFor({ state: 'hidden', timeout: 10000 });
  } catch {
    // Existing isolated test dsh-home may already have dismissed the dialog.
  }

  // Chat mode is intentionally session-only: internal per-session Workspaces
  // stay hidden, but the DSH Session must still be creatable and promptable.
  if (await page.getByText('Add an API key to get started').isVisible().catch(() => false)) {
    throw new Error('platform default LLM did not suppress the first-run API-key modal');
  }
  await page.getByText('ИИ-мастерская ТюмГУ', { exact: true }).waitFor({ state: 'visible', timeout: 10000 });
  await page.getByText('Тюмень на правильном пути', { exact: true }).waitFor({ state: 'visible', timeout: 10000 });
  await page.getByText('Сессии', { exact: true }).waitFor({ state: 'visible', timeout: 10000 });
  if (await page.locator('svg[viewBox="0 0 23.16 17.04"]').count() !== 0) {
    throw new Error('DeepSeek whale mark remains in Chat mode');
  }
  if (await page.locator('img[data-agctl-logo]').count() === 0) throw new Error('TюмГУ logo image missing');
  const visibleWorkspaceRows = await page.locator('.YDXeBa_projectRow').evaluateAll((rows) => rows.filter((row) => !!row.offsetParent).length);
  if (visibleWorkspaceRows !== 0) throw new Error(`Chat mode exposed ${visibleWorkspaceRows} Workspace directory rows`);
  const chatOnlyControls = [
    'button[aria-label="Open right sidebar"]',
    'button[aria-label*="Workspace files"]',
    'button[aria-label*="New terminal"]',
    'button[aria-label*="Access mode"]',
    'button[aria-label="Add files or run commands"]',
  ];
  for (const selector of chatOnlyControls) {
    await page.locator(selector).waitFor({ state: 'hidden', timeout: 10000 }).catch(() => {
      throw new Error(`Chat mode exposes a development/file control: ${selector}`);
    });
  }
  await page.locator('button[aria-label="New session"]').first().click();
  const chatComposer = page.locator('[data-composer-input="true"]');
  await chatComposer.waitFor({ state: 'visible', timeout: 10000 });
  await page.waitForFunction(() => document.querySelector('[data-composer-input="true"]')?.getAttribute('contenteditable') === 'true', { timeout: 10000 });
  await chatComposer.fill('Проверь чат ТюмГУ');
  await chatComposer.press('Enter');
  await page.getByText('FAKE-LLM-REPLY: всё работает', { exact: false }).last().waitFor({ state: 'visible', timeout: 20000 });
  console.log('PASS: Chat mode branding, hidden directory tools, New Session and send work');

  await cabinetEntry.click();
  await page.getByRole('heading', { name: 'Кабинет agctl' }).waitFor({ state: 'visible' });
  await page.getByText(`Вошёл как ${process.env.E2E_LOGIN}`, { exact: false }).waitFor({ state: 'visible', timeout: 15000 });
  console.log('PASS: cabinet plugin opens and reuses the agctl session same-origin');

  await page.getByPlaceholder('имя (например, demo-1c)').fill('playwright-ui-workspace');
  await page.getByPlaceholder('LLM Base URL (например, https://litellm.example/v1)').fill(process.env.E2E_LLM_BASE_URL);
  await page.getByPlaceholder('модель (например, gpt-4.1)').fill('stub-model');
  await page.getByPlaceholder('API-токен (не сохраняется в браузере; пусто = использовать сохранённый)').fill('sk-playwright-e2e-not-a-secret');
  await page.getByPlaceholder('SSH public key (необязательно; например, содержимое ~/.ssh/id_ed25519.pub)').fill('');
  await page.getByRole('button', { name: 'Запросить' }).click();

  const adminSection = page.locator('section').filter({ hasText: 'Заявки на апрув (администратор)' });
  await adminSection.getByText('playwright-ui-workspace', { exact: false }).waitFor({ state: 'visible', timeout: 15000 });
  await adminSection.getByRole('button', { name: 'Одобрить' }).click();
  console.log('PASS: server workspace request appears and admin approves it in the plugin');

  await page.getByText('playwright-ui-workspace', { exact: false }).last().waitFor({ state: 'visible', timeout: 30000 });
  console.log('PASS: approved dev workspace appears alongside the default chat');

  if (pageErrors.length) throw new Error(`browser page errors: ${pageErrors.join('; ')}`);
  if (process.env.E2E_SCREENSHOT) await page.screenshot({ path: process.env.E2E_SCREENSHOT, fullPage: true });
} catch (error) {
  if (process.env.E2E_SCREENSHOT) await page.screenshot({ path: process.env.E2E_SCREENSHOT, fullPage: true }).catch(() => {});
  throw error;
} finally {
  await browser.close();
}
