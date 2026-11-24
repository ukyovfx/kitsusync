// Chromium acceptance for the handoff wiring editors. Started by
// TestHandoffBrowserAcceptance; needs HANDOFF_TEST_URL (the Production
// handoff page), HANDOFF_BOT_URL (the connection settings page) and
// HANDOFF_SCREENSHOTS_DIR. Playwright resolves from NODE_PATH or the
// project, and PLAYWRIGHT_MODULE_PATH overrides it for local runs.
//
// The wiring has two editors since the 2026-11-24 placement fix: the
// studio-common routes live on the connection settings page, and the
// Production page carries only its overrides (with inheritance) and the
// no-assignee handling. Each row folds its target checkboxes into a
// <details> block, and a Task Type is never its own target.
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
const botUrl = process.env.HANDOFF_BOT_URL;
const outDir = process.env.HANDOFF_SCREENSHOTS_DIR;
if (!url || !botUrl || !outDir) {
  console.error('HANDOFF_TEST_URL, HANDOFF_BOT_URL and HANDOFF_SCREENSHOTS_DIR are required');
  process.exit(1);
}
fs.mkdirSync(outDir, { recursive: true });

const shot = name => path.join(outDir, name);

(async () => {
  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  const fail = message => { throw new Error(message); };
  const noHorizontalScroll = async label => {
    const doc = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
    console.log(`${label} document ${doc.scroll}/${doc.client}`);
    if (doc.scroll > doc.client + 1) fail(`${label} page scrolls horizontally: ${doc.scroll} > ${doc.client}`);
  };
  const openDetails = async row => {
    const details = row.locator('details.handoff-route-details');
    if (!(await details.evaluate(el => el.open))) {
      await details.locator('summary').click();
    }
  };
  const checked = async (row, name, value) => row.locator(`input[name="${name}"][value="${value}"]`).isChecked();

  // --- Studio-common editor on the connection settings page ---
  await page.goto(botUrl, { waitUntil: 'networkidle' });
  const studioForm = page.locator('form').filter({ has: page.locator('input[name="action"][value="save_studio_routes"]') });
  if (await studioForm.count() !== 1) fail(`expected exactly one studio routes form, got ${await studioForm.count()}`);
  const studioHeading = (await studioForm.locator('section.section-card > h2').allTextContents()).map(text => text.trim());
  console.log(`studio headings: ${JSON.stringify(studioHeading)}`);
  if (!studioHeading.includes('受け渡しの配線')) fail(`studio section heading missing: ${JSON.stringify(studioHeading)}`);
  await noHorizontalScroll('bot');

  const studioTable = studioForm.locator('table.handoff-routes');
  if (await studioTable.count() !== 1) fail(`expected 1 studio handoff-routes table, got ${await studioTable.count()}`);
  const studioRow = id => studioTable.locator('tr').filter({ has: page.locator(`input[name="st_notify_${id}"]`) });

  // Seeded wiring renders as seeded, and no row offers its own Task Type
  // as a target.
  if (!(await checked(studioRow('tt-render'), 'st_notify_tt-render', 'tt-comp'))) fail('studio Rendering -> Compositing checkbox is not checked');
  if (await checked(studioRow('tt-render'), 'st_notify_tt-render', 'tt-anim')) fail('studio Rendering -> Animation checkbox should start unchecked');
  if (!(await checked(studioRow('tt-lay'), 'st_notify_tt-lay', 'tt-anim'))) fail('studio Layout -> Animation checkbox is not checked');
  if (!(await checked(studioRow('tt-lay'), 'st_notify_tt-lay', 'tt-light'))) fail('studio Layout -> Lighting checkbox is not checked');
  if (await page.locator('input[name="st_notify_tt-render"][value="tt-render"]').count() !== 0) fail('studio Rendering offers itself as a target');
  await page.screenshot({ path: shot('01_studio_editor_seeded.png'), fullPage: true });

  // The no-handoff checkbox is exclusive: checking a target clears it,
  // and checking it clears the row's targets.
  await openDetails(studioRow('tt-comp'));
  const compNone = studioRow('tt-comp').locator('input[name="st_none_tt-comp"]');
  await compNone.check();
  if (await checked(studioRow('tt-comp'), 'st_notify_tt-comp', 'tt-anim')) fail('checking no-handoff did not clear the row targets');
  await studioRow('tt-comp').locator('input[name="st_notify_tt-comp"][value="tt-anim"]').check();
  if (await compNone.isChecked()) fail('checking a target did not clear the no-handoff checkbox');
  await studioRow('tt-comp').locator('input[name="st_notify_tt-comp"][value="tt-anim"]').uncheck();

  // Edit the studio layer: add Animation to the Rendering route, save,
  // and wait for the saved redirect back to the connection settings page.
  await openDetails(studioRow('tt-render'));
  await studioRow('tt-render').locator('input[name="st_notify_tt-render"][value="tt-anim"]').check();
  const studioSaved = page.waitForURL(u => u.pathname === '/bot/admin/bot/wiring' && u.searchParams.get('msg') === 'saved', { waitUntil: 'load', timeout: 15000 });
  await studioForm.locator('button[type="submit"]').click();
  await studioSaved;

  // --- Production overrides on the handoff page ---
  await page.goto(url, { waitUntil: 'networkidle' });
  const headings = (await page.locator('form.section-stack > section.section-card > h2').allTextContents()).map(text => text.trim());
  console.log(`handoff headings: ${JSON.stringify(headings)}`);
  for (const want of ['この製作の上書き', '担当者なしの扱い']) {
    if (!headings.includes(want)) fail(`missing section heading "${want}": ${JSON.stringify(headings)}`);
  }
  if (await page.locator('input[name^="st_notify_"]').count() !== 0) fail('the production page still carries the studio editor inputs');
  if (await page.locator('a[href*="/bot/admin/bot?edit=1"]').count() === 0) fail('the production page does not point at the connection settings page');
  await noHorizontalScroll('handoff');

  const tables = page.locator('table.handoff-routes');
  if (await tables.count() !== 1) fail(`expected 1 handoff-routes table, got ${await tables.count()}`);
  const overrideTable = tables.nth(0);
  const overrideRow = id => overrideTable.locator('tr').filter({ has: page.locator(`select[name="ov_mode_${id}"]`) });

  // Seeded overrides render as seeded: Rendering is "no handoff" here,
  // Layout inherits the studio wiring.
  const renderMode = overrideRow('tt-render').locator('select[name="ov_mode_tt-render"]');
  if ((await renderMode.inputValue()) !== 'none') fail(`override Rendering mode should be "none", got "${await renderMode.inputValue()}"`);
  const renderHelp = (await overrideRow('tt-render').locator('[data-handoff-summary="inherit"]').textContent()).trim();
  console.log(`override Rendering inherited summary: "${renderHelp}"`);
  if (!renderHelp.includes('継承') || !renderHelp.includes('Compositing')) fail(`override Rendering does not show the inherited wiring: "${renderHelp}"`);
  if ((await overrideRow('tt-lay').locator('select[name="ov_mode_tt-lay"]').inputValue()) !== 'inherit') fail('override Layout mode should start as "inherit"');
  // An inherit row is a single line: its target editor stays hidden with
  // no expandable toggle until the mode switches to custom.
  const layDetails = overrideRow('tt-lay').locator('details.handoff-route-details');
  if (!(await layDetails.evaluate(el => el.hidden))) fail('inherit Layout row shows an expandable editor toggle');
  // The fallback user ID field shows only for the chosen-user handling.
  if (await page.locator('#handoff-fallback-user-field').isVisible()) fail('fallback user field should start hidden for the admin handling');
  await page.locator('select[name="no_assignee"]').selectOption('users');
  if (!(await page.locator('#handoff-fallback-user-field').isVisible())) fail('fallback user field did not appear for the chosen-user handling');
  await page.locator('select[name="no_assignee"]').selectOption('admin');
  if (await page.locator('input[name="ov_notify_tt-lay"][value="tt-lay"]').count() !== 0) fail('override Layout offers itself as a target');
  await page.screenshot({ path: shot('02_overrides_seeded.png'), fullPage: true });

  // Edit the override layer: Layout gets a Production-only override to
  // Compositing. Save and wait for the saved redirect.
  await overrideRow('tt-lay').locator('select[name="ov_mode_tt-lay"]').selectOption('custom');
  await openDetails(overrideRow('tt-lay'));
  await overrideRow('tt-lay').locator('input[name="ov_notify_tt-lay"][value="tt-comp"]').check();
  const savedNavigation = page.waitForURL(u => u.pathname === '/bot/admin/handoff' && u.searchParams.get('msg') === 'saved', { waitUntil: 'load', timeout: 15000 });
  await page.locator('form').filter({ has: page.locator('input[name="project"]') }).locator('button[type="submit"]').click();
  await savedNavigation;
  const notice = (await page.locator('.notice').first().innerText()).trim();
  console.log(`save notice: "${notice}"`);
  if (!notice.includes('設定を保存しました')) fail(`save did not confirm: "${notice}"`);

  // Reload both pages from storage: both edits persisted, the untouched
  // studio Layout route survived the whole-replace save, and the
  // Rendering override is still "none".
  await page.goto(botUrl, { waitUntil: 'networkidle' });
  const studioForm2 = page.locator('form').filter({ has: page.locator('input[name="action"][value="save_studio_routes"]') });
  const studioTable2 = studioForm2.locator('table.handoff-routes');
  const studioRow2 = id => studioTable2.locator('tr').filter({ has: page.locator(`input[name="st_notify_${id}"]`) });
  if (!(await checked(studioRow2('tt-render'), 'st_notify_tt-render', 'tt-comp'))) fail('studio Rendering -> Compositing did not persist');
  if (!(await checked(studioRow2('tt-render'), 'st_notify_tt-render', 'tt-anim'))) fail('studio Rendering -> Animation did not persist');
  if (!(await checked(studioRow2('tt-lay'), 'st_notify_tt-lay', 'tt-anim'))) fail('untouched studio Layout -> Animation was lost in the save');
  if (!(await checked(studioRow2('tt-lay'), 'st_notify_tt-lay', 'tt-light'))) fail('untouched studio Layout -> Lighting was lost in the save');

  await page.goto(url, { waitUntil: 'networkidle' });
  const overrideTable2 = page.locator('table.handoff-routes').nth(0);
  const overrideRow2 = id => overrideTable2.locator('tr').filter({ has: page.locator(`select[name="ov_mode_${id}"]`) });
  if ((await overrideRow2('tt-lay').locator('select[name="ov_mode_tt-lay"]').inputValue()) !== 'custom') fail('override Layout mode did not persist as "custom"');
  if (!(await checked(overrideRow2('tt-lay'), 'ov_notify_tt-lay', 'tt-comp'))) fail('override Layout -> Compositing did not persist');
  if ((await overrideRow2('tt-render').locator('select[name="ov_mode_tt-render"]').inputValue()) !== 'none') fail('override Rendering mode did not persist as "none"');
  await page.screenshot({ path: shot('03_editors_saved.png'), fullPage: true });

  await browser.close();
  console.log('All browser verification checks passed successfully!');
})().catch(error => {
  console.error(error);
  process.exit(1);
});
