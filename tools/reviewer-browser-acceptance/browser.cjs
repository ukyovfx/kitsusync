const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');

const base = process.env.KITSUSYNC_REVIEWER_BROWSER_URL;
const output = process.env.KITSUSYNC_REVIEWER_BROWSER_OUTPUT;
const guildID = process.env.KITSUSYNC_REVIEWER_BROWSER_GUILD;
const head = process.env.KITSUSYNC_REVIEWER_BROWSER_SHA || 'local';
const username = 'manager@synthetic.invalid';
const password = 'synthetic-browser-password';
if (!base || !output || !guildID) throw new Error('isolated acceptance inputs are missing');
fs.mkdirSync(output, { recursive: true });

const records = [];
const errors = [];
const locales = [
  { lang: 'en', automatic: 'Automatic', overrides: 'Overrides', supervisor: 'Project Supervisor', comp: 'Compositing Supervisor' },
  { lang: 'ja', automatic: '自動', overrides: 'Overrides', supervisor: 'Project Supervisor', comp: 'Compositing担当' },
];
const viewports = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 844 },
];

async function record(page, route, locale, viewport, state, detail) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
  if (overflow) throw new Error(`${route} overflows at ${viewport}`);
  const body = await page.locator('body').innerText();
  if (body.includes('\uFFFD') || body.includes('Ã') || body.includes('ï¿½')) throw new Error(`${route} contains mojibake at ${locale}`);
  records.push({ route, locale, viewport, state, detail });
}

async function fixture(page, scenario) {
  const response = await page.request.get(`${base}/__fixture?scenario=${encodeURIComponent(scenario)}`);
  if (response.status() !== 204) throw new Error(`synthetic fixture rejected state ${scenario}`);
}

async function gotoUsers(page, locale, extra = '') {
  await page.goto(`${base}/bot/admin/projects?project=reviewer-production&tab=users&lang=${locale.lang}${extra}`, { waitUntil: 'networkidle' });
}

function automaticGroup(page, locale) {
  const heading = page.getByRole('heading', { name: locale.automatic, exact: true });
  return page.locator('.reviewer-group').filter({ has: heading });
}

function overridesGroup(page, locale) {
  const heading = page.getByRole('heading', { name: locale.overrides, exact: true });
  return page.locator('.reviewer-group').filter({ has: heading });
}

async function assertAutomatic(page, locale, expected, forbidden = []) {
  const text = await automaticGroup(page, locale).innerText();
  for (const value of expected) if (!text.includes(value)) throw new Error(`Automatic is missing ${value}`);
  for (const value of forbidden) if (text.includes(value)) throw new Error(`Automatic includes ineligible person ${value}`);
}

