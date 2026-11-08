// Regression: dashboard numbers never count up from zero (first load, polls, remount, reload).
// Usage: node web/scripts/e2e-counters.mjs http://127.0.0.1:18200/k9Qz3x/ [mobile|desktop]  (needs playwright)
// Animated numbers must never count from zero: on first load, on polls, and on
// remount (leave the dashboard and come back). Samples every frame.
// node counters.mjs BASE [mobile|desktop]
import { chromium } from 'playwright';
const BASE = process.argv[2] || 'http://127.0.0.1:18300/k9Qz3x/';
const mobile = (process.argv[3] || 'mobile') === 'mobile';
const b = await chromium.launch({ args: ['--no-sandbox'] });
const ctx = await b.newContext(mobile ? { viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true } : { viewport: { width: 1440, height: 900 } });
await ctx.addInitScript(() => {
  localStorage.setItem('veilon.lang', 'fa');
  const fa = '۰۱۲۳۴۵۶۷۸۹';
  const num = (t) => parseFloat(t.replace(/[۰-۹]/g, (c) => fa.indexOf(c)).replace(/[٬,]/g, '').replace('٫', '.'));
  window.__ct = { on: false, frames: [] };
  const loop = () => {
    if (window.__ct.on) {
      // every CountUp renders as span.num with inline min-width
      // every CountUp on the dashboard: the traffic total (inside .big) and the big user counts (inside .huge)
      const els = [...document.querySelectorAll('#main [class*="_big_"] span.num, #main [class*="_huge_"] span.num')];
      window.__ct.frames.push(els.map((e, i) => [e.closest('[class*="_big_"]') ? 'trafficTotal' : 'count' + i, num(e.textContent || ''), e.closest('[class*="_big_"]')?.querySelector('small')?.textContent || '']));
    }
    requestAnimationFrame(loop);
  };
  requestAnimationFrame(loop);
});
const p = await ctx.newPage();
await p.goto(BASE + 'login', { waitUntil: 'networkidle' });
await p.fill('input[autocomplete="username"]', 'admin');
await p.fill('input[type=password]', 'S3cretPass!');
await p.evaluate(() => (window.__ct.on = true));
await p.click('button[type=submit]');
await p.waitForURL((u) => !u.pathname.endsWith('/login'));
let fail = 0;
async function check(label, ms) {
  await p.evaluate(() => { window.__ct.frames = []; window.__ct.on = true; });
  await p.waitForTimeout(ms);
  const f = await p.evaluate(() => { window.__ct.on = false; return window.__ct.frames; });
  const series = {};
  for (const fr of f) for (const [k, v, unit] of fr) (series[k] ??= []).push([v, unit]);
  const out = [];
  for (const [k, s] of Object.entries(series)) {
    const vals = s.map((x) => x[0]).filter(Number.isFinite);
    if (!vals.length) continue;
    const first = vals[0], last = vals.at(-1), min = Math.min(...vals);
    // count-from-zero: a frame far below both the first and the final value, or a first value near zero while the final isn't
    const fromZero = (last > 0.5 && first < last * 0.2) || (min < Math.min(first, last) * 0.5 && Math.min(first, last) > 0.5);
    // unit/digit disagreement: unit changed mid-series but value tweened
    const units = [...new Set(s.map((x) => x[1]))];
    if (fromZero) fail++;
    out.push(`${k}:${fromZero ? 'COUNTED-FROM-ZERO' : 'ok'} first=${first} min=${min} last=${last}${units.length > 1 ? ' units=' + units.join('/') : ''} samples=${vals.length}`);
  }
  console.log(label.padEnd(24), out.join(' | ') || 'no numbers');
}
await check('first load', 2500);
await check('polls (6s)', 6000);
for (let i = 0; i < 2; i++) {
  await p.locator('nav a').filter({ hasText: 'کاربران' }).first().click();
  await p.waitForTimeout(800);
  await p.evaluate(() => { window.__ct.frames = []; window.__ct.on = true; });
  await p.locator('nav a').filter({ hasText: 'داشبورد' }).first().click();
  await check(`remount #${i + 1}`, 1500);
}
await p.reload({ waitUntil: 'domcontentloaded' });
await check('reload', 3000);
console.log(fail ? `RESULT: ${fail} FAILURES` : 'RESULT: ALL PASS');
await b.close();
process.exit(fail ? 1 : 0);
