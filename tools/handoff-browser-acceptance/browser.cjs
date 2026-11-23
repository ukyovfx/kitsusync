// Chromium acceptance for the handoff mapping page (/bot/admin/handoff).
// Started by TestHandoffBrowserAcceptance; needs HANDOFF_TEST_URL and
// HANDOFF_SCREENSHOTS_DIR. Playwright resolves from NODE_PATH or the project,
// and PLAYWRIGHT_MODULE_PATH overrides it for local runs.
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

const shot = name => path.join(outDir, name);

(async () => {
  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  const fail = message => { throw new Error(message); };

  await page.goto(url, { waitUntil: 'networkidle' });

  // (a) 13 product columns fit 1280px without horizontal scrolling.
  const wrap = page.locator('.handoff-matrix-wrap');
  if (!(await wrap.count())) fail('.handoff-matrix-wrap not found');
  const size = await wrap.evaluate(el => ({ scroll: el.scrollWidth, client: el.clientWidth }));
  const doc = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
  const columns = await page.locator('.handoff-matrix thead th:not(.handoff-matrix-corner)').count();
  console.log(`matrix wrap ${size.scroll}/${size.client}, document ${doc.scroll}/${doc.client}, columns ${columns}`);
  if (size.scroll > size.client + 1) fail(`matrix scrolls horizontally: ${size.scroll} > ${size.client}`);
  if (doc.scroll > doc.client + 1) fail(`page scrolls horizontally: ${doc.scroll} > ${doc.client}`);
  if (columns !== 13) fail(`expected 13 product columns, got ${columns}`);
  await page.screenshot({ path: shot('01_matrix_1280px_no_scroll.png') });

  // (b) Hand-off chips keep the flow word readable and summarize the recipients.
  const handCell = page.locator('.handoff-cell[data-flow="hand"]').first();
  if (!(await handCell.count())) fail('no hand-off cell found');
  const chip = handCell.locator('.handoff-chip-hand');
  const flow = chip.locator('.handoff-chip-flow');
  const targets = chip.locator('.handoff-chip-targets');
  const flowText = (await flow.innerText()).trim();
  const flowClipped = await flow.evaluate(el => el.scrollWidth > el.clientWidth + 1);
  const targetsText = (await targets.innerText()).trim();
  const chipTitle = (await chip.getAttribute('title')) || '';
  console.log(`hand chip flow "${flowText}" (clipped=${flowClipped}), targets "${targetsText}", title "${chipTitle}"`);
  if (flowText !== '受け渡し') fail(`hand chip flow label is "${flowText}"`);
  if (flowClipped) fail('hand chip flow label is clipped');
  if (!targetsText.startsWith('→ ')) fail(`hand chip does not summarize recipients: "${targetsText}"`);
  if (!chipTitle.includes(targetsText)) fail(`hand chip title "${chipTitle}" does not carry the full list`);
  await wrap.screenshot({ path: shot('02_handoff_chip_with_recipients.png') });

  // (c) Clicking one shelved cell opens editing for that cell only.
  const shelveCell = page.locator('.handoff-cell[data-flow="shelve"]').first();
  const cellID = await shelveCell.getAttribute('data-handoff-cell');
  await shelveCell.locator('.handoff-view').click();
  await page.waitForTimeout(200);
  const editingCount = await page.locator('.handoff-cell.is-editing').count();
  const opened = await shelveCell.evaluate(el => el.classList.contains('is-editing'));
  const editorVisible = await shelveCell.locator('.handoff-edit').isVisible();
  console.log(`editing cell ${cellID}: is-editing=${opened} visible=${editorVisible} editing cells=${editingCount}`);
  if (!opened || !editorVisible || editingCount !== 1) fail(`click did not open exactly one cell editor (open=${opened} visible=${editorVisible} count=${editingCount})`);
  await page.screenshot({ path: shot('03_shelve_cell_editing_opened.png') });

  // (d) Edit, save, reload: the change is persisted.
  // Re-locate by the stable cell id: the data-flow locator would re-resolve
  // to the next shelved cell once this cell's flow changes to hand.
  const editCell = page.locator(`.handoff-cell[data-handoff-cell="${cellID}"]`);
  await editCell.locator('select[name="row_flow"]').selectOption('hand');
  const firstBox = editCell.locator('.handoff-notify-list input[type="checkbox"]').first();
  const recipient = (await firstBox.evaluate(el => el.closest('label').textContent.trim()));
  await firstBox.check();
  await editCell.locator('.handoff-edit-done').click();
  const chipBeforeSave = (await editCell.locator('.handoff-chip-hand').innerText()).replace(/\s+/g, ' ').trim();
  if (!chipBeforeSave.includes(recipient)) fail(`chip did not update before save: "${chipBeforeSave}"`);
  await page.locator('button[type="submit"]').click();
  await page.waitForLoadState('networkidle');
  const reloaded = page.locator(`.handoff-cell[data-handoff-cell="${cellID}"]`);
  const reloadedFlow = await reloaded.getAttribute('data-flow');
  const reloadedText = (await reloaded.locator('.handoff-chip-hand').innerText()).replace(/\s+/g, ' ').trim();
  console.log(`reloaded ${cellID}: flow=${reloadedFlow} text="${reloadedText}"`);
  if (reloadedFlow !== 'hand') fail(`saved flow did not persist: ${reloadedFlow}`);
  if (!reloadedText.includes(recipient)) fail(`saved recipient did not persist: "${reloadedText}"`);
  await page.screenshot({ path: shot('04_saved_and_reloaded.png') });

  // (e) Product types are chip-style inputs on wrapped horizontal lines.
  const products = page.locator('.handoff-products-wrap');
  const layout = await products.evaluate(el => ({ display: getComputedStyle(el).display, wrap: getComputedStyle(el).flexWrap }));
  const chips = page.locator('.handoff-product-chip:not(.handoff-product-chip-add)');
  const chipCount = await chips.count();
  const heights = await chips.evaluateAll(nodes => nodes.map(node => Math.round(node.getBoundingClientRect().height)));
  const addChip = page.locator('.handoff-product-chip-add');
  console.log(`products display=${layout.display}/${layout.wrap} chips=${chipCount} heights=${[...new Set(heights)].join(',')}`);
  if (layout.display !== 'flex' || layout.wrap !== 'wrap') fail(`product types are not a wrapped chip row: ${JSON.stringify(layout)}`);
  if (chipCount !== 13) fail(`expected 13 product chips, got ${chipCount}`);
  if (heights.some(height => height > 44)) fail(`product chips are taller than one line: ${heights.join(',')}`);
  if (!(await addChip.count())) fail('add product type chip not found');
  await products.evaluate(el => el.closest('.section-card').scrollIntoView());
  await products.locator('..').screenshot({ path: shot('05_product_types_chips_row.png') });

  await browser.close();
  console.log('All browser verification checks passed successfully!');
})().catch(error => {
  console.error(error);
  process.exit(1);
});
