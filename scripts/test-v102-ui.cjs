const assert = require('node:assert/strict');
const { chromium } = require(process.env.WUBU_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.WUBU_TEST_BASE_URL;
if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(base || '')) throw new Error('Temporary loopback service required');

const workspaces = [['/items', 'items'], ['/finance', 'finance'], ['/ai', 'ai']];
function domain(path) {
  if (path.startsWith('/api/v1/intake')) return 'ai';
  if (/^\/api\/v1\/(accounts|money|finance|financial|expenses|statements|runway|planned-cashflows|residences|deposits|receivables)(\/|$)/.test(path)) return 'finance';
  if (/^\/api\/v1\/(summary|items(?:-running-low)?|inventory(?:-events)?|acquisitions|durables|maintenance)(\/|$)/.test(path)) return 'items';
  if (path.startsWith('/api/v1/links') || path.startsWith('/api/v1/ledger/')) return 'cross-domain';
}

(async () => {
  const browser = await chromium.launch({
    channel: 'msedge', headless: true,
    args: ['--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream'],
  });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const errors = [], requests = [];
    let blocked = new Set();
    page.on('pageerror', error => errors.push(error.message));
    await page.route('**/api/v1/**', async route => {
      const { pathname } = new URL(route.request().url());
      requests.push(pathname);
      if (blocked.has(domain(pathname))) {
        await route.fulfill({ status: 503, json: { error: { message: '测试注入的模块故障' } } });
      } else {
        await route.continue();
      }
    });
    const visible = async selector => page.locator(selector).first().waitFor();
    const navigation = async () => {
      for (const [path] of workspaces) await visible('a[href="' + path + '"]');
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Page overflows the viewport');
      assert(await page.locator('.ledger-table-wrap:has(table.ledger-table)').evaluateAll(tables =>
        tables.every(table => table.scrollWidth <= table.clientWidth + 1)), 'A workbench table needs horizontal scrolling');
    };
    const finishLoading = () => page.waitForLoadState('networkidle');
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 });
      blocked.clear();
      await page.goto(base);
      await page.evaluate(() => document.fonts.ready);
      const icons = await page.locator('.workspace-card-icon').evaluateAll(elements => elements.map(icon => {
        const range = document.createRange();
        range.selectNodeContents(icon);
        const glyph = range.getBoundingClientRect();
        const box = icon.getBoundingClientRect();
        const fontSize = parseFloat(getComputedStyle(icon).fontSize);
        return { name: icon.textContent, fits: glyph.width <= fontSize + 1 && glyph.left >= box.left && glyph.right <= box.right };
      }));
      assert.equal(icons.length, 3);
      assert.deepEqual(icons.filter(icon => !icon.fits), [], 'Home icon names must render as single glyphs');
      for (const [path, owner] of workspaces) {
        blocked = new Set(['items', 'finance', 'ai', 'cross-domain'].filter(key => key !== owner));
        requests.length = 0;
        await page.goto(base + path);
        await navigation();
        await finishLoading();
        assert(requests.some(path => domain(path) === owner), owner + ' did not load its own data');
        assert.deepEqual(requests.filter(path => blocked.has(domain(path))), [], owner + ' requested another domain');
        if(width===390&&owner!=='ai'){
          const trigger=owner==='items'?'#new-purchase':'[data-cash-action=expense]';
          const dialog=page.locator(owner==='items'?'#purchase-dialog':'#cash-dialog');
          await page.locator(trigger).first().click();
          await dialog.waitFor();
          assert(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth+1),owner+' form overflows on mobile');
          await page.keyboard.press('Escape');
        }
      }
    }

    for (const [, owner] of workspaces) {
      blocked = new Set([owner]);
      await page.goto(base);
      await navigation();
      await finishLoading();
      await visible('[data-module="' + owner + '"][data-state=unavailable]');
      for (const [path, available] of workspaces) {
        if (available === owner) continue;
        await visible('[data-module="' + available + '"][data-state=ready]');
        await page.locator('a[href="' + path + '"]').first().click();
        await page.waitForURL(base + path);
        await navigation();
        await page.goto(base);
      }
    }
    blocked = new Set(['finance', 'ai', 'cross-domain']);
    requests.length = 0;
    await page.goto(base + '/inventory');
    await finishLoading();
    assert(requests.some(path => domain(path) === 'items'));
    assert.deepEqual(requests.filter(path => blocked.has(domain(path))), [], 'Standalone inventory booted another domain');
    blocked.clear();
    for (const path of ['/weekly-sync', '/financial-inbox', '/inbox?batch=missing']) {
      await page.goto(base + path);
      await navigation();
    }
    await page.goto(base + '/ai');
    await page.getByText('未配置 AI 分析，可继续手动处理。').waitFor();
    await page.context().grantPermissions(['microphone'], { origin: base });
    await page.locator('#inbox-voice').click();
    await page.getByRole('button', { name: '结束录音' }).waitFor();
    await page.locator('#inbox-voice').click();
    await visible('#inbox-capture-files .inbox-file-chip');
    await page.locator('#inbox-text').fill('按任务书验收独立 AI 工作区');
    const submitted = page.waitForResponse(response =>
      response.url().endsWith('/api/v1/intake/batches') && response.request().method() === 'POST');
    await page.locator('#inbox-capture-submit').click();
    const response = await submitted;
    assert(response.ok(), await response.text());
    const batch = await response.json();
    assert.equal(batch.analysis_scheduled, false);
    const inventory = await page.request.get(base + '/api/v1/items');
    assert.equal((await inventory.json()).total, 0, 'Unconfirmed AI input changed Items');
    await page.locator('#analysis-history summary').click();
    await page.getByText('暂无已处理记录。').waitFor();
    assert.deepEqual(errors, []);
    console.log('v1.0.2 UI PASS: desktop/mobile navigation, domain request isolation, failures, legacy routes, AI unavailable and microphone intake.');
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
