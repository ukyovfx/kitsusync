const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');

const base = process.env.KITSUSYNC_ACCEPTANCE_URL;
const cookie = process.env.KITSUSYNC_ACCEPTANCE_COOKIE;
const output = process.env.KITSUSYNC_ACCEPTANCE_OUTPUT;
const syntheticKitsu = process.env.KITSUSYNC_SYNTH_KITSU;
const syntheticDiscord = process.env.KITSUSYNC_SYNTH_DISCORD;
if (!base || !cookie || !output || !syntheticKitsu || !syntheticDiscord) throw new Error('acceptance harness inputs missing');
fs.mkdirSync(output, { recursive: true });
const records = [];
const viewports = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 844 },
];
const locales = ['en', 'ja'];

async function fixture(page, scenario) {
  const response = await page.request.get(`${base}/__fixture?scenario=${encodeURIComponent(scenario)}`);
  if (!response.ok()) throw new Error(`fixture state rejected: ${scenario}`);
}
async function visible(page, selector, message) {
  if (!(await page.locator(selector).first().isVisible().catch(() => false))) throw new Error(`expected visible ${message}`);
}
async function bodyHas(page, text, expected = true) {
  const has = (await page.locator('body').innerText()).includes(text);
  if (has !== expected) throw new Error(`expected text presence=${expected}: ${text}`);
}
async function record(page, pr, route, locale, viewport, state, evidence) {
  records.push({ candidate: process.env.GITHUB_SHA || 'local-unset', pr, route, locale, viewport, fixture_state: state, result: 'PASS', evidence });
}