(async () => {
  const browser = await chromium.launch({ headless: true, args: ['--disable-dev-shm-usage'] });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, acceptDownloads: false });
    const page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
    const interceptedExternal = [];
    await page.route('**/*', route => {
      const url = new URL(route.request().url());
      if (url.origin === new URL(base).origin) return route.continue();
      if (url.hostname === 'fonts.googleapis.com' || url.hostname === 'fonts.gstatic.com') {
        interceptedExternal.push(url.hostname);
        return route.fulfill({ status: 200, contentType: url.hostname === 'fonts.googleapis.com' ? 'text/css' : 'font/woff2', body: '' });
      }
      errors.push(`unexpected browser origin ${url.origin}`);
      return route.abort();
    });

    // Enter through the ordinary protected route and complete the ordinary login form.
    await gotoUsers(page, locales[0]);
    if (!page.url().includes('/bot/login')) throw new Error('protected Production Users route did not redirect to login');
    const unauthenticatedWrite = await page.request.post(`${base}/bot/admin/projects`, {
      form: { action: 'add_production_reviewer_target', project_id: 'reviewer-production', task_type_id: 'task-comp', target_kind: 'user', target_id: '22222222222222234' },
      maxRedirects: 0,
    });
    if (unauthenticatedWrite.status() !== 303 || !unauthenticatedWrite.headers()['location']?.includes('/bot/login')) {
      throw new Error('unauthenticated Reviewer write was not redirected to normal login');
    }
    await page.locator('#login-email').fill(username);
    await page.locator('#login-password').fill(password);
    await page.getByRole('button', { name: 'Login', exact: true }).click();
    await page.waitForURL(url => url.pathname === '/bot/admin/projects', { timeout: 10000 });
    if (await context.cookies(base).then(cookies => !cookies.some(cookie => cookie.name === 'kitsu_admin_session' && cookie.httpOnly))) {
      throw new Error('normal login did not establish the HttpOnly KitsuSync session');
    }
    records.push({ route: '/bot/login', locale: 'en', viewport: 'desktop', state: 'synthetic manager login', detail: 'normal form authenticated against the isolated Kitsu fixture and received a server-created HttpOnly session' });

    await gotoUsers(page, locales[0]);
    if (!(await page.locator('.production-reviewer-manager').count())) throw new Error('Reviewer manager did not render');
    await assertAutomatic(page, locales[0], ['Project Supervisor', 'Compositing Supervisor', 'Global Name Supervisor', 'Username Supervisor'], [
      'Departmentless Supervisor', 'Wrong Department Supervisor', 'Production Manager', 'Global Admin',
      'Demoted Supervisor', 'Project Manager Override', 'Position Only', 'Inactive Supervisor', 'Kitsu Bot', 'Unlinked Supervisor',
    ]);
    const teamText = await page.locator('main').innerText();
    for (const expected of ['Global Admin', 'Guild Nick Supervisor', 'Global Name Fallback', 'Username Fallback', 'Synthetic Review Production']) {
      if (!teamText.includes(expected)) throw new Error(`Production Team view is missing ${expected}`);
    }
    for (const stale of ['Automatic inactive while overridden', 'Add Production member', 'Remove Production member', 'Reviewer / Checker task types', 'Production Manager fallback', 'global CheckerMap']) {
      if (teamText.includes(stale)) throw new Error(`stale/manual Reviewer copy remains: ${stale}`);
    }
    const userForms = page.locator('form.reviewer-target-form').filter({ has: page.locator('input[name="target_kind"][value="user"]') });
    const userSelect = userForms.locator('select[name="target_id"]');
    for (const blocked of ['22222222222222232', '22222222222222233', '22222222222222235']) {
      if (await userSelect.locator(`option[value="${blocked}"]`).count()) throw new Error(`ineligible user ${blocked} is selectable`);
    }
    if (!(await userSelect.locator('option[value="22222222222222234"]').count())) throw new Error('linked Team Guild member is unavailable as an override');
    const roleForms = page.locator('form.reviewer-target-form').filter({ has: page.locator('input[name="target_kind"][value="role"]') });
    const roleSelect = roleForms.locator('select[name="target_id"]');
    if (!(await roleSelect.locator('option[value="33333333333333331"]').count())) throw new Error('mentionable Guild Role is not selectable');
    for (const blocked of [guildID, '33333333333333332']) {
      if (await roleSelect.locator(`option[value="${blocked}"]`).count()) throw new Error(`@everyone/non-mentionable Role ${blocked} is selectable`);
    }
    await page.screenshot({ path: path.join(output, 'reviewer-en-desktop-automatic.png'), fullPage: true });
    await record(page, '/bot/admin/projects?tab=users', 'en', 'desktop', 'automatic and candidate filtering', 'matching project_role Supervisor shown by guild nickname; ineligible roles and members excluded');

    // The live Task Type Department, not assignment or Position, selects the automatic Supervisor.
    await page.locator('form.reviewer-task-type-select select[name="reviewer_task_type"]').selectOption('task-animation');
    await page.locator('form.reviewer-task-type-select').getByRole('button', { name: 'View', exact: true }).click();
    await page.waitForLoadState('networkidle');
    await assertAutomatic(page, locales[0], ['Wrong Department Supervisor', 'Animation Supervisor'], ['Project Supervisor']);
    await page.locator('form.reviewer-task-type-select select[name="reviewer_task_type"]').selectOption('task-unassigned');
    await page.locator('form.reviewer-task-type-select').getByRole('button', { name: 'View', exact: true }).click();
    await page.waitForLoadState('networkidle');
    await assertAutomatic(page, locales[0], ['No matching Supervisor']);
    records.push({ route: '/bot/admin/projects?tab=users', locale: 'en', viewport: 'desktop', state: 'Task Type Department changes', detail: 'Animation and Department-less Task Types produce the expected fail-closed Automatic list' });

    // Additive controls are real same-origin POST forms protected by normal session/CSRF middleware.
    await page.locator('form.reviewer-task-type-select select[name="reviewer_task_type"]').selectOption('task-comp');
    await page.locator('form.reviewer-task-type-select').getByRole('button', { name: 'View', exact: true }).click();
    await page.waitForLoadState('networkidle');
    const userForm = page.locator('form.reviewer-target-form').filter({ has: page.locator('input[name="target_kind"][value="user"]') });
    await userForm.locator('select[name="target_id"]').selectOption('22222222222222234');
    await userForm.getByRole('button', { name: 'Add user', exact: true }).click();
    await page.waitForLoadState('networkidle');
    await assertAutomatic(page, locales[0], ['Project Supervisor', 'Compositing Supervisor', 'Global Name Supervisor', 'Username Supervisor']);
    const roleForm = page.locator('form.reviewer-target-form').filter({ has: page.locator('input[name="target_kind"][value="role"]') });
    await roleForm.locator('select[name="target_id"]').selectOption('33333333333333331');
    await roleForm.getByRole('button', { name: 'Add role', exact: true }).click();
    await page.waitForLoadState('networkidle');
    const overrideText = await overridesGroup(page, locales[0]).innerText();
    for (const value of ['Guild Nick Override', '@Reviewers']) if (!overrideText.includes(value)) throw new Error(`additive override missing ${value}`);
    const names = await overridesGroup(page, locales[0]).locator('.reviewer-target-row').allInnerTexts();
    if (names.length !== 2) throw new Error(`expected two explicit targets, got ${names.length}`);
    await page.screenshot({ path: path.join(output, 'reviewer-en-desktop-additive.png'), fullPage: true });
    await record(page, '/bot/admin/projects?tab=users', 'en', 'desktop', 'Automatic plus User and Role Overrides', 'both override kinds appear as additional targets while Automatic remains visible');

    // Removing one explicit target does not change the automatic reviewer or the other override.
    await overridesGroup(page, locales[0]).locator('.reviewer-target-row').filter({ hasText: 'Guild Nick Override' }).getByRole('button', { name: 'Remove', exact: true }).click();
    await page.waitForLoadState('networkidle');
    await assertAutomatic(page, locales[0], ['Project Supervisor', 'Compositing Supervisor', 'Global Name Supervisor', 'Username Supervisor']);
    const afterRemove = await overridesGroup(page, locales[0]).innerText();
    if (afterRemove.includes('Guild Nick Override') || !afterRemove.includes('@Reviewers')) throw new Error('override removal changed the wrong target');

    // Empty/error states use isolated fixtures and remain visibly distinct.
    const states = [
      { name: 'empty-team', expected: 'The Kitsu Production Team is empty' },
      { name: 'team-failure', expected: 'Production Team unavailable' },
      { name: 'no-matching', expected: 'No matching Supervisor' },
      { name: 'no-linked', expected: 'No matching Supervisor' },
      { name: 'no-roles', expected: 'No mentionable Discord roles are available' },
      { name: 'discord-failure', expected: 'Discord' },
      { name: 'stale-membership', expected: 'No matching Supervisor' },
    ];
    for (const state of states) {
      await fixture(page, state.name);
      await gotoUsers(page, locales[0]);
      const body = await page.locator('main').innerText();
      if (!body.includes(state.expected)) throw new Error(`${state.name} state is unclear; missing ${state.expected}`);
      if (state.name === 'team-failure' && await page.locator('form.reviewer-target-form input[name="target_kind"][value="user"]').count()) throw new Error('Team read failure left User Reviewer writes enabled');
      if (state.name === 'no-linked' && await page.locator('form.reviewer-target-form select[name="target_id"] option[value="22222222222222222"]').count()) throw new Error('unlinked Supervisor appeared as a User Override candidate');
      if (state.name === 'discord-failure' && await page.locator('form.reviewer-target-form select[name="target_id"] option[value="22222222222222222"]').count()) throw new Error('Guild lookup failure left a User Override candidate selectable');
      if (state.name === 'stale-membership' && await page.locator('form.reviewer-target-form select[name="target_id"] option[value="22222222222222222"]').count()) throw new Error('stale Team membership remained selectable after live Team changed');
      if (state.name === 'no-roles' && await page.locator('form.reviewer-target-form input[name="target_kind"][value="role"]').count()) throw new Error('no-roles state left a Role Override form enabled');
      await record(page, '/bot/admin/projects?tab=users', 'en', 'desktop', state.name, `visible fail-closed state: ${state.expected}`);
    }
    await fixture(page, 'ready');

    // JP/EN and desktop/mobile acceptance for Production Users, User Linking, and System Status.
    for (const locale of locales) {
      for (const viewport of viewports) {
        await page.setViewportSize({ width: viewport.width, height: viewport.height });
        await gotoUsers(page, locale);
        if (!(await page.locator('.production-reviewer-manager').count())) throw new Error(`Reviewer UI missing in ${locale.lang}`);
        await assertAutomatic(page, locale, [locale.supervisor, locale.comp, 'Global Name Supervisor', 'Username Supervisor']);
        await page.screenshot({ path: path.join(output, `reviewer-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/projects?tab=users', locale.lang, viewport.name, 'ready', 'Production Team and additive Reviewer controls rendered without overflow or mojibake');

        await page.goto(`${base}/bot/admin/users?lang=${locale.lang}&discord_guild_id=${guildID}`, { waitUntil: 'networkidle' });
        if (!(await page.locator('.user-linking-table').count())) throw new Error(`User Linking table missing in ${locale.lang}`);
        await page.screenshot({ path: path.join(output, `user-linking-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/users', locale.lang, viewport.name, 'ready', 'four-column User Linking table rendered');

        await page.goto(`${base}/bot/admin/health?lang=${locale.lang}`, { waitUntil: 'networkidle' });
        if (!(await page.locator('main').innerText()).trim()) throw new Error(`System Status did not render in ${locale.lang}`);
        await page.screenshot({ path: path.join(output, `system-status-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/health', locale.lang, viewport.name, 'ready', 'Current IA System Status loaded');
      }
    }

    if (errors.length) throw new Error(`browser console/runtime or external-origin errors: ${errors.length}`);
    const report = { candidate_sha: head, result: 'PASS', authentication: 'normal /bot/login synthetic manager flow', browser: 'Playwright Chromium', external_network: 'blocked; remote fonts fulfilled locally', intercepted_font_hosts: [...new Set(interceptedExternal)], states_checked: records };
    fs.writeFileSync(path.join(output, 'browser-results.json'), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 });
    await context.close();
  } finally {
    await browser.close();
  }
})().catch(error => {
  process.stderr.write(`reviewer browser acceptance failed: ${String(error.message || error)}\n`);
  process.exitCode = 1;
});
