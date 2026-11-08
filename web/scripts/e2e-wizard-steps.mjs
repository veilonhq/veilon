// Regression: every protocol in the inbound wizard must show step 2/3 content, also when the
// browser never advances animations (Android Telegram WebView left step 2 blank).
// Usage: node web/scripts/e2e-wizard-steps.mjs http://127.0.0.1:18200/k9Qz3x/ [stall]  (needs playwright)
// Every protocol: wizard step 1 -> 2 (and 3) must show visible content, with
// animations running AND with animations frozen (WebView that never advances them).
// node wizard-steps.mjs BASE [stall]
import { chromium } from 'playwright';
const BASE = process.argv[2] || 'http://127.0.0.1:18300/k9Qz3x/';
const STALL = process.argv[3] === 'stall';
const b = await chromium.launch({ args: ['--no-sandbox'] });
const ctx = await b.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true });
await ctx.addInitScript(() => { localStorage.setItem('veilon.lang', 'fa'); });
const p = await ctx.newPage();
p.setDefaultTimeout(6000);
const errs = [];
p.on('pageerror', (e) => errs.push(e.message));
await p.goto(BASE + 'login', { waitUntil: 'networkidle' });
await p.fill('input[autocomplete="username"]', 'admin');
await p.fill('input[type=password]', 'S3cretPass!');
await p.click('button[type=submit]');
await p.waitForURL((u) => !u.pathname.endsWith('/login'));
await p.waitForTimeout(1500);
const PROTOS = ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria', 'tuic', 'masque', 'wireguard', 'amneziawg', 'tun', 'http', 'mixed', 'tunnel', 'mtproto'];
// visible content in the step body: effective opacity of the body (product over ancestors up to the dialog), its height, inner text, on-screen position
const probe = () => p.evaluate(() => {
  const d = document.querySelector('[role=dialog]');
  const ol = d?.querySelector('ol');
  let body = ol?.nextElementSibling;
  if (!body) return { err: 'no body' };
  let op = 1;
  for (let e = body; e && e !== d; e = e.parentElement) op *= parseFloat(getComputedStyle(e).opacity);
  // also the most transparent descendant chain of the first field
  const r = body.getBoundingClientRect();
  const fields = body.querySelectorAll('input, button, [role=switch], .field');
  return { op: +op.toFixed(2), h: Math.round(r.height), left: Math.round(r.left), w: Math.round(r.width), text: body.innerText.trim().length, fields: fields.length };
});
let fail = 0;
for (const proto of PROTOS) {
  await p.goto(BASE + 'inbounds?new=1', { waitUntil: 'networkidle' });
  await p.waitForTimeout(700);
  if (STALL) await p.evaluate(() => { window.__stall = setInterval(() => document.getAnimations().forEach((a) => a.pause()), 1); });
  await p.click(`[data-proto="${proto}"]`);
  await p.waitForTimeout(150);
  const next = p.locator('[role=dialog] .btn--primary').last();
  const res = [];
  for (let s = 2; s <= 3; s++) {
    if (await next.isDisabled()) { res.push(`s${s}:next-disabled`); break; }
    await next.click();
    await p.waitForTimeout(STALL ? 700 : 450);
    const r = await probe();
    const ok = r.op > 0.95 && r.text > 0 && r.fields > 0 && r.left > -5 && r.left + r.w < 400;
    if (!ok) fail++;
    res.push(`s${s}:${ok ? 'ok' : 'BLANK'} ${JSON.stringify(r)}`);
  }
  console.log(proto.padEnd(12), res.join('  '));
}
console.log(STALL ? 'stalled-animations' : 'normal', fail ? `RESULT: ${fail} FAILURES` : 'RESULT: ALL PASS', 'errors', JSON.stringify(errs.slice(0, 3)));
await b.close();
process.exit(fail ? 1 : 0);
