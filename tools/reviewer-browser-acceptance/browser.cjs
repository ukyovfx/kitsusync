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
function isNotificationsApplyRequest(request) {
  const actual = new URL(request.url());
  const expected = new URL('/bot/admin/projects?apply_notifications=1', base);
  return request.method() === 'POST' && actual.origin === expected.origin && actual.pathname === expected.pathname && actual.search === expected.search;
}
const locales = [
  { lang: 'en', automatic: 'Automatic recipients', overrides: 'Additional recipients', tabs: ['Overview', 'Notifications', 'Team', 'Settings'] },
  { lang: 'ja', automatic: '自動通知先', overrides: '追加通知先', tabs: ['概要', '通知', 'チーム', '設定'] },
];
const compositingAutomatic = ['@Guild Nick Supervisor', '@Global Name Fallback', '@username-fallback'];
const animationAutomatic = ['@Wrong Department Global'];
const viewports = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 844 },
];
const backgroundViewports = [
  { name: 'desktop-1440', width: 1440, height: 900 },
  { name: 'desktop-1920', width: 1920, height: 1080 },
  { name: 'mobile', width: 390, height: 844 },
];

async function record(page, route, locale, viewport, state, detail) {
  const layout = await page.evaluate(() => {
    const viewport = document.documentElement.clientWidth;
    const panel = document.querySelector('#panel-notifications');
    const notifications = document.querySelector('.production-notifications');
    const offenders = [...document.body.querySelectorAll('*')].map(element => {
      const rect = element.getBoundingClientRect();
      const style = getComputedStyle(element);
      return {
        tag: element.tagName,
        id: element.id,
        className: typeof element.className === 'string' ? element.className.slice(0, 100) : '',
        left: Math.round(rect.left),
        right: Math.round(rect.right),
        width: Math.round(rect.width),
        minWidth: style.minWidth,
        gridColumns: style.gridTemplateColumns,
        overflowX: style.overflowX,
      };
    }).filter(element => element.right > viewport + 2 || element.left < -2).slice(0, 12);
    return {
      document: document.documentElement.scrollWidth,
      body: document.body.scrollWidth,
      viewport,
      notificationsGrid: panel ? getComputedStyle(panel).gridTemplateColumns : '',
      notificationsMinWidth: notifications ? getComputedStyle(notifications).minWidth : '',
      offenders,
    };
  });
  if (layout.document > layout.viewport || layout.body > layout.viewport) {
    throw new Error(`${route} overflows at ${viewport}: ${JSON.stringify(layout)}`);
  }
  const body = await page.locator('body').innerText();
  if (body.includes('\uFFFD') || body.includes('Ã') || body.includes('ï¿½')) throw new Error(`${route} contains mojibake at ${locale}`);
  records.push({ route, locale, viewport, state, detail });
}

async function fixture(page, scenario) {
  const response = await page.request.get(`${base}/__fixture?scenario=${encodeURIComponent(scenario)}`);
  if (response.status() !== 204) throw new Error(`synthetic fixture rejected state ${scenario}`);
}

async function gotoWFARecipients(page, locale, extra = '') {
  await page.goto(`${base}/bot/admin/projects?project=reviewer-production&tab=notifications&lang=${locale.lang}${extra}`, { waitUntil: 'networkidle' });
}

async function gotoProduction(page, locale, tab, extra = '') {
  await page.goto(`${base}/bot/admin/projects?project=reviewer-production&tab=${tab}&lang=${locale.lang}${extra}`, { waitUntil: 'networkidle' });
}

async function automaticGroup(page, locale) {
	if (await page.locator('[data-current-routing-form]').count()) return page.locator('[data-wfa-automatic]');
	return page.locator('.production-wfa-summary > div').first();
}

async function overridesGroup(page, locale) {
	if (await page.locator('[data-current-routing-form]').count()) return page.locator('[data-wfa-additional]');
	return page.locator('.production-wfa-summary > div').nth(1);
}

async function assertAutomatic(page, locale, expected, forbidden = []) {
	const group = await automaticGroup(page, locale);
	const text = await group.innerText();
  for (const value of expected) if (!text.includes(value)) throw new Error(`Automatic is missing ${value}`);
  for (const value of forbidden) if (text.includes(value)) throw new Error(`Automatic includes ineligible person ${value}`);
}

async function assertBackgroundCanvas(page, mode, locale, viewport, screenshotName) {
  const canvas = page.locator(`canvas[data-background="${mode}"]`);
  if (await canvas.count() !== 1) throw new Error(`${mode} canvas missing for ${locale.lang}/${viewport.name}`);
  const details = await page.evaluate(() => {
    const canvas = document.querySelector('canvas[data-background]');
    const card = document.querySelector('.login-card');
    const formPanel = card?.querySelector('.section-card');
    const ctx = canvas?.getContext('2d');
    let visible = 0;
    let nonzeroAlpha = 0;
    let maxAlpha = 0;
    let gridCenterAlpha = 0;
    if (ctx) {
      const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
      const stride = canvas.dataset.background === 'app-dots' ? 4 : 4 * 16;
      const minVisibleAlpha = canvas.dataset.background === 'app-dots' ? 2 : 8;
      for (let i = 3; i < pixels.length; i += 4) {
        if (pixels[i] > 0) nonzeroAlpha++;
        if (pixels[i] > maxAlpha) maxAlpha = pixels[i];
        if (i % stride === 3 && pixels[i] > minVisibleAlpha) visible++;
      }
      if (canvas.dataset.background === 'app-dots') {
        const dpr = canvas.width / canvas.getBoundingClientRect().width;
        gridCenterAlpha = ctx.getImageData(Math.floor(18 * dpr), Math.floor(18 * dpr), 1, 1).data[3];
      }
    }
    let centered = null;
    if (card) {
      const rect = card.getBoundingClientRect();
      centered = {
        x: Math.abs(rect.left + rect.width / 2 - innerWidth / 2),
        y: Math.abs(rect.top + rect.height / 2 - innerHeight / 2),
      };
    }
    const cardBackground = card ? getComputedStyle(card).backgroundColor : '';
    const panelBackground = formPanel ? getComputedStyle(formPanel).backgroundColor : '';
    const cardRect = card?.getBoundingClientRect();
    const panelRect = formPanel?.getBoundingClientRect();
    const isOpaqueRGB = color => /^rgb\(\s*\d+\s*,\s*\d+\s*,\s*\d+\s*\)$/.test(color);
    const orangePixelsUnder = element => {
      if (!canvas || !ctx || !element || getComputedStyle(canvas).display === 'none') return 0;
      const r = element.getBoundingClientRect();
      const canvasRect = canvas.getBoundingClientRect();
      if (r.width <= 0 || r.height <= 0 || canvasRect.width <= 0 || canvasRect.height <= 0) return 0;
      const scale = canvas.width / canvasRect.width;
      const left = Math.max(0, Math.floor(r.left * scale));
      const top = Math.max(0, Math.floor(r.top * scale));
      const right = Math.min(canvas.width, Math.ceil(r.right * scale));
      const bottom = Math.min(canvas.height, Math.ceil(r.bottom * scale));
      const data = ctx.getImageData(left, top, right - left, bottom - top).data;
      let count = 0;
      for (let i = 0; i < data.length; i += 4 * 8) {
        if (data[i + 3] > 70 && data[i] > 100 && data[i + 1] > 14 && data[i + 1] < 150 && data[i + 2] < 100) count++;
      }
      return count;
    };
    return {
      visible, centered,
      canvasSize: canvas ? [canvas.width, canvas.height] : null,
      nonzeroAlpha, maxAlpha, gridCenterAlpha,
      cardBackground, panelBackground,
      cardOpaque: isOpaqueRGB(cardBackground),
      panelOpaque: isOpaqueRGB(panelBackground),
      canvasDisplay: canvas ? getComputedStyle(canvas).display : null,
      foregroundSamples: cardRect && panelRect ? [
        { name: 'card', x: Math.floor(cardRect.left + 8), y: Math.floor(cardRect.top + cardRect.height * .72) },
        { name: 'form panel', x: Math.floor(panelRect.left + 8), y: Math.floor(panelRect.top + panelRect.height * .5) },
      ] : [],
      canvasOrangeUnderCard: orangePixelsUnder(card),
      canvasOrangeUnderPanel: orangePixelsUnder(formPanel),
      width: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
      lang: document.documentElement.lang,
      gutterBackground: getComputedStyle(document.documentElement).backgroundColor,
    };
  });
  if (!(mode === 'login-fabric' && viewport.name === 'mobile') && details.visible < 3) throw new Error(`${mode} canvas is visually empty for ${locale.lang}/${viewport.name}: ${JSON.stringify({ visible: details.visible, canvasSize: details.canvasSize, nonzeroAlpha: details.nonzeroAlpha, maxAlpha: details.maxAlpha, gridCenterAlpha: details.gridCenterAlpha })}`);
  if (mode === 'login-fabric' && (!details.cardOpaque || !details.panelOpaque)) {
    throw new Error(`login foreground surfaces are translucent at ${viewport.name}: ${JSON.stringify({ card: details.cardBackground, panel: details.panelBackground })}`);
  }
  if (mode === 'login-fabric' && viewport.name !== 'mobile' && (details.canvasOrangeUnderCard < 1 || details.canvasOrangeUnderPanel < 1)) {
    throw new Error(`fabric no longer flows geometrically behind the Login card/form at ${viewport.name}: ${JSON.stringify({ card: details.canvasOrangeUnderCard, panel: details.canvasOrangeUnderPanel })}`);
  }
  if (mode === 'login-fabric' && viewport.name === 'mobile' && (details.nonzeroAlpha !== 0 || details.canvasDisplay !== 'none')) throw new Error(`mobile Login fabric is still visible for ${locale.lang}: ${JSON.stringify({ alpha: details.nonzeroAlpha, display: details.canvasDisplay })}`);
  if (details.width > details.clientWidth) throw new Error(`${mode} canvas caused horizontal overflow at ${viewport.name}`);
  if (details.lang !== locale.lang) throw new Error(`${mode} page language mismatch for ${locale.lang}`);
  if (details.gutterBackground !== 'rgb(7, 7, 7)') throw new Error(`${mode} scrollbar gutter is not using the dark page background at ${viewport.name}`);
  if (mode === 'login-fabric' && (!details.centered || details.centered.x > 8 || details.centered.y > 8)) {
    throw new Error(`login card lost centered composition at ${viewport.name}: ${JSON.stringify(details.centered)}`);
  }
  const screenshot = await page.screenshot({ path: path.join(output, screenshotName), fullPage: false });
  if (mode === 'login-fabric') {
    const samples = await page.evaluate(async ({ image, points }) => {
      const decoded = new Image();
      decoded.src = `data:image/png;base64,${image}`;
      await decoded.decode();
      const sampleCanvas = document.createElement('canvas');
      sampleCanvas.width = decoded.width;
      sampleCanvas.height = decoded.height;
      const sampleContext = sampleCanvas.getContext('2d');
      sampleContext.drawImage(decoded, 0, 0);
      return points.map(point => ({
        name: point.name,
        rgba: [...sampleContext.getImageData(point.x, point.y, 1, 1).data],
      }));
    }, { image: screenshot.toString('base64'), points: details.foregroundSamples });
    for (const sample of samples) {
      if (!sample.rgba || sample.rgba[0] > 70 || sample.rgba[1] > 70 || sample.rgba[2] > 70) {
        throw new Error(`Login ${sample.name} screenshot pixel is not an opaque dark foreground sample at ${viewport.name}: ${JSON.stringify(sample)}`);
      }
    }
  }
  await record(page, mode === 'login-fabric' ? '/bot/login' : '/bot/admin', locale.lang, viewport.name, 'background rendered', `${mode}; visible canvas pixels=${details.visible}; centered card=${JSON.stringify(details.centered)}`);
}