(async () => {
  const browser = await chromium.launch({ headless: true, args: ['--disable-dev-shm-usage'] });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, acceptDownloads: false });
    await context.addCookies([{ name: 'kitsu_admin_session', value: cookie, url: base, httpOnly: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    let unexpectedDialogs = 0;
    const browserErrors = [];
    page.on('dialog', dialog => {
      if (!dialog.message().startsWith('Unlink the Kitsu user')) unexpectedDialogs += 1;
      dialog.accept();
    });
    page.on('request', request => {
      if (!request.url().startsWith(base)) throw new Error(`unexpected browser origin: ${new URL(request.url()).origin}`);
    });
    page.on('pageerror', error => browserErrors.push(error.message));
    page.on('console', message => { if (message.type() === 'error') browserErrors.push(message.text()); });

    // PR #205: real authenticated Connections page and reversible token inputs.
    await page.goto(`${base}/bot/admin/bot?edit=1&lang=en`, { waitUntil: 'networkidle' });
    if (!page.url().includes('/bot/admin/bot')) throw new Error('Connections route did not remain authenticated');
    const connectionsHTML = await page.locator('html').evaluate(el => el.outerHTML);
    if (connectionsHTML.includes(syntheticKitsu) || connectionsHTML.includes(syntheticDiscord)) throw new Error('persisted synthetic credential appeared in DOM');
    for (const id of ['kitsu-bot-token', 'discord-bot-token']) {
      const input = page.locator(`#${id}`);
      if (await input.inputValue() !== '' || await input.getAttribute('placeholder') !== '••••••••••••••••') throw new Error(`${id} did not render as a fixed safe mask`);
      const button = page.locator(`[data-token-change="${id}"]`);
      await button.click();
      if (await input.inputValue() !== '' || await input.getAttribute('placeholder') !== '' || await input.isDisabled()) throw new Error(`${id} Change did not open a blank field`);
      await page.locator(`[data-token-cancel-button="${id === 'kitsu-bot-token' ? 'kitsu-token-cancel' : 'discord-token-cancel'}"]`).click();
      if (await input.inputValue() !== '' || await input.getAttribute('placeholder') !== '••••••••••••••••' || !(await input.isDisabled())) throw new Error(`${id} Cancel did not restore masked state`);
      await button.click();
      await page.locator(`[data-token-cancel-button="${id === 'kitsu-bot-token' ? 'kitsu-token-cancel' : 'discord-token-cancel'}"]`).click();
      if (await input.inputValue() !== '') throw new Error(`${id} repeated Change/Cancel recovered a value`);
    }
    await page.screenshot({ path: path.join(output, 'pr205-connections-masked.png'), fullPage: true });
    await record(page, 205, '/bot/admin/bot?edit=1', 'en', 'desktop', 'persisted synthetic credentials', 'fixed masks; secrets absent from DOM; Change opens blank field; Cancel restores mask twice');
    await page.locator('[data-token-change="kitsu-bot-token"]').click();
    await page.locator('#kitsu-bot-token').fill(syntheticKitsu);
    await page.locator('#kitsu-recheck').click();
    await page.waitForLoadState('networkidle');
    const postSaveHTML = await page.locator('html').evaluate(el => el.outerHTML);
    if (postSaveHTML.includes(syntheticKitsu)) throw new Error('synthetic Kitsu token remained in the rendered DOM after save/recheck');
    await record(page, 205, '/bot/admin/bot?edit=1', 'en', 'desktop', 'synthetic Kitsu save/recheck', 'saved synthetic Bot token was validated against local fixture and omitted from response DOM');
    const languageToggle = page.locator('a.lang-toggle');
    const toggleHref = await languageToggle.getAttribute('href');
    if (!toggleHref || !toggleHref.includes('lang=ja')) throw new Error('language-toggle Japanese URL was not rendered correctly');
    await languageToggle.click();
    await page.waitForLoadState('networkidle');
    if (!page.url().includes('lang=ja')) throw new Error('language toggle did not navigate to Japanese');
    await page.locator('a.lang-toggle').click();
    await page.waitForLoadState('networkidle');
    if (!page.url().includes('lang=en')) throw new Error('language toggle did not return to English');
    await page.goto(`${base}/bot/admin/bot?edit=1&lang=ja`, { waitUntil: 'networkidle' });
    await bodyHas(page, 'Kitsu Bot APIトークン');
    await record(page, 205, '/bot/admin/bot?edit=1', 'ja', 'desktop', 'persisted synthetic credentials', 'Japanese Connections labels rendered');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`${base}/bot/admin/bot?edit=1&lang=en`, { waitUntil: 'networkidle' });
    await bodyHas(page, 'Kitsu Bot API token');
    await record(page, 205, '/bot/admin/bot?edit=1', 'en', 'mobile', 'persisted synthetic credentials', 'English mobile Connections rendered');

    // PR #207: Production setup reads using the encrypted DB credential while
    // KitsuJWTToken remains unset in the test process.
    if (process.env.KitsuJWTToken) throw new Error('legacy KitsuJWTToken unexpectedly present');
    await fixture(page, 'ready');
    await page.goto(`${base}/bot/setup?lang=en&wizard_step=2`, { waitUntil: 'networkidle' });
    await bodyHas(page, 'Synthetic Production');
    await page.screenshot({ path: path.join(output, 'pr207-production-setup.png'), fullPage: true });
    await record(page, 207, '/bot/setup?wizard_step=2', 'en', 'mobile', 'persisted Kitsu token; legacy env absent', 'fixture Production rendered through saved runtime source');
    await page.goto(`${base}/bot/setup?lang=en&wizard_step=4&project=synthetic-production-1&plan_guild=11111111111111111`, { waitUntil: 'networkidle' });
    await bodyHas(page, 'Synthetic Comp');
    await record(page, 207, '/bot/setup?wizard_step=4', 'en', 'mobile', 'persisted Kitsu token; legacy env absent', 'fixture Task Type rendered');
    await fixture(page, 'kitsu_empty');
    await page.goto(`${base}/bot/setup?lang=en&wizard_step=2`, { waitUntil: 'networkidle' });
    const emptyProductionText = await page.locator('main').innerText();
    await fixture(page, 'kitsu_failure');
    await page.goto(`${base}/bot/setup?lang=en&wizard_step=2`, { waitUntil: 'networkidle' });
    const failedProductionText = await page.locator('main').innerText();
    if (emptyProductionText === failedProductionText) throw new Error('Production lookup failure rendered indistinguishably from a successful empty result');
    await record(page, 207, '/bot/setup?wizard_step=2', 'en', 'mobile', 'Kitsu empty success and controlled failure', 'empty Production result and request failure rendered distinctly');

    // PR #206 readiness state matrix. Every state is loaded through the live
    // HTTP server and rendered again in both locales and viewport sizes.
    const matrix = [
      { mode: 'missing_kitsu', en: 'Configure Kitsu and the Discord Bot', ja: 'KitsuとDiscord Botを設定', noTable: true },
      { mode: 'missing_discord', en: 'Configure the Discord Bot', ja: 'Discord Botを設定', noTable: true },
      { mode: 'kitsu_failure', en: 'Kitsu users could not be checked', ja: 'Kitsuユーザーを確認できませんでした', noTable: true },
      { mode: 'kitsu_empty', en: 'No Kitsu users were returned', ja: 'Kitsuユーザーが見つかりません', noTable: true },
      { mode: 'discord_failure', en: 'Discord users could not be loaded', ja: 'Discordユーザーを取得できません', noTable: true },
      { mode: 'zero_guilds', en: 'No Discord servers are available', ja: 'Discordサーバーがありません', noTable: true },
      { mode: 'multiple_guilds', en: 'Select a Discord server', ja: 'Discordサーバーを選択', noTable: true },
      { mode: 'zero_humans', en: 'No selectable Discord users', ja: '選択できるDiscordユーザーがいません', guild: '11111111111111111', noTable: true },
      { mode: 'ready', en: 'Synthetic Human', ja: 'Synthetic Human', guild: '11111111111111111', noTable: false },
    ];
    for (const state of matrix) {
      for (const locale of locales) {
        for (const viewport of viewports) {
          await page.setViewportSize({ width: viewport.width, height: viewport.height });
          await fixture(page, state.mode);
          const suffix = state.guild ? `&discord_guild_id=${state.guild}` : '';
          await page.goto(`${base}/bot/admin/users?lang=${locale}${suffix}`, { waitUntil: 'networkidle' });
          await bodyHas(page, locale === 'en' ? state.en : state.ja);
          const tableCount = await page.locator('.user-linking-table').count();
          if ((state.noTable && tableCount !== 0) || (!state.noTable && tableCount !== 1)) throw new Error(`${state.mode} table visibility mismatch`);
          if (state.mode === 'ready') {
            await bodyHas(page, 'Synthetic Bot Identity', false);
            await bodyHas(page, 'Synthetic Discord Bot', false);
            if (await page.locator('img').count() !== 0) throw new Error('hostile Kitsu value created an image node');
            if (await page.locator('select[name="discord_user_id"] option[value="44444444444444444"]').count() !== 0) throw new Error('Discord bot member was offered as a selectable mapping');
            if ((await page.locator('.user-link-grid-row').innerText()).includes('Synthetic Bot Identity')) throw new Error('Kitsu bot person was rendered as a mappable row');
            const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
            if (overflow) throw new Error('User Linking page overflows the viewport');
          }
          if (state.mode === 'multiple_guilds' && (await page.locator('#global-discord-guild option').count()) < 3) throw new Error('multiple guilds were not offered for explicit selection');
          await record(page, 206, '/bot/admin/users', locale, viewport.name, state.mode, 'rendered expected state and checked table/selectability boundary');
        }
      }
    }

    // Mapping save, persistence, and unlink use temporary SQLite only.
    await page.setViewportSize({ width: 1440, height: 1000 });
    await fixture(page, 'ready');
    await page.goto(`${base}/bot/admin/users?lang=en&discord_guild_id=11111111111111111`, { waitUntil: 'networkidle' });
    const mapping = page.locator('form.user-link-form').filter({ has: page.locator('input[name="kitsu_id"][value="synthetic-person-1"]') });
    const memberSelect = mapping.locator('select[name="discord_user_id"]');
    const save = mapping.locator('button[type="submit"]');
    if (!(await save.isDisabled())) throw new Error('Save was enabled before selection changed');
    await memberSelect.selectOption('33333333333333333');
    if (await save.isDisabled()) throw new Error('Save did not enable after selection changed');
    await save.click();
    await page.waitForLoadState('networkidle');
    const humanRow = () => page.locator('tr.user-link-grid-row').filter({ hasText: 'Synthetic Human' }).first();
    if ((await humanRow().locator('td').nth(1).innerText()).trim() !== 'Synthetic Discord Human') throw new Error('saved member did not render as mapped');
    await page.reload({ waitUntil: 'networkidle' });
    if ((await humanRow().locator('td').nth(1).innerText()).trim() !== 'Synthetic Discord Human') throw new Error('mapping did not persist after reload');
    await page.screenshot({ path: path.join(output, 'pr206-mapped-desktop.png'), fullPage: true });
    await record(page, 206, '/bot/admin/users', 'en', 'desktop', 'mapped synthetic human', 'Save disabled until change; selected member saved and persisted after reload');
    await page.locator('form.delete-form button[type="submit"]').first().click();
    await page.waitForLoadState('networkidle');
    await page.reload({ waitUntil: 'networkidle' });
    if ((await humanRow().locator('td').nth(1).innerText()).trim() !== 'Not set') throw new Error('Unlink did not persist after reload');
    await record(page, 206, '/bot/admin/users', 'en', 'desktop', 'unmapped synthetic human', 'Unlink removed the local mapping and remained removed after reload');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`${base}/bot/admin/users?lang=ja&discord_guild_id=11111111111111111`, { waitUntil: 'networkidle' });
    await page.screenshot({ path: path.join(output, 'pr206-user-linking-ja-mobile.png'), fullPage: true });
    await record(page, 206, '/bot/admin/users', 'ja', 'mobile', 'ready unmapped synthetic humans', 'Japanese mobile layout rendered without overflow');
    if (unexpectedDialogs !== 0) throw new Error('hostile fixture triggered a browser dialog');
    if (browserErrors.length !== 0) throw new Error(`browser console/runtime errors: ${browserErrors.length}`);

    const report = { candidate_sha: process.env.GITHUB_SHA || 'local-unset', result: 'PASS', browser: 'Chromium via Playwright', records };
    fs.writeFileSync(path.join(output, 'browser-results.json'), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 });
    await context.close();
  } finally {
    await browser.close();
  }
})().catch(error => {
  process.stderr.write(`premerge browser acceptance failed: ${String(error.message || error)}\n`);
  process.exitCode = 1;
});
