import { chromium } from "playwright";
const BASE = "http://127.0.0.1:8929";
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
await page.goto(BASE + "/", { waitUntil: "networkidle" });
await page.locator('input').first().fill("demo-user");
await page.locator('input[type="password"]').fill("DemoUser-2026!");
await page.locator('button[type="submit"], button:has-text("Войти")').first().click();
await page.waitForTimeout(1500);
console.log("url:", page.url());
// ждём прогрев чата и чип консоли
await page.waitForTimeout(6000);
const chip = page.locator("[data-agctl-wsc-chip]");
console.log("chip visible:", await chip.count());
await page.screenshot({ path: "/tmp/console-chip.png" });
if (await chip.count()) {
  await chip.first().click();
  await page.waitForTimeout(1200);
  // раскроем «Настроить» у первого воркспейса, если есть
  const tune = page.getByRole("button", { name: "Настроить" }).first();
  if (await tune.count()) { await tune.click(); await page.waitForTimeout(1200); }
  await page.screenshot({ path: "/tmp/console-open.png" });
}
await browser.close();
console.log("done");
