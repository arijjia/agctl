// first-session-ui.mjs — регрессия дефекта D6 (ручной кейс Б7 протокола
// РТиППО): «чистый браузер → первый запуск → композер активен».
//
// Проверяет на живом стенде agctl (BASE_URL с уже зарегистрированным демо-
// юзером): свежий browser-контекст (localStorage пуст = «чистый кэш»),
// логин, ленивый старт чат-контейнера (терпит 503-окно прогрева), затем:
//   1. composer редактируемый (contenteditable=true);
//   2. сессия открыта (data-conversation-session не пуст);
//   3. чип воркспейса показывает папку, а не плейсхолдер «Choose workspace»;
//   4. в консоли нет фатальных ошибок класса D6-эпохи
//      (found is not defined, setEnabled is not a function);
//   5. перезагрузка (потеря сохранённого выбора) снова приводит к активному
//      композеру — restore-путь не оставляет пользователя в hero.
//
// Запуск: node first-session-ui.mjs <BASE_URL> <login> <password>
// Обвязка с изолированным стендом — bin/e2e-first-session.sh.
import { chromium } from 'playwright';

const [base = 'http://127.0.0.1:18937', login = 'demo-user', password = 'DemoUser-2026!'] = process.argv.slice(2);
const FATAL = /found is not defined|setEnabled is not a function/i;
const failures = [];

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } }); // свежий профиль — «чистый кэш»
const page = await context.newPage();
const consoleLines = [];
page.on('pageerror', (err) => consoleLines.push(`[pageerror] ${err.message}`));
page.on('console', (m) => { if (m.type() === 'error') consoleLines.push(`[console.error] ${m.text().slice(0, 160)}`); });

async function waitChatReady() {
  // Ленивый старт контейнера: gateway отвечает 503 до готовности — браузер
  // сам повторяет навигацию; ждём и композер, с запасом на прогрев образа.
  await page.waitForURL('**/chat/**', { timeout: 150000 }).catch(() => {});
  await page.waitForSelector('[data-composer-input]', { timeout: 150000 }).catch(() => {});
  await page.waitForFunction(() => document.querySelector('[data-composer-input]')?.getAttribute('contenteditable') === 'true', null, { timeout: 60000 })
    .catch(() => {});
  await page.waitForTimeout(4000); // плагины/эффекты догоняют первый кадр
}

async function assertActive(label) {
  const s = await page.evaluate(() => {
    const composer = document.querySelector('[data-composer-input]');
    const chip = [...document.querySelectorAll('button[aria-haspopup="menu"]')]
      .find((b) => /workspace|воркспейс/i.test(b.getAttribute('aria-label') || ''));
    return {
      composerEditable: composer?.getAttribute('contenteditable') === 'true',
      session: document.querySelector('[data-conversation-content]')?.getAttribute('data-conversation-session') || null,
      chipText: chip ? chip.textContent.trim() : null,
    };
  }).catch(() => ({ err: 'page evaluate failed' }));
  console.log(`[${label}]`, JSON.stringify(s));
  if (s.err || !s.composerEditable) failures.push(`${label}: композер не активен`);
  if (!s.err && !s.session) failures.push(`${label}: сессия не открыта`);
  if (!s.err && (!s.chipText || /^choose workspace$|^выберите воркспейс$/i.test(s.chipText))) {
    failures.push(`${label}: чип показывает плейсхолдер вместо папки`);
  }
  return s;
}

// Сценарий 1: первый запуск (кейс Б7).
await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
await page.getByLabel('Логин').fill(login);
await page.getByLabel('Пароль').fill(password);
await page.getByRole('button', { name: /Войти/ }).click();
await waitChatReady();
await assertActive('first-launch');

// Сценарий 2: потеря сохранённого выбора (перезагрузка с тем же контейнером).
await page.reload({ waitUntil: 'domcontentloaded' });
await page.waitForSelector('[data-composer-input]', { timeout: 120000 }).catch(() => {});
await page.waitForTimeout(6000);
await assertActive('after-reload');

const fatal = consoleLines.filter((l) => FATAL.test(l));
if (fatal.length > 0) failures.push(`фатальные ошибки консоли: ${fatal.slice(0, 3).join(' | ')}`);

await page.screenshot({ path: `${process.env.E2E_SHOT_PREFIX || '/tmp/agctl-first-session'}.png` });
await browser.close();

if (failures.length > 0) {
  console.error('FAIL:\n- ' + failures.join('\n- '));
  process.exit(1);
}
console.log('PASS: первый запуск и перезагрузка — композер активен, сессия открыта, фатальных ошибок нет');