async function localCanvasAlpha(page, x, y, radius) {
  return page.evaluate(({ x, y, radius }) => {
    const canvas = document.querySelector('canvas[data-background]');
    const ctx = canvas?.getContext('2d');
    if (!canvas || !ctx) return 0;
    const dpr = canvas.width / canvas.getBoundingClientRect().width;
    const left = Math.max(0, Math.floor((x - radius) * dpr));
    const top = Math.max(0, Math.floor((y - radius) * dpr));
    const right = Math.min(canvas.width, Math.ceil((x + radius) * dpr));
    const bottom = Math.min(canvas.height, Math.ceil((y + radius) * dpr));
    const pixels = ctx.getImageData(left, top, right - left, bottom - top).data;
    let sum = 0;
    for (let i = 3; i < pixels.length; i += 4) sum += pixels[i];
    return sum;
  }, { x, y, radius });
}

async function fabricPeakY(page, x) {
  return page.evaluate(x => {
    const canvas = document.querySelector('canvas[data-background="login-fabric"]');
    const ctx = canvas?.getContext('2d');
    if (!canvas || !ctx) return null;
    const scale = canvas.width / canvas.getBoundingClientRect().width;
    const center = Math.max(0, Math.min(canvas.width - 1, Math.floor(x * scale)));
    const half = Math.floor(18 * scale);
    const left = Math.max(0, center - half);
    const right = Math.min(canvas.width, center + half);
    const data = ctx.getImageData(left, 0, right - left, canvas.height).data;
    let bestY = 0, best = 0;
    for (let y = 0; y < canvas.height; y++) {
      let row = 0;
      for (let px = 0; px < right - left; px++) row += data[(y * (right - left) + px) * 4 + 3];
      if (row > best) { best = row; bestY = y; }
    }
    return best > 0 ? bestY / scale : null;
  }, x);
}

async function assertLoginFabricGoldStandard(page, locale, viewport) {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.waitForTimeout(100);
  if (viewport.name === 'mobile') {
    const mobileFrame = await page.evaluate(() => {
      const canvas = document.querySelector('canvas[data-background="login-fabric"]');
      const pixels = canvas?.getContext('2d')?.getImageData(0, 0, canvas.width, canvas.height).data;
      const card = document.querySelector('.login-card');
      let count = 0;
      if (pixels) for (let i = 3; i < pixels.length; i += 4 * 16) if (pixels[i] > 8) count++;
      const rect = card?.getBoundingClientRect();
      return {
        count,
        profile: canvas?.dataset.profile,
        display: canvas ? getComputedStyle(canvas).display : null,
        centered: rect ? { x: Math.abs(rect.left + rect.width / 2 - innerWidth / 2), y: Math.abs(rect.top + rect.height / 2 - innerHeight / 2) } : null,
        width: innerWidth,
        overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
      };
    });
    if (mobileFrame.count !== 0 || mobileFrame.display !== 'none' || mobileFrame.profile !== 'mobile-no-fabric') throw new Error(`mobile Login waves were not hidden at ${locale.lang}: ${JSON.stringify(mobileFrame)}`);
    if (!mobileFrame.centered || mobileFrame.centered.x > 8 || mobileFrame.centered.y > 8 || mobileFrame.overflow) {
      throw new Error(`mobile login card or viewport layout is invalid at ${locale.lang}: ${JSON.stringify(mobileFrame)}`);
    }
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.waitForTimeout(100);
    const staticFrame = await page.locator('canvas[data-background="login-fabric"]').evaluate(canvas => canvas.toDataURL());
    await page.waitForTimeout(350);
    const reducedFrame = await page.locator('canvas[data-background="login-fabric"]').evaluate(canvas => canvas.toDataURL());
    if (reducedFrame !== staticFrame) throw new Error(`mobile Login background changed under reduced motion at ${locale.lang}`);
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await page.mouse.move(0, 0);
    await page.waitForTimeout(200);
    await page.mouse.move(mobileFrame.width * .30, 420);
    await page.waitForTimeout(100);
    if (await page.locator('canvas[data-background="login-fabric"]').evaluate(canvas => canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data.some((value, index) => index % 4 === 3 && value !== 0))) {
      throw new Error(`mobile pointer interaction rendered Login waves for ${locale.lang}`);
    }
    records.push({ route: '/bot/login', locale: locale.lang, viewport: viewport.name, state: 'no Login waves on phone widths', detail: 'fabric canvas hidden; centered card; no overflow; reduced-motion and pointer checks remained static' });
    return;
  }
  const geometry = await page.evaluate(() => {
    const canvas = document.querySelector('canvas[data-background="login-fabric"]');
    const card = document.querySelector('.login-card');
    if (!canvas || !card) return null;
    const rect = card.getBoundingClientRect();
    const width = document.documentElement.clientWidth;
    const centerY = rect.top + rect.height * .5;
    return {
      width,
      canvasWidth: canvas.getBoundingClientRect().width,
      centerY,
      cardCenterX: rect.left + rect.width * .5,
      cardLeft: rect.left,
      cardRight: rect.right,
      rows: Number(canvas.dataset.meshRows),
      spacing: Number(canvas.dataset.meshSpacingX),
      profile: canvas.dataset.profile,
      tiers: canvas.dataset.tierCounts.split(',').map(Number),
      streamDistance: Number(canvas.dataset.streamDistance),
    };
  });
  if (!geometry || Math.abs(geometry.canvasWidth - geometry.width) > 1 || Math.abs(geometry.cardCenterX - geometry.width / 2) > 8) {
    throw new Error(`login ribbon or card is not aligned to the viewport at ${viewport.name}: ${JSON.stringify(geometry)}`);
  }
  if (geometry.profile !== 'gold-standard-desktop' || geometry.rows !== 26 || geometry.spacing !== 7) {
    throw new Error(`desktop fabric departed from the Gold Standard density at ${viewport.name}: ${JSON.stringify(geometry)}`);
  }
  const visual = await page.evaluate(() => {
    const canvas = document.querySelector('canvas[data-background="login-fabric"]');
    const data = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data;
    let visible = 0, orange = 0, luminance = 0;
    for (let i = 0; i < data.length; i += 4) {
      if (data[i + 3] > 8) visible++;
      if (data[i + 3] > 70 && data[i] > 100 && data[i + 1] > 14 && data[i + 1] < 150) orange++;
      luminance += (data[i] * .2126 + data[i + 1] * .7152 + data[i + 2] * .0722) * data[i + 3] / 255;
    }
    return { visible, orange, luminance };
  });
  const tierTotal = geometry.tiers.reduce((sum, count) => sum + count, 0);
  const midBrightTiers = geometry.tiers.slice(4).reduce((sum, count) => sum + count, 0);
  if (tierTotal < 500 || midBrightTiers < 50 || visual.orange < 30 || visual.luminance < 10000) {
    throw new Error(`desktop fabric density/brightness fell below the prototype class at ${viewport.name}: ${JSON.stringify({ tierTotal, midBrightTiers, visual })}`);
  }
  const leftReach = await localCanvasAlpha(page, geometry.cardLeft - 14, geometry.centerY, 48);
  const rightReach = await localCanvasAlpha(page, geometry.cardRight + 14, geometry.centerY, 48);
  if (leftReach < 20 || rightReach < 20) throw new Error(`fabric does not reach the card edges at ${viewport.name}: ${leftReach}/${rightReach}`);
  const firstStream = geometry.streamDistance;
  await page.waitForTimeout(650);
  const laterStream = await page.locator('canvas[data-background="login-fabric"]').evaluate(canvas => Number(canvas.dataset.streamDistance));
  if (laterStream - firstStream < 5) throw new Error(`longitudinal stream did not move at ${viewport.name}: ${firstStream} -> ${laterStream}`);
  records.push({
    route: '/bot/login', locale: locale.lang, viewport: viewport.name,
    state: 'prototype Gold Standard fabric',
    detail: `26 strands at 7px; ${tierTotal} particles, ${midBrightTiers} mid/bright-tier particles, ${visual.orange} sampled orange pixels; card-edge activity ${leftReach}/${rightReach}; stream distance ${firstStream.toFixed(1)} -> ${laterStream.toFixed(1)}`,
  });
  await page.emulateMedia({ reducedMotion: 'no-preference' });
}

