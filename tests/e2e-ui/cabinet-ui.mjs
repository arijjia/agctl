import { chromium } from 'playwright';

const required = ['DSH_URL', 'AGCTL_URL', 'E2E_LOGIN', 'E2E_PASSWORD', 'E2E_LLM_BASE_URL'];
for (const name of required) {
  if (!process.env[name]) throw new Error(`missing environment variable ${name}`);
}

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const pageErrors = [];
page.on('pageerror', (error) => pageErrors.push(error.message));

try {
  await page.goto(process.env.DSH_URL, { waitUntil: 'domcontentloaded' });

  // dsh shows the API-key modal on every fresh boot until a key is saved;
  // saving a dummy key (this instance talks to the fake LLM only) closes it
  // for good. Configure later would reopen it on the next interaction. On
  // repeat runs the modal may already be dismissed — both steps are optional.
  const continueButton = page.getByRole('button', { name: 'Continue' });
  if (await continueButton.count()) {
    await continueButton.dispatchEvent('click');
  }
  const apiKeyInput = page.getByPlaceholder('Enter your API key');
  try {
    await apiKeyInput.waitFor({ state: 'visible', timeout: 5000 });
    await apiKeyInput.fill('sk-playwright-dummy-key');
    await page.getByRole('button', { name: 'Save and continue' }).dispatchEvent('click');
    await page.getByRole('button', { name: 'Configure later' }).waitFor({ state: 'hidden', timeout: 15000 });
  } catch {
    // already configured on a previous run — nothing to dismiss
  }

  const cabinetEntry = page.getByText('Кабинет', { exact: true }).first();
  await cabinetEntry.waitFor({ state: 'visible', timeout: 30000 });
  await cabinetEntry.click();
  await page.getByRole('heading', { name: 'Кабинет agctl' }).waitFor({ state: 'visible' });
  console.log('PASS: plugin panel registered and opens in dsh sidebar');

  await page.getByPlaceholder('https://agctl.example.org (пусто = тот же origin)').fill(process.env.AGCTL_URL);
  await page.getByRole('button', { name: 'Обновить' }).click();
  await page.getByRole('heading', { name: 'Вход' }).waitFor({ state: 'visible' });
  await page.getByPlaceholder('логин').fill(process.env.E2E_LOGIN);
  await page.getByPlaceholder('пароль').fill(process.env.E2E_PASSWORD);
  await page.getByRole('button', { name: 'Войти' }).click();
  await page.getByText(`Вошёл как ${process.env.E2E_LOGIN}`, { exact: false }).waitFor({ state: 'visible', timeout: 15000 });
  console.log('PASS: browser login crosses CORS boundary and receives agctl session');

  await page.getByPlaceholder('имя (например, demo-1c)').fill('playwright-ui-workspace');
  await page.getByPlaceholder('LLM Base URL (например, https://litellm.example/v1)').fill(process.env.E2E_LLM_BASE_URL);
  await page.getByPlaceholder('модель (например, gpt-4.1)').fill('stub-model');
  await page.getByPlaceholder('API-токен (не сохраняется в браузере; пусто = использовать сохранённый)').fill('sk-playwright-e2e-not-a-secret');
  await page.getByRole('button', { name: 'Создать' }).click();
  await page.getByText('playwright-ui-workspace', { exact: true }).waitFor({ state: 'visible', timeout: 30000 });
  console.log('PASS: browser creates a workspace through the cabinet UI');

  if (pageErrors.length) throw new Error(`browser page errors: ${pageErrors.join('; ')}`);
  if (process.env.E2E_SCREENSHOT) {
    await page.screenshot({ path: process.env.E2E_SCREENSHOT, fullPage: true });
  }
} catch (error) {
  if (process.env.E2E_SCREENSHOT) {
    await page.screenshot({ path: process.env.E2E_SCREENSHOT, fullPage: true }).catch(() => {});
  }
  throw error;
} finally {
  await browser.close();
}
