// Chromium acceptance for the handoff wiring editor (/bot/admin/handoff).
// Started by TestHandoffBrowserAcceptance; needs HANDOFF_TEST_URL and
// HANDOFF_SCREENSHOTS_DIR. Playwright resolves from NODE_PATH or the project,
// and PLAYWRIGHT_MODULE_PATH overrides it for local runs.
//
// The page under test is the redesign's wiring editor: studio-common routes
// (task type -> notify task types) plus per-Production overrides with
// inheritance. The old product-dimension matrix page is gone.
const path = require('path');
const fs = require('fs');

let playwright;
try {
  playwright = require(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright');
} catch (error) {
  playwright = require(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright-core');
}
const { chromium } = playwright;

const url = process.env.HANDOFF_TEST_URL;
const outDir = process.env.HANDOFF_SCREENSHOTS_DIR;
if (!url || !outDir) {
  console.error('HANDOFF_TEST_URL and HANDOFF_SCREENSHOTS_DIR are required');
  process.exit(1);
}
fs.mkdirSync(outDir, { recursive: true });

const shot = name => path.join(outDir, name);

(async () => {
  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  const fail = message => { throw new Error(message); };

  await page.goto(url, { waitUntil: 'networkidle' });

  // (a) The three sections of the wiring editor render, at 1280px without
  // horizontal scrolling.
  const headings = (await page.locator('form.section-stack > section.section-card > h2').allTextContents()).map(text => text.trim());
  console.log(`headings: ${JSON.stringify(headings)}`);
  for (const want of ['受け渡しの配線（スタジオ共通）', 'この製作の上書き', '担当者なしの扱い']) {
    if (!headings.includes(want)) fail(`missing section heading "${want}": ${JSON.stringify(headings)}`);
  }
  const doc = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
  console.log(`document ${doc.scroll}/${doc.client}`);
  if (doc.scroll > doc.client + 1) fail(`page scrolls horizontally: ${doc.scroll} > ${doc.client}`);

  const tables = page.locator('table.handoff-routes');
  if (await tables.count() !== 2) fail(`expected 2 handoff-routes tables, got ${await tables.count()}`);
  const studioTable = tables.nth(0);
  const overrideTable = tables.nth(1);
  const studioRow = id => studioTable.locator('tr').filter({ has: page.locator(`input[name="st_notify_${id}"]`) });
  const overrideRow = id => overrideTable.locator('tr').filter({ has: page.locator(`select[name="ov_mode_${id}"]`) });
  const checked = async (row, name, value) => row.locator(`input[name="${name}"][value="${value}"]`).isChecked();

  // (b) Seeded wiring renders as seeded: Rendering hands off to Compositing
  // in the studio default, and this Production overrides Rendering to none
  // while inheriting the rest.
  if (!(await checked(studioRow('tt-render'), 'st_notify_tt-render', 'tt-comp'))) fail('studio Rendering -> Compositing checkbox is not checked');
  if (await checked(studioRow('tt-render'), 'st_notify_tt-render', 'tt-anim')) fail('studio Rendering -> Animation checkbox should start unchecked');
  if (!(await checked(studioRow('tt-lay'), 'st_notify_tt-lay', 'tt-anim'))) fail('studio Layout -> Animation checkbox is not checked');
  if (!(await checked(studioRow('tt-lay'), 'st_notify_tt-lay', 'tt-light'))) fail('studio Layout -> Lighting checkbox is not checked');
  const renderMode = overrideRow('tt-render').locator('select[name="ov_mode_tt-render"]');
  if ((await renderMode.inputValue()) !== 'none') fail(`override Rendering mode should be "none", got "${await renderMode.inputValue()}"`);
  const renderHelp = (await overrideRow('tt-render').locator('.field-help').innerText()).trim();
  console.log(`override Rendering help: "${renderHelp}"`);
  if (!renderHelp.includes('継承') || !renderHelp.includes('Compositing')) fail(`override Rendering does not show the inherited wiring: "${renderHelp}"`);
  if ((await overrideRow('tt-lay').locator('select[name="ov_mode_tt-lay"]').inputValue()) !== 'inherit') fail('override Layout mode should start as "inherit"');
  await page.screenshot({ path: shot('01_wiring_editor_seeded.png'), fullPage: true });

  // (c) Edit both layers: add Animation to the studio Rendering route, and
  // give Layout a Production-only override to Compositing. Save and wait for
  // the saved redirect.
  await studioRow('tt-render').locator('input[name="st_notify_tt-render"][value="tt-anim"]').check();
  await overrideRow('tt-lay').locator('select[name="ov_mode_tt-lay"]').selectOption('custom');
  await overrideRow('tt-lay').locator('input[name="ov_notify_tt-lay"][value="tt-comp"]').check();
  const savedNavigation = page.waitForURL(u => u.pathname === '/bot/admin/handoff' && u.searchParams.get('msg') === 'saved', { waitUntil: 'load', timeout: 15000 });
  await page.locator('button[type="submit"]').click();
  await savedNavigation;
  const notice = (await page.locator('.notice').first().innerText()).trim();
  console.log(`save notice: "${notice}"`);
  if (!notice.includes('設定を保存しました')) fail(`save did not confirm: "${notice}"`);

  // (d) Reload from storage: both edits persisted, the untouched studio
  // Layout route survived the whole-replace save, and the Rendering
  // override is still "none".
  await page.reload({ waitUntil: 'networkidle' });
  if (!(await checked(studioRow('tt-render'), 'st_notify_tt-render', 'tt-comp'))) fail('studio Rendering -> Compositing did not persist');
  if (!(await checked(studioRow('tt-render'), 'st_notify_tt-render', 'tt-anim'))) fail('studio Rendering -> Animation did not persist');
  if (!(await checked(studioRow('tt-lay'), 'st_notify_tt-lay', 'tt-anim'))) fail('untouched studio Layout -> Animation was lost in the save');
  if (!(await checked(studioRow('tt-lay'), 'st_notify_tt-lay', 'tt-light'))) fail('untouched studio Layout -> Lighting was lost in the save');
  if ((await overrideRow('tt-lay').locator('select[name="ov_mode_tt-lay"]').inputValue()) !== 'custom') fail('override Layout mode did not persist as "custom"');
  if (!(await checked(overrideRow('tt-lay'), 'ov_notify_tt-lay', 'tt-comp'))) fail('override Layout -> Compositing did not persist');
  if ((await overrideRow('tt-render').locator('select[name="ov_mode_tt-render"]').inputValue()) !== 'none') fail('override Rendering mode did not persist as "none"');
  await page.screenshot({ path: shot('02_wiring_editor_saved.png'), fullPage: true });

  await browser.close();
  console.log('All browser verification checks passed successfully!');
})().catch(error => {
  console.error(error);
  process.exit(1);
});
