// Regression test: dock navigation on the REAL Go binary must render the new
// page without a reload — including when the browser never advances
// animations (Android WebViews / in-app browsers that throttle the compositor),
// which used to leave every newly opened page stuck at opacity 0 (black screen).
//
// Needs Playwright (not a dependency of the UI build):
//   npm i --no-save playwright && npx playwright install chromium
// Run a panel locally (make web && make build), e.g.
//   VEILON_CONFIG=/dev/null VEILON_DB_DRIVER=sqlite VEILON_DATA_DIR=$PWD/.dev VEILON_PANEL_PORT=18200 \
//   VEILON_PANEL_PATH=k9Qz3x VEILON_LISTEN=127.0.0.1 VEILON_ADMIN_USERNAME=admin VEILON_ADMIN_PASSWORD='S3cretPass!' bin/veilon serve
// then:
//   node web/scripts/e2e-nav.mjs http://127.0.0.1:18200/k9Qz3x/ [screenshotDir]
// Env: U / P (login, default admin / S3cretPass!), SLOW=1 (3G + 6x CPU), RM=1 (reduced motion), FULL=1 (full Chromium).
import { chromium, devices } from 'playwright';
import fs from 'node:fs';
const BASE = process.argv[2] || 'http://127.0.0.1:18200/k9Qz3x/';
const SHOTS = process.argv[3] || '';
const SLOW = process.env.SLOW === '1';
const RM = process.env.RM === '1';
if (SHOTS) fs.mkdirSync(SHOTS, { recursive: true });
const b = await chromium.launch({ args: ['--no-sandbox'], ...(process.env.FULL === '1' ? { channel: 'chromium' } : {}) });
const ctx = await b.newContext({ ...devices['Pixel 7'], viewport: { width: 390, height: 844 }, colorScheme: 'dark', reducedMotion: RM ? 'reduce' : 'no-preference' });
const p = await ctx.newPage();
const errs = [];
if (SLOW) { const c = await ctx.newCDPSession(p); await c.send('Network.enable'); await c.send('Network.emulateNetworkConditions', { offline: false, latency: 400, downloadThroughput: 400*1024/8, uploadThroughput: 200*1024/8 }); await c.send('Emulation.setCPUThrottlingRate', { rate: 6 }); }
p.on('pageerror', (e) => errs.push('pageerror: ' + e.message));
p.on('console', (m) => m.type() === 'error' && errs.push('console: ' + m.text()));
p.on('response', (r) => r.status() >= 400 && errs.push(r.status() + ' ' + r.url()));
await p.goto(BASE + 'login', { waitUntil: 'networkidle' });
await p.fill('input[autocomplete="username"], input:not([type=password])', process.env.U || 'admin');
await p.fill('input[type=password]', process.env.P || 'S3cretPass!');
await p.click('button[type=submit]');
await p.waitForURL((u) => !u.pathname.endsWith('/login'), { timeout: 10000 });
await p.waitForTimeout(1500);
let fail = 0;
const H1 = {};
async function check(label, expectPath) {
  const want = H1[expectPath.replace(/\/$/, '')];
  // wait up to 3s for visible page content in <main>
  const t0 = Date.now();
  let info;
  while (Date.now() - t0 < (SLOW ? 12000 : 4000)) {
    info = await p.evaluate(() => {
      const m = document.querySelector('main');
      const wrap = m?.firstElementChild;
      const h = m?.querySelector('h1');
      // effective opacity of the page heading (product over its ancestors)
      let op = 1;
      for (let e = h; e; e = e.parentElement) op *= parseFloat(getComputedStyle(e).opacity);
      op = h ? String(op) : 'none';
      return { path: location.pathname, opacity: op, h1: h?.textContent?.trim() ?? null, text: (m?.innerText || '').trim().length };
    });
    if ((info.path.replace(/\/$/, '') + '/').endsWith(expectPath.replace(/\/$/, '') + '/') && parseFloat(info.opacity) > 0.95 && info.text > 0 && info.h1 && (!want || info.h1 === want)) break;
    await p.waitForTimeout(100);
  }
  const ok = (info.path.replace(/\/$/, '') + '/').endsWith(expectPath.replace(/\/$/, '') + '/') && parseFloat(info.opacity) > 0.95 && info.text > 0 && !!info.h1 && (!want || info.h1 === want);
  if (!ok) fail++;
  console.log(ok ? 'PASS' : 'FAIL', label, JSON.stringify(info));
  if (SHOTS) await p.screenshot({ path: `${SHOTS}/nav-${label}.png` });
}
await check('initial', '');
const items = await p.locator('nav a').all();
const hrefs = [];
for (const a of items) hrefs.push(new URL(await a.evaluate((x) => x.href)).pathname);
console.log('dock links', hrefs);
// learn each page's heading from a direct load, so navigation must show the *new* page, not a stale one
for (const h of [...hrefs, ...['certs', 'settings', 'admins', 'audit', 'groups'].map((x) => hrefs[0].replace(/\/$/, '') + '/' + x)]) {
  await p.goto(new URL(h, BASE).href, { waitUntil: 'networkidle' });
  await p.waitForTimeout(500);
  H1[h.replace(/\/$/, '')] = await p.locator('main h1').first().textContent();
}
// map short names used below
for (const k of Object.keys(H1)) H1['/' + k.split('/').filter(Boolean).slice(1).join('/')] = H1[k];
console.log('headings', H1);
await p.goto(BASE, { waitUntil: 'networkidle' });
await p.waitForTimeout(800);
for (let round = 0; round < 2; round++) {
  for (let i = items.length - 1; i >= 0; i--) {
    if (round === 1) { await p.mouse.wheel(0, 4000); await p.evaluate(() => window.scrollTo(0, document.body.scrollHeight)); await p.waitForTimeout(300); }
    await items[i].tap();
    await check(`r${round}-${hrefs[i].split('/').filter(Boolean).slice(1).join('-') || 'home'}`, hrefs[i]);
  }
}
// "more" menu items
for (const name of ['certs', 'settings', 'admins', 'audit', 'groups']) {
  await p.locator('nav button[aria-haspopup=menu]').click();
  await p.waitForTimeout(400);
  await p.locator('[role=menu] [role=menuitem]').nth(['certs', 'settings', 'admins', 'audit', 'groups'].indexOf(name)).click();
  await check('more-' + name, '/' + name);
}
// deep-link refresh
for (const path of ['inbounds', 'users', 'settings', 'logs']) {
  await p.goto(BASE + path, { waitUntil: 'networkidle' });
  await check('deeplink-' + path, '/' + path);
}
await p.reload({ waitUntil: 'networkidle' });
await check('reload-logs', '/logs');
// --- Stalled-animation mode: emulate a WebView whose animations never advance
// (backgrounded/throttled compositor). Page content must still be visible.
await p.goto(BASE, { waitUntil: 'networkidle' });
await p.waitForTimeout(800);
await p.evaluate(() => { setInterval(() => document.getAnimations().forEach((a) => a.pause()), 1); });
const items2 = await p.locator('nav a').all();
for (let i = items2.length - 1; i >= 0; i--) {
  await items2[i].tap();
  await check(`stall-${hrefs[i].split('/').filter(Boolean).slice(1).join('-') || 'home'}`, hrefs[i]);
}
console.log('errors:', errs.filter((e) => !/favicon/.test(e)));
console.log(fail ? `RESULT: ${fail} FAILURES` : 'RESULT: ALL PASS');
await b.close();
process.exit(fail ? 1 : 0);