(async () => {
  const browser = await chromium.launch({ headless: true, args: ['--disable-dev-shm-usage'] });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, acceptDownloads: false });
    await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: new URL(base).origin });
    const page = await context.newPage();
    let expectedStaleApply = null;
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', message => {
      if (message.type() !== 'error') return;
      const text = message.text();
      const isExpectedConflictConsole = /^Failed to load resource: the server responded with a status of 409 \(Conflict\)$/.test(text);
      if (isExpectedConflictConsole && expectedStaleApply?.active && expectedStaleApply.requestSeen) {
        expectedStaleApply.console409Messages.push(text);
        return;
      }
      errors.push(text);
    });
    page.on('response', response => {
      if (response.status() < 400) return;
      const request = response.request();
      const staleScenario = expectedStaleApply;
      if (response.status() === 409 && staleScenario?.active && staleScenario.requestSeen && isNotificationsApplyRequest(request) && staleScenario.responseCount === 0) {
        staleScenario.responseCount += 1;
        staleScenario.responseStatus = response.status();
        return;
      }
      errors.push(`HTTP ${response.status()} ${request.method()} ${new URL(request.url()).pathname}`);
    });
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

    // Background acceptance is independent of the existing Current IA route checks.
    for (const locale of locales) {
      for (const viewport of backgroundViewports) {
        await page.setViewportSize({ width: viewport.width, height: viewport.height });
        await page.goto(`${base}/bot/login?lang=${locale.lang}`, { waitUntil: 'networkidle' });
        if (locale === locales[0] && viewport === backgroundViewports[0]) {
          const iconHref = await page.locator('link[rel="icon"]').first().getAttribute('href');
          if (iconHref !== '/favicon.ico') throw new Error(`page icon link is unexpected: ${iconHref}`);
          const iconLinks = await page.locator('link[rel="icon"]').evaluateAll(nodes => nodes.map(node => ({ href: node.getAttribute('href'), type: node.getAttribute('type') })));
          if (!iconLinks.some(icon => icon.href === '/kitsusync.svg' && icon.type === 'image/svg+xml')) throw new Error('page does not reference the supplied KitsuSync SVG icon');
          const response = await page.request.get(`${base}${iconHref}`);
          const favicon = { status: response.status(), type: response.headers()['content-type'], bytes: (await response.body()).byteLength };
          if (favicon.status !== 200 || favicon.type !== 'image/x-icon' || favicon.bytes === 0) throw new Error(`favicon response is invalid: ${JSON.stringify(favicon)}`);
          records.push({ route: '/favicon.ico', locale: locale.lang, viewport: viewport.name, state: 'served', detail: `HTTP ${favicon.status}; ${favicon.type}; ${favicon.bytes} bytes` });
          const svgIcon = await page.request.get(`${base}/kitsusync.svg`);
          if (svgIcon.status() !== 200 || svgIcon.headers()['content-type'] !== 'image/svg+xml' || !(await svgIcon.text()).includes('fill: #fc8742')) throw new Error('supplied KitsuSync SVG icon did not serve correctly');
        }
        await assertBackgroundCanvas(page, 'login-fabric', locale, viewport, `login-background-${locale.lang}-${viewport.name}.png`);
        await assertLoginFabricGoldStandard(page, locale, viewport);
      }
    }
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(`${base}/bot/login?lang=en`, { waitUntil: 'networkidle' });
    const loginCard = await page.locator('.login-card').boundingBox();
    const loginPointer = { x: Math.max(5, loginCard.x - 58), y: loginCard.y + loginCard.height * .5 };
    await page.mouse.move(0, 0);
    await page.waitForTimeout(1000);
    const clothY = await fabricPeakY(page, loginPointer.x);
    if (clothY === null) throw new Error('login fabric has no measurable cloth near the card edge');
    const farY = clothY > 450 ? 8 : 892;
    await page.mouse.move(loginPointer.x, farY);
    await page.waitForTimeout(1300);
    const falseTrigger = await page.locator('canvas[data-background="login-fabric"]').evaluate(canvas => Number(canvas.dataset.maxPointerGust));
    if (falseTrigger > .01) throw new Error(`same-X pointer far from the cloth triggered a gust (${falseTrigger})`);
    await page.mouse.move(loginPointer.x, clothY);
    await page.waitForTimeout(1300);
    const trueTrigger = await page.locator('canvas[data-background="login-fabric"]').evaluate(canvas => Number(canvas.dataset.maxPointerGust));
    const localAfter = await localCanvasAlpha(page, loginPointer.x, clothY, 42);
    if (trueTrigger < .10 || localAfter < 1) throw new Error(`pointer near cloth did not produce a local gust (${falseTrigger} -> ${trueTrigger}; rendered activity ${localAfter})`);
    records.push({ route: '/bot/login', locale: 'en', viewport: 'desktop-1440', state: '2D local pointer gust', detail: `same-X far pointer max gust=${falseTrigger}; pointer at cloth peak y=${clothY} max gust=${trueTrigger}; local rendered activity=${localAfter}; no repulsion` });

    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.waitForTimeout(100);
    const reducedLogin = await page.locator('canvas[data-background="login-fabric"]').evaluate(canvas => canvas.toDataURL());
    await page.waitForTimeout(500);
    if (await page.locator('canvas[data-background="login-fabric"]').evaluate(canvas => canvas.toDataURL()) !== reducedLogin) {
      throw new Error('login fabric continued animating under reduced-motion preference');
    }
    records.push({ route: '/bot/login', locale: 'en', viewport: 'desktop-1440', state: 'reduced motion', detail: 'canvas frame remained stable with reduced motion enabled' });
    await page.emulateMedia({ reducedMotion: 'no-preference' });

    // Enter through the ordinary protected route and complete the ordinary login form.
    await gotoWFARecipients(page, locales[0]);
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

    // Production-detail IA acceptance: this stays in the same authenticated
    // Chromium context and uses only the isolated fixture database/services.
    for (const locale of locales) {
      for (const viewport of viewports) {
        await page.setViewportSize({ width: viewport.width, height: viewport.height });
        await gotoProduction(page, locale, 'overview');
        const tabLinks = page.locator('.production-tabs [role="tab"]');
        const tabLabels = (await tabLinks.allTextContents()).map(text => text.trim());
        if (tabLabels.length !== 4 || JSON.stringify(tabLabels) !== JSON.stringify(locale.tabs)) {
          throw new Error(`Production primary navigation is not the four-section ${locale.lang} set: ${JSON.stringify(tabLabels)}`);
        }
        if (await page.locator('.production-identity').count() !== 1 || (await page.locator('.production-identity').innerText()).includes('Selected Production')) {
          throw new Error(`Production identity header is redundant or missing in ${locale.lang}`);
        }
        const tabTreatment = await page.locator('.production-tabs [role="tab"].active').evaluate(node => {
          const tab = getComputedStyle(node);
          const underline = getComputedStyle(node, '::after');
          const nav = getComputedStyle(node.closest('.production-tabs'));
          return {
            tabBackground: tab.backgroundColor,
            tabRadius: tab.borderRadius,
            tabShadow: tab.boxShadow,
            underlineContent: underline.content,
            underlineHeight: underline.height,
            underlineBottom: underline.bottom,
            navBorderStyle: nav.borderBottomStyle,
          };
        });
        if (tabTreatment.tabBackground !== 'rgba(0, 0, 0, 0)' || tabTreatment.tabRadius !== '0px' || tabTreatment.tabShadow !== 'none' ||
            tabTreatment.underlineContent !== '""' || tabTreatment.underlineHeight !== '2px' || tabTreatment.underlineBottom !== '-7px' || tabTreatment.navBorderStyle !== 'solid') {
          throw new Error(`Production tabs are not plain labels on a shared hairline/underline in ${locale.lang}: ${JSON.stringify(tabTreatment)}`);
        }
        if (await page.locator('.production-summary-card,.production-summary-grid').count()) throw new Error('Overview retained metric-card UI');
        const overview = await page.locator('#panel-overview').innerText();
        for (const expected of [locale.lang === 'ja' ? '状態' : 'Status', locale.lang === 'ja' ? '現在の問題' : 'Current issues', 'Storyboard']) {
          if (!overview.includes(expected)) throw new Error(`Overview is missing real status/activity ${expected} in ${locale.lang}`);
        }
        if (overview.includes('Must not leak') || overview.includes('Current issues (0)')) throw new Error('Overview leaked cross-Production activity or invented a count');
        const overviewSections = await page.locator('.production-overview > .production-settings-section').evaluateAll(sections => sections.map(section => {
          const style = getComputedStyle(section);
          return { borderTopStyle: style.borderTopStyle, radius: style.borderRadius, background: style.backgroundColor, shadow: style.boxShadow };
        }));
        if (overviewSections.length !== 3 || overviewSections[0].borderTopStyle !== 'none' || overviewSections.slice(1).some(section => section.borderTopStyle !== 'solid' || section.radius !== '0px' || section.background !== 'rgba(0, 0, 0, 0)' || section.shadow !== 'none')) {
          throw new Error(`Overview sections are not a compact divider hierarchy in ${locale.lang}: ${JSON.stringify(overviewSections)}`);
        }
        await page.screenshot({ path: path.join(output, `production-overview-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await page.screenshot({ path: path.join(output, `production-rich-overview-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/projects?tab=overview', locale.lang, viewport.name, 'overview', 'compact status, Current Issues and exact-Production Recent Activity sections remain visible');

        await gotoProduction(page, locale, 'notifications');
        const routingHeadings = (await page.locator('.production-notification-table thead th').allTextContents()).map(text => text.trim());
        if (JSON.stringify(routingHeadings) !== JSON.stringify(['Kitsu Task Type', locale.lang === 'ja' ? 'Discordチャンネル' : 'Discord Channel', locale.lang === 'ja' ? 'WFA通知先' : 'WFA recipients'])) {
          throw new Error(`Notifications routing columns are missing or concatenated in ${locale.lang}: ${JSON.stringify(routingHeadings)}`);
        }
        const compReadRow = page.locator('.production-notification-table tbody tr[data-task-type-id="task-comp"]');
        if (!(await compReadRow.innerText()).includes('#compositing')) throw new Error(`real seeded destination is missing in ${locale.lang}`);
        if (await page.locator('[data-current-routing-form]').count()) throw new Error('Notifications read mode exposed routing edit controls');
        const editLink = page.getByRole('link', { name: locale.lang === 'ja' ? '編集' : 'Edit', exact: true });
        if (await editLink.count() !== 1) throw new Error(`Notifications should show one edit entry point in ${locale.lang}`);
        if (await page.locator('#notification-preview,.discord-message-preview,[data-notification-preview-select]').count()) throw new Error(`Removed Notification Preview returned in ${locale.lang}`);
        if (await page.locator('.production-wfa-recipients').count()) throw new Error(`Notifications view retained the former standalone Reviewer section in ${locale.lang}`);
        if (!(await compReadRow.innerText()).includes(locale.automatic) || !(await compReadRow.innerText()).includes(locale.overrides)) {
          throw new Error(`Notifications is missing Automatic or Additional recipients in ${locale.lang}`);
        }
        const notificationColumnWidths = await page.locator('.production-notification-table col').evaluateAll(nodes => nodes.map(node => getComputedStyle(node).width));
        if (notificationColumnWidths.length !== 3 || notificationColumnWidths.some(width => parseFloat(width) <= 0)) throw new Error(`Notifications lost stable three-column geometry in ${locale.lang}: ${JSON.stringify(notificationColumnWidths)}`);
        if (await compReadRow.locator('.production-wfa-kind').count() !== 2 || await compReadRow.locator('.production-wfa-value').count() !== 2) throw new Error(`WFA Automatic/Additional summaries are not distinct sub-rows in ${locale.lang}`);
        const notificationSections = await page.locator('.production-notifications > .production-settings-section').evaluateAll(sections => sections.map(section => {
          const style = getComputedStyle(section);
          return { borderTopStyle: style.borderTopStyle, radius: style.borderRadius, background: style.backgroundColor, shadow: style.boxShadow };
        }));
        if (notificationSections.length !== 1 || notificationSections[0].borderTopStyle !== 'none' || notificationSections[0].radius !== '0px' || notificationSections[0].background !== 'rgba(0, 0, 0, 0)' || notificationSections[0].shadow !== 'none') {
          throw new Error(`Notifications table is not a compact flat section in ${locale.lang}: ${JSON.stringify(notificationSections)}`);
        }
        await page.screenshot({ path: path.join(output, `production-notifications-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await page.screenshot({ path: path.join(output, `production-rich-notifications-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/projects?tab=notifications', locale.lang, viewport.name, 'routing and WFA recipients', 'one compact Task Type → Discord Channel → Automatic/Additional summary table; no synthetic Notification Preview');

        await gotoProduction(page, locale, 'notifications', '&edit_routing=1');
        if (await page.locator('[data-current-routing-form]').count() !== 1 || !(await page.locator('[data-current-routing-form]').innerText()).includes(locale.lang === 'ja' ? '変更を適用' : 'Apply')) {
          throw new Error(`explicit Notifications edit mode did not preserve the existing routing form in ${locale.lang}`);
        }
        if (await page.locator('[data-current-routing-form] select[name="task_type_id"]').count() === 0 || await page.locator('[data-current-routing-form] select[name="destination_webhook_id"]').count() === 0) {
          throw new Error(`routing edit mode lost Task Type or Channel controls in ${locale.lang}`);
        }
        const editForm = page.locator('[data-current-routing-form]');
        const automaticPanel = editForm.locator('[data-wfa-automatic]');
        if (await automaticPanel.locator('input,button,select').count()) throw new Error('Automatic WFA recipients are editable');
        await editForm.locator('[data-select-task]').filter({ hasText: 'Animation' }).click();
        if (!(await editForm.locator('[data-wfa-title]').innerText()).includes('Animation') || !(await automaticPanel.innerText()).includes(animationAutomatic[0])) {
          throw new Error(`selecting a route did not select its matching Automatic WFA summary in ${locale.lang}`);
        }
        await editForm.locator('[data-select-task]').filter({ hasText: 'Compositing' }).click();
        await assertAutomatic(page, locale, compositingAutomatic);

        await editForm.locator('[data-wfa-add-target]').click();
        const addModal = page.locator('[data-wfa-add-modal]');
        const candidateUsers = addModal.locator('[data-wfa-user-id]');
        for (const blocked of ['22222222222222232', '22222222222222233', '22222222222222235']) {
          if (await candidateUsers.locator(`option[value="${blocked}"]`).count()) throw new Error(`ineligible User Override ${blocked} is selectable`);
        }
        if (!(await candidateUsers.locator('option[value="22222222222222234"]').count())) throw new Error('linked Team Guild member is unavailable as an override');
        const candidateRoles = addModal.locator('[data-wfa-role-id]');
        if (!(await candidateRoles.locator('option[value="33333333333333331"]').count())) throw new Error('mentionable Guild Role is not selectable');
        for (const blocked of [guildID, '33333333333333332']) {
          if (await candidateRoles.locator(`option[value="${blocked}"]`).count()) throw new Error(`@everyone/non-mentionable Role ${blocked} is selectable`);
        }
        await addModal.locator('[data-wfa-user-id]').selectOption('22222222222222225');
        await addModal.locator('[data-wfa-add-confirm]').click();
        await editForm.locator('[data-wfa-add-target]').click();
        await addModal.locator('[data-wfa-role-id]').selectOption('33333333333333331');
        await addModal.locator('[data-wfa-add-confirm]').click();
        const additionalPanel = editForm.locator('[data-wfa-additional]');
        const additionalText = await additionalPanel.innerText();
        if (!additionalText.includes('Guild Nick Override') || !additionalText.includes('Artist Global') || !additionalText.includes('@Reviewers')) throw new Error(`Additional User/Role overrides were not presented together in ${locale.lang}`);
        if (await additionalPanel.locator('[data-wfa-remove-target]').count() !== 3) throw new Error('User and Role pending removals were not available');

        await editForm.locator('[data-routing-add]').click();
        const newTypeSelect = editForm.locator('[data-routing-new-row] select[name="task_type_id"]');
        await newTypeSelect.selectOption('task-unassigned');
        const pendingRow = editForm.locator('[data-routing-row][data-task-type="task-unassigned"]');
        if (!(await automaticPanel.innerText()).includes(locale.lang === 'ja' ? '該当するSupervisorはいません' : 'No matching Supervisor')) {
          throw new Error(`new Task Type did not receive an immediate truthful Automatic summary in ${locale.lang}`);
        }
        await editForm.locator('[data-wfa-add-target]').click();
        await addModal.locator('[data-wfa-user-id]').selectOption('22222222222222234');
        await addModal.locator('[data-wfa-add-confirm]').click();
        await pendingRow.locator('.routing-row-menu summary').click();
        await pendingRow.locator('[data-routing-remove]').click();
        if (!(await pendingRow.locator('[data-routing-undo]').isVisible())) throw new Error('pending route removal did not offer Undo');

        let submittedApply = null;
        expectedStaleApply = { active: true, requestSeen: false, responseCount: 0, responseStatus: null, console409Messages: [] };
        const applyEndpoint = new URL('/bot/admin/projects?apply_notifications=1', base).href;
        await page.route(applyEndpoint, async route => {
          if (!expectedStaleApply?.active || !isNotificationsApplyRequest(route.request()) || expectedStaleApply.requestSeen) {
            errors.push('Unexpected or duplicate Notifications Apply request during stale-edit interception');
            await route.continue();
            return;
          }
          expectedStaleApply.requestSeen = true;
          submittedApply = route.request().postDataJSON();
          await route.fulfill({ status: 409, contentType: 'application/json', body: '{"error":"stale_edit"}' });
        });
        const urlBeforeApply = page.url();
        const staleApplyResponsePromise = page.waitForResponse(response => isNotificationsApplyRequest(response.request()));
        await editForm.locator('[data-apply-submit]').click();
        const staleApplyResponse = await staleApplyResponsePromise;
        if (staleApplyResponse.status() !== 409 || expectedStaleApply.responseCount !== 1 || expectedStaleApply.responseStatus !== 409) {
          throw new Error(`Stale Notifications Apply did not produce exactly one expected HTTP 409 in ${locale.lang}: status=${staleApplyResponse.status()} tracked=${expectedStaleApply.responseCount}`);
        }
        if (!submittedApply || submittedApply.reviewer_changes.some(change => change.task_type_id === 'task-unassigned')) {
          throw new Error('Apply sent a reviewer delta for a route pending removal');
        }
        const staleMessage = locale.lang === 'ja' ? '別の編集が保存されました' : 'Another edit was saved';
        await page.waitForFunction(({ selector, expected }) => document.querySelector(selector)?.textContent.includes(expected), {
          selector: '[data-apply-message]',
          expected: staleMessage,
        }, { timeout: 5000 });
        if (page.url() !== urlBeforeApply) throw new Error(`HTTP 409 navigated away from the pending editor in ${locale.lang}`);
        if (!(await pendingRow.isVisible()) || !(await pendingRow.locator('[data-routing-undo]').isVisible())) {
          throw new Error(`HTTP 409 discarded the pending route removal in ${locale.lang}`);
        }
        await editForm.locator('[data-routing-row][data-task-type="task-comp"] [data-select-task]').click();
        const preservedTargets = await additionalPanel.innerText();
        for (const expected of ['Guild Nick Override', 'Artist Global', '@Reviewers']) {
          if (!preservedTargets.includes(expected)) throw new Error(`HTTP 409 discarded pending User/Role target ${expected} in ${locale.lang}`);
        }
        if (expectedStaleApply.console409Messages.length > 1) {
          errors.push(...expectedStaleApply.console409Messages.slice(1));
        }
        expectedStaleApply.console409Consumed = expectedStaleApply.console409Messages.length === 1;
        expectedStaleApply.active = false;
        await pendingRow.locator('[data-routing-undo]').click();
        await pendingRow.locator('[data-select-task]').click();
        await page.screenshot({ path: path.join(output, `production-notifications-edit-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await page.unroute(applyEndpoint);
        await editForm.locator('[data-pending-cancel]').click();
        if (await page.locator('.production-notification-table tbody tr[data-task-type-id="task-unassigned"]').count()) throw new Error('Cancel persisted the pending route addition');
        await record(page, '/bot/admin/projects?tab=notifications&edit_routing=1', locale.lang, viewport.name, 'pending routing/WFA edit and stale Apply', 'select/add/remove/undo and additional User/Role edits stayed local; 409 retained pending state; Cancel discarded it');

        await gotoProduction(page, locale, 'notifications', '&edit_routing=1');
        const refreshForm = page.locator('[data-current-routing-form]');
        await refreshForm.locator('[data-routing-add]').click();
        await refreshForm.locator('[data-routing-new-row] select[name="task_type_id"]').selectOption('task-unassigned');
        await page.reload({ waitUntil: 'networkidle' });
        if (page.url().includes('edit_routing=1') && await page.locator('[data-current-routing-form] [data-routing-row][data-task-type="task-unassigned"]').count()) {
          throw new Error('Refresh persisted a browser-only pending route addition');
        }

        await gotoProduction(page, locale, 'team');
        const teamTab = page.locator('#panel-team');
        if (await page.locator('.production-team-page').count() !== 1 || await page.locator('.production-wfa-recipients').count()) {
          throw new Error(`Team is not a separate read-only view in ${locale.lang}`);
        }
        if (await page.locator('form input[name="action"][value*="production_member"],form input[name="action"][value*="production_role"]').count()) throw new Error('Team exposed membership or role mutation');
        if ((await teamTab.innerText()).includes('Example task') || (await teamTab.innerText()).includes('Assigned')) throw new Error('Team inferred task assignments from Department membership');
        const teamText = await teamTab.innerText();
        for (const expected of locale.lang === 'ja' ? ['チーム', 'Supervisor範囲', '未リンク'] : ['Team', 'Supervision scope', 'Not linked']) {
          if (!teamText.includes(expected)) throw new Error(`Team view is missing ${expected} in ${locale.lang}`);
        }
        const teamHeaders = (await page.locator('.production-team-table thead th').allTextContents()).map(text => text.trim());
        const expectedTeamHeaders = locale.lang === 'ja'
          ? ['メンバー', 'Kitsuロール', 'Department', 'Discord', '状態 / 操作']
          : ['Member', 'Kitsu role', 'Department', 'Discord', 'Status / action'];
        if (JSON.stringify(teamHeaders) !== JSON.stringify(expectedTeamHeaders)) {
          throw new Error(`Team table does not match the five-column structure in ${locale.lang}: ${JSON.stringify(teamHeaders)}`);
        }
        const userLinkingHref = await page.locator('.production-team-user-link').first().getAttribute('href');
        if (!userLinkingHref || !userLinkingHref.startsWith(`/bot/admin/users?lang=${locale.lang}`)) {
          throw new Error(`Unlinked Team member has no localized User Linking route in ${locale.lang}`);
        }
        if (viewport.name === 'mobile') {
          const teamControls = await page.locator('.production-team-discord,.production-team-status').evaluateAll(cells => cells.flatMap(cell => {
            const cellRight = cell.getBoundingClientRect().right;
            return [...cell.querySelectorAll('.status-pill, button')].map(control => ({
              label: control.textContent.trim(),
              right: Math.round(control.getBoundingClientRect().right),
              cellRight: Math.round(cellRight),
            }));
          }));
          const overflow = teamControls.find(control => control.right > control.cellRight + 1);
          if (overflow) throw new Error(`Team status/action overflows its Discord cell at 390px in ${locale.lang}: ${JSON.stringify(overflow)}`);
        }
        const teamRows = await page.locator('.production-team-row').evaluateAll(rows => rows.map(row => {
          const style = getComputedStyle(row);
          const firstCell = row.querySelector('th,td');
          return { display: style.display, borderTopStyle: style.borderTopStyle, firstCellBorderTopStyle: firstCell && getComputedStyle(firstCell).borderTopStyle, radius: style.borderRadius, background: style.backgroundColor, shadow: style.boxShadow };
        }));
        const expectedTeamDisplay = viewport.name === 'mobile' ? 'grid' : 'table-row';
        if (!teamRows.length || teamRows.some(row => row.display !== expectedTeamDisplay) || teamRows[0].firstCellBorderTopStyle !== 'none' || teamRows.slice(1).some(row => (viewport.name === 'mobile' ? row.borderTopStyle : row.firstCellBorderTopStyle) !== 'solid') || teamRows.some(row => row.radius !== '0px' || row.background !== 'rgba(0, 0, 0, 0)' || row.shadow !== 'none')) {
          throw new Error(`Production Team is not a compact responsive table in ${locale.lang}: ${JSON.stringify(teamRows)}`);
        }
        const teamColumnWidths = await page.locator('.production-team-table col').evaluateAll(nodes => nodes.map(node => getComputedStyle(node).width));
        if (teamColumnWidths.length !== 5 || teamColumnWidths.some(width => parseFloat(width) <= 0)) throw new Error(`Team lost stable five-column geometry in ${locale.lang}: ${JSON.stringify(teamColumnWidths)}`);
        const teamLinkButton = page.locator('[data-open-team-link]').first();
        if (!(await teamLinkButton.count())) throw new Error(`Team has no verified in-place global User Linking action in ${locale.lang}`);
        const teamLinkModal = page.locator('[data-team-link-modal]');
        const teamUrlBeforeLink = page.url();
        await teamLinkButton.click();
        if (!(await teamLinkModal.isVisible()) || !(await teamLinkModal.locator('select[name="discord_user_id"] option:not([value=""])').count())) {
          throw new Error(`Team global User Linking modal did not open with verified Guild choices in ${locale.lang}`);
        }
        await teamLinkModal.locator('[data-team-link-cancel]').click();
        if (await teamLinkModal.isVisible() || page.url() !== teamUrlBeforeLink) {
          throw new Error(`Cancel did not close the Team User Linking modal without navigation in ${locale.lang}`);
        }
        await page.screenshot({ path: path.join(output, `production-team-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await page.screenshot({ path: path.join(output, `production-rich-team-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/projects?tab=team', locale.lang, viewport.name, 'live Production Team', 'Kitsu Team, effective role, derived Supervisor scope, and global User Linking state; no local membership editor');

        await gotoProduction(page, locale, 'settings');
        const settings = await page.locator('#panel-settings').innerText();
        const settingsPositions = ['Storage', locale.lang === 'ja' ? '技術情報' : 'Technical details', locale.lang === 'ja' ? '診断' : 'Diagnostics', 'Danger Zone'].map(label => settings.indexOf(label));
        if (settingsPositions.some(position => position < 0) || settingsPositions.some((position, index) => index > 0 && position <= settingsPositions[index - 1])) {
          throw new Error(`Settings sections are missing or out of order in ${locale.lang}: ${JSON.stringify(settingsPositions)}`);
        }
        const settingsLayout = await page.locator('.production-settings-list').evaluate(node => {
          const style = getComputedStyle(node);
          const sections = [...node.querySelectorAll(':scope > .production-settings-section')];
          return { display: style.display, columns: style.gridTemplateColumns.trim().split(/\s+/).length, gap: style.rowGap, sections: sections.length, paddings: sections.map(section => parseFloat(getComputedStyle(section).paddingTop)), summaryHeights: [...node.querySelectorAll('details > summary')].map(summary => summary.getBoundingClientRect().height) };
        });
        if (settingsLayout.display !== 'grid' || settingsLayout.columns !== 1 || settingsLayout.gap !== '0px' || settingsLayout.sections !== 4 || settingsLayout.paddings.some(value => value < 20) || settingsLayout.summaryHeights.some(value => value < 44)) {
          throw new Error(`Settings are not a compact four-section vertical layout in ${locale.lang}: ${JSON.stringify(settingsLayout)}`);
        }
        if (await page.locator('#technical-details[open],#diagnostics[open],#danger-zone[open]').count()) throw new Error(`Settings disclosures must start collapsed in ${locale.lang}`);
        const saveButton = page.locator('.drive-storage-form [data-drive-save]');
        const storageInput = page.locator('#storage-url');
        const originalStorage = await storageInput.inputValue();
        if (!(await saveButton.isDisabled())) throw new Error(`Storage Save should start disabled in ${locale.lang}`);
        await storageInput.fill(`${originalStorage}/changed`);
        if (await saveButton.isDisabled()) throw new Error(`Storage Save did not enable after an actual edit in ${locale.lang}`);
        await storageInput.fill(originalStorage);
        if (!(await saveButton.isDisabled())) throw new Error(`Storage Save did not disable when the original value was restored in ${locale.lang}`);

        const technicalIndent = await page.locator('#technical-details').evaluate(node => ({
          summary: parseFloat(getComputedStyle(node.querySelector('summary')).paddingInlineStart),
          content: parseFloat(getComputedStyle(node.querySelector('.production-technical-details')).marginInlineStart),
        }));
        if (technicalIndent.summary < 12 || technicalIndent.content < technicalIndent.summary) throw new Error(`Technical details indentation is not hierarchical: ${JSON.stringify(technicalIndent)}`);
        await page.locator('#technical-details summary').click();
        if (!(await page.locator('#technical-details[open]').count())) throw new Error(`Technical details did not expand in ${locale.lang}`);
        const copyButtons = page.locator('#technical-details .production-copy-id');
        if (await copyButtons.count() !== 3) throw new Error(`Technical identifiers need three copy controls in ${locale.lang}`);
        const productionCopy = copyButtons.nth(0);
        const copyLabel = locale.lang === 'ja' ? 'コピーしました' : 'Copied';
        await productionCopy.click();
        const clipboardText = await page.evaluate(() => navigator.clipboard.readText());
        if (clipboardText !== 'reviewer-production' || (await productionCopy.innerText()) !== copyLabel) {
          throw new Error(`Production ID copy action failed in ${locale.lang}: button=${await productionCopy.innerText()}`);
        }
        await page.locator('#diagnostics > summary').click();
        if (!(await page.locator('#diagnostics[open]').count()) || await page.locator('#diagnostics details[open]').count()) throw new Error(`Diagnostics did not expand in a compact collapsed-details state in ${locale.lang}`);
        await page.locator('#danger-zone summary').click();
        if (!(await page.locator('#danger-zone[open]').count())) throw new Error(`Danger Zone did not expand in ${locale.lang}`);
        await page.screenshot({ path: path.join(output, `production-settings-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await page.screenshot({ path: path.join(output, `production-rich-settings-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/projects?tab=settings', locale.lang, viewport.name, 'settings and disclosures', `section order preserved; Save is change-sensitive; technical indentation=${JSON.stringify(technicalIndent)}`);

        const legacyCases = [
          { query: 'storage-settings', panel: 'settings', target: '#storage', expanded: null },
          { query: 'activity', panel: 'overview', target: '#recent-activity', expanded: null },
          { query: 'troubleshooting', panel: 'settings', target: '#diagnostics', expanded: '#diagnostics' },
          { query: 'advanced', panel: 'settings', target: '#technical-details', expanded: '#technical-details' },
          { query: 'danger-zone', panel: 'settings', target: '#danger-zone', expanded: '#danger-zone' },
          { query: 'users', panel: 'team', target: '#production-team', expanded: null },
          { query: 'user-settings', panel: 'team', target: '#production-team', expanded: null },
          { query: 'reviewers', panel: 'notifications', target: '#wfa-recipients', expanded: null },
        ];
        for (const legacy of legacyCases) {
          await page.goto(`${base}/bot/admin/projects?project=reviewer-production&tab=${legacy.query}&lang=${locale.lang}`, { waitUntil: 'networkidle' });
          if (await page.locator(`#panel-${legacy.panel}`).count() !== 1 || await page.locator(legacy.target).count() !== 1) {
            throw new Error(`legacy Production destination ${legacy.query} did not map to ${legacy.panel}/${legacy.target}`);
          }
          if (legacy.expanded && !(await page.locator(legacy.expanded).evaluate(node => node.open))) throw new Error(`legacy Production disclosure ${legacy.query} did not open`);
          if (legacy.query !== 'users' && legacy.query !== 'user-settings') {
            const isFocused = await page.evaluate(selector => {
              const target = document.querySelector(selector);
              return !!target && (document.activeElement === target || target.querySelector('summary') === document.activeElement);
            }, legacy.target);
            if (!isFocused) throw new Error(`legacy Production destination ${legacy.query} did not receive focus`);
          }
          await record(page, `/bot/admin/projects?tab=${legacy.query}`, locale.lang, viewport.name, 'legacy route mapped', `mapped to ${legacy.panel}; destination ${legacy.target}`);
        }

        await page.goto(`${base}/bot/admin/health?lang=${locale.lang}`, { waitUntil: 'networkidle' });
        const systemIndent = await page.locator('.pipeline-health-diagnostic').first().evaluate(node => ({
          summary: parseFloat(getComputedStyle(node.querySelector('summary')).paddingInlineStart),
          parent: parseFloat(getComputedStyle(node).marginInlineStart),
          content: parseFloat(getComputedStyle(node.querySelector('.pipeline-health-diagnostic-content')).marginInlineStart),
        }));
        if (systemIndent.parent < 12 || systemIndent.content < systemIndent.parent) throw new Error(`System Status disclosure indentation is not hierarchical: ${JSON.stringify(systemIndent)}`);
        await record(page, '/bot/admin/health', locale.lang, viewport.name, 'diagnostic disclosure indent', JSON.stringify(systemIndent));
      }
    }

    await gotoWFARecipients(page, locales[0]);
    if (await page.locator('#tab-reviewers').count() || await page.locator('.production-tabs [role="tab"]').filter({ hasText: /^Reviewers$/ }).count()) throw new Error('Reviewers remains a primary Production tab');
    await assertAutomatic(page, locales[0], compositingAutomatic, [
      '@Departmentless Global', '@Wrong Department Global', '@Manager Global', '@Admin Global',
      '@Demoted Global', '@Project Manager Global', '@position-only', '@inactive', '@kitsu-bot',
    ]);
    const productionIdentity = await page.locator('.production-identity').innerText();
    if (!productionIdentity.includes('Synthetic Review Production')) throw new Error('Production identity header is missing the selected Production name');
    await gotoProduction(page, locales[0], 'team');
    const teamText = await page.locator('#production-team').innerText();
    for (const expected of ['Global Admin', 'Guild Nick Supervisor', 'Global Name Fallback', '@username-fallback']) {
      if (!teamText.includes(expected)) throw new Error(`Production Team view is missing ${expected}`);
    }
    for (const stale of ['Automatic inactive while overridden', 'Add Production member', 'Remove Production member', 'Reviewer / Checker task types', 'Production Manager fallback', 'global CheckerMap']) {
      if (teamText.includes(stale)) throw new Error(`stale/manual Reviewer copy remains: ${stale}`);
    }

    const states = [
      { name: 'empty-team', expected: 'The Kitsu Production Team is empty', team: true },
      { name: 'team-failure', expected: 'Could not load the Kitsu Production Team', team: true },
      { name: 'no-matching', expected: 'No matching Supervisor.' },
      { name: 'no-linked', expected: 'No matching Supervisor.' },
      { name: 'discord-failure', expected: 'Discord membership could not be verified.' },
      { name: 'stale-membership', expected: 'No matching Supervisor.' },
    ];
    for (const state of states) {
      await fixture(page, state.name);
      if (state.team) {
        await gotoProduction(page, locales[0], 'team');
        if (!(await page.locator('main').innerText()).includes(state.expected)) throw new Error(`${state.name} Team state is unclear`);
      } else {
        await gotoWFARecipients(page, locales[0]);
        const text = await page.locator('.production-notification-table tbody tr[data-task-type-id="task-comp"]').innerText();
        if (!text.includes(state.expected)) throw new Error(`${state.name} Automatic state is unclear: ${text}`);
      }
      await record(page, state.team ? '/bot/admin/projects?tab=team' : '/bot/admin/projects?tab=notifications', 'en', 'desktop', state.name, `visible fail-closed state: ${state.expected}`);
    }
    await fixture(page, 'no-roles');
    await gotoWFARecipients(page, locales[0], '&edit_routing=1');
    await page.locator('[data-wfa-add-target]').click();
    const emptyRoleModal = page.locator('[data-wfa-add-modal]');
    if (!(await emptyRoleModal.innerText()).includes('No mentionable Discord roles are available')) throw new Error('No-role empty state is not explained in the Add recipient dialog');
    if (await emptyRoleModal.locator('[data-wfa-role-id] option[value="33333333333333331"]').count()) throw new Error('no-roles scenario left a Role target available');
    await emptyRoleModal.locator('[data-wfa-add-cancel]').click();
    await fixture(page, 'ready');

    // JP/EN and desktop/mobile acceptance for Production Users, User Linking, and System Status.
    for (const locale of locales) {
      for (const viewport of viewports) {
        await page.setViewportSize({ width: viewport.width, height: viewport.height });

        await page.goto(`${base}/bot/admin?lang=${locale.lang}`, { waitUntil: 'networkidle' });
        if (!(await page.locator('main').innerText()).trim()) throw new Error(`Dashboard did not render in ${locale.lang}`);
        await page.screenshot({ path: path.join(output, `dashboard-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin', locale.lang, viewport.name, 'ready', 'Dashboard rendered without overflow or mojibake');

        if (viewport.name === 'desktop') {
          for (const backgroundViewport of backgroundViewports) {
            await page.setViewportSize({ width: backgroundViewport.width, height: backgroundViewport.height });
            await page.goto(`${base}/bot/admin?lang=${locale.lang}`, { waitUntil: 'networkidle' });
            await assertBackgroundCanvas(page, 'app-dots', locale, backgroundViewport, `app-background-${locale.lang}-${backgroundViewport.name}.png`);
          }
          await page.setViewportSize({ width: viewport.width, height: viewport.height });
          await page.goto(`${base}/bot/admin?lang=${locale.lang}`, { waitUntil: 'networkidle' });
        }

        await gotoWFARecipients(page, locale);
        if (!(await page.locator('.production-notification-table').count())) throw new Error(`Notifications table missing in ${locale.lang}`);
        await assertAutomatic(page, locale, compositingAutomatic);
        if (await page.locator('#tab-reviewers').count()) throw new Error(`Reviewers is still a primary tab in ${locale.lang}`);
        await page.screenshot({ path: path.join(output, `wfa-recipients-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/projects?tab=notifications', locale.lang, viewport.name, 'ready', 'WFA Automatic and additional recipients rendered without overflow or mojibake');

        await gotoProduction(page, locale, 'team');
        if (!(await page.locator('#production-team').count())) throw new Error(`Production Team view missing in ${locale.lang}`);
        if (await page.locator('form input[name="action"][value*="production_member"],form input[name="action"][value*="production_role"]').count()) throw new Error(`Team mutation controls appeared in ${locale.lang}`);
        await page.screenshot({ path: path.join(output, `production-team-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/projects?tab=team', locale.lang, viewport.name, 'ready', 'read-only live Team view and derived Supervisor scope rendered without overflow or mojibake');

        await page.goto(`${base}/bot/admin/users?lang=${locale.lang}`, { waitUntil: 'networkidle' });
        if (!(await page.locator('#global-discord-guild').count())) throw new Error(`User Linking server selector missing in ${locale.lang}`);
        if (await page.locator('.user-linking-table').count()) throw new Error(`User Linking table appeared before explicit server selection in ${locale.lang}`);
        if ((await page.locator('.user-linking-select-hint').innerText()).trim() !== (locale.lang === 'ja' ? 'Discordサーバーを選択してください。' : 'Select a Discord server first.')) throw new Error(`User Linking selection prompt is not concise in ${locale.lang}`);
        if (await page.locator('.user-linking-server-select').evaluate(node => getComputedStyle(node).borderBottomWidth !== '0px')) throw new Error(`unselected User Linking has a bottom divider in ${locale.lang}`);
        await page.screenshot({ path: path.join(output, `user-linking-${locale.lang}-${viewport.name}-unselected.png`), fullPage: true });
        await record(page, '/bot/admin/users', locale.lang, viewport.name, 'server unselected', 'compact server selector rendered without an unselected-state mapping table');

        await page.goto(`${base}/bot/admin/users?lang=${locale.lang}&discord_guild_id=${guildID}`, { waitUntil: 'networkidle' });
        if (!(await page.locator('.user-linking-table').count())) throw new Error(`User Linking table missing in ${locale.lang}`);
        const selectedContext = await page.locator('.user-linking-directory').innerText();
        if (/Showing Discord server|表示中のDiscordサーバー/.test(selectedContext)) throw new Error(`User Linking repeats the selected server context in ${locale.lang}`);
        if (await page.locator('.user-linking-directory').evaluate(node => getComputedStyle(node).borderBottomWidth !== '0px')) throw new Error(`selected User Linking selector adds a duplicate divider in ${locale.lang}`);
        await page.screenshot({ path: path.join(output, `user-linking-${locale.lang}-${viewport.name}-selected.png`), fullPage: true });
        await record(page, '/bot/admin/users', locale.lang, viewport.name, 'ready', 'four-column User Linking table rendered');

        await page.goto(`${base}/bot/admin/health?lang=${locale.lang}`, { waitUntil: 'networkidle' });
        if (!(await page.locator('main').innerText()).trim()) throw new Error(`System Status did not render in ${locale.lang}`);
        const graphContract = await page.evaluate(() => ({
          lines: document.querySelectorAll('.telemetry-line').length,
          bars: document.querySelectorAll('.telemetry-bar').length,
          failureMarks: document.querySelectorAll('.telemetry-failure').length,
          oldDetails: document.querySelectorAll('.pipeline-health-details,[data-open-pipeline-details]').length,
          redundantResponseLabels: [...document.querySelectorAll('.api-observation-label')].filter(node => /Current response time|現在の応答時間/.test(node.textContent || '')).length,
          plots: [...document.querySelectorAll('.api-sparkline')].map(svg => ({
            baselineLeft: Number(svg.querySelector('.chart-baseline')?.getAttribute('x1')),
            baselineRight: Number(svg.querySelector('.chart-baseline')?.getAttribute('x2')),
            labels: [...svg.querySelectorAll('.chart-time-label')].map(node => [Number(node.getAttribute('x')), node.textContent.trim()]),
            points: [...svg.querySelectorAll('.telemetry-point.success')].map(node => Number(node.getAttribute('cx'))),
            paths: [...svg.querySelectorAll('.telemetry-line')].map(node => node.getAttribute('d')),
          })),
          selector: document.querySelector('[data-system-status-window]') !== null,
          generatedAt: document.querySelector('[data-system-status-refresh]')?.textContent.includes('payload.generated_at') || false,
        }));
        const expectedLabels = locale.lang === 'ja' ? ['60秒', '30秒', '今'] : ['60s', '30s', 'Now'];
        const expectedX = [44, 248, 452];
        if (graphContract.lines !== 2 || graphContract.bars || graphContract.failureMarks || graphContract.oldDetails || graphContract.redundantResponseLabels || graphContract.selector || !graphContract.generatedAt || graphContract.plots.length !== 2 || graphContract.plots.some(plot => plot.baselineLeft !== 44 || plot.baselineRight !== 452 || plot.labels.some((label, index) => label[0] !== expectedX[index] || label[1] !== expectedLabels[index]) || plot.points.some(x => x < 48 || x > 448) || plot.paths.length !== 1 || !plot.paths[0].startsWith('M48.0,')) || JSON.stringify(graphContract.plots[0].points) !== JSON.stringify(graphContract.plots[1].points) || JSON.stringify(graphContract.plots[0].labels) !== JSON.stringify(graphContract.plots[1].labels)) {
          throw new Error(`System Status retained obsolete graph/detail UI in ${locale.lang}: ${JSON.stringify(graphContract)}`);
        }
        await page.screenshot({ path: path.join(output, `system-status-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/health', locale.lang, viewport.name, 'ready', `Current IA System Status loaded; ${graphContract.lines} line paths`);

        await page.waitForFunction(() => [...document.querySelectorAll('.api-sparkline .telemetry-line')].length === 2, null, { timeout: 5000 });
        await page.screenshot({ path: path.join(output, `system-status-${locale.lang}-${viewport.name}-60s.png`), fullPage: true });
        await record(page, '/bot/admin/health', locale.lang, viewport.name, '60s', 'both charts share the fixed rolling window, cycle timestamps, and X coordinates');
      }
    }

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(`${base}/bot/admin?lang=en`, { waitUntil: 'networkidle' });
    await page.mouse.move(-50, -50);
    await page.waitForTimeout(100);
    const dashboardGrid = await page.locator('canvas[data-background="app-dots"]').evaluate(canvas => canvas.toDataURL());
    await page.waitForTimeout(500);
    if (await page.locator('canvas[data-background="app-dots"]').evaluate(canvas => canvas.toDataURL()) !== dashboardGrid) {
      throw new Error('authenticated dot grid changed without pointer interaction');
    }
    for (const route of ['/bot/admin/projects?lang=en', '/bot/admin/users?lang=en', '/bot/admin/health?lang=en']) {
      await page.goto(`${base}${route}`, { waitUntil: 'networkidle' });
      const routeGrid = await page.locator('canvas[data-background="app-dots"]').evaluate(canvas => canvas.toDataURL());
      if (routeGrid !== dashboardGrid) throw new Error(`authenticated dot positions/background changed with content route ${route}`);
    }
    await page.goto(`${base}/bot/admin?lang=en`, { waitUntil: 'networkidle' });
    const appPointer = { x: 420, y: 380 };
    await page.mouse.move(0, 0);
    await page.waitForTimeout(250);
    const appBefore = await localCanvasAlpha(page, appPointer.x, appPointer.y, 100);
    const farPoint = { x: 1200, y: 820 };
    const farBefore = await localCanvasAlpha(page, farPoint.x, farPoint.y, 24);
    await page.mouse.move(appPointer.x, appPointer.y);
    await page.waitForTimeout(400);
    const appAfter = await localCanvasAlpha(page, appPointer.x, appPointer.y, 100);
    const farAfter = await localCanvasAlpha(page, farPoint.x, farPoint.y, 24);
    if (appAfter <= appBefore) throw new Error(`app pointer did not subtly activate local dots (${appBefore} -> ${appAfter})`);
    if (farAfter !== farBefore) throw new Error(`app pointer activation changed distant dots (${farBefore} -> ${farAfter})`);
    records.push({ route: '/bot/admin', locale: 'en', viewport: 'desktop-1440', state: 'pointer activation', detail: `local dot alpha increased from ${appBefore} to ${appAfter}; distant dot alpha stayed ${farAfter}; dot positions remain fixed` });
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.waitForTimeout(100);
    const reducedApp = await page.locator('canvas[data-background="app-dots"]').evaluate(canvas => canvas.toDataURL());
    await page.waitForTimeout(500);
    if (await page.locator('canvas[data-background="app-dots"]').evaluate(canvas => canvas.toDataURL()) !== reducedApp) {
      throw new Error('app ambient background continued animating under reduced-motion preference');
    }
    records.push({ route: '/bot/admin', locale: 'en', viewport: 'desktop-1440', state: 'reduced motion', detail: 'canvas frame remained stable with reduced motion enabled' });
    await page.emulateMedia({ reducedMotion: 'no-preference' });

    await fixture(page, 'sparse-production');
    for (const locale of locales) {
      for (const viewport of viewports) {
        await page.setViewportSize({ width: viewport.width, height: viewport.height });
        await gotoProduction(page, locale, 'overview');
        const overviewText = await page.locator('#panel-overview').innerText();
        const sectionCount = await page.locator('.production-overview > .production-settings-section').count();
        const emptyIssues = locale.lang === 'ja' ? '現在の問題はありません' : 'No current issues';
        const emptyActivity = locale.lang === 'ja' ? '最近のアクティビティはありません' : 'No recent activity';
        if (sectionCount !== 3 || !overviewText.includes(emptyIssues) || !overviewText.includes(emptyActivity)) throw new Error(`Sparse Overview collapsed its three-section skeleton in ${locale.lang}: ${overviewText}`);
        await page.screenshot({ path: path.join(output, `production-sparse-overview-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/projects?tab=overview', locale.lang, viewport.name, 'sparse Production', 'Status / Current Issues / Recent Activity remain visible with concise empty states');

        await gotoProduction(page, locale, 'notifications');
        const routeRows = page.locator('.production-notification-table tbody tr[data-task-type-id]');
        if (await routeRows.count() !== 1 || await page.locator('.production-notification-table thead th').count() !== 3 || await routeRows.first().locator('.production-wfa-kind').count() !== 2) throw new Error(`Sparse Notifications lost the one-route table/WFA summary structure in ${locale.lang}`);
        await page.screenshot({ path: path.join(output, `production-sparse-notifications-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/projects?tab=notifications', locale.lang, viewport.name, 'sparse Production', 'one route retains the complete three-column table and separate Automatic/Additional rows');

        await gotoProduction(page, locale, 'team');
        if (await page.locator('.production-team-row').count() !== 1 || await page.locator('.production-team-table thead th').count() !== 5 || await page.locator('.production-team-table col').count() !== 5) throw new Error(`Sparse Team lost its single row/five-column table structure in ${locale.lang}`);
        await page.screenshot({ path: path.join(output, `production-sparse-team-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/projects?tab=team', locale.lang, viewport.name, 'sparse Production', 'one member retains a stable five-column Team table');

        await gotoProduction(page, locale, 'settings');
        if (await page.locator('.production-settings-list > .production-settings-section').count() !== 4 || await page.locator('.production-settings-list details > summary').evaluateAll(nodes => nodes.some(node => node.getBoundingClientRect().height < 44))) throw new Error(`Sparse Settings lost its vertical section/disclosure rhythm in ${locale.lang}`);
        await page.screenshot({ path: path.join(output, `production-sparse-settings-${locale.lang}-${viewport.name}.png`), fullPage: true });
        await record(page, '/bot/admin/projects?tab=settings', locale.lang, viewport.name, 'sparse Production', 'four vertical Settings sections and readable disclosure rows remain visible');
      }
    }

    if (errors.length) throw new Error(`browser console/runtime or external-origin errors (${errors.length}): ${errors.slice(0, 12).join(' | ')}`);
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
