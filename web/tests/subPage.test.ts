import { test } from 'node:test';
import assert from 'node:assert/strict';
import { allLinks, expiryOf, formatBytes, formatDate, knownStatus, linkChips, onlineState, usageRatio, usageTone } from '../src/sub/format.ts';
import { dictionaries } from '../src/sub/i18n.ts';

const now = Date.parse('2026-11-11T12:00:00Z');
const base = { status: 'active', used: 10, limit: 100, expire_at: null, on_hold_days: null };

test('usage ratio: unlimited is null, clamped otherwise', () => {
  assert.equal(usageRatio({ used: 5, limit: 0 }), null);
  assert.equal(usageRatio({ used: 25, limit: 100 }), 0.25);
  assert.equal(usageRatio({ used: 250, limit: 100 }), 1);
});

test('expiry: none, on hold, future (days rounded up), past', () => {
  assert.deepEqual(expiryOf({ expire_at: null }, now), { kind: 'none' });
  assert.deepEqual(expiryOf({ expire_at: null, on_hold_days: 30 }, now), { kind: 'hold', days: 30 });
  assert.deepEqual(expiryOf({ expire_at: '2026-11-11T15:00:00Z' }, now), { kind: 'future', days: 1 });
  assert.deepEqual(expiryOf({ expire_at: '2026-11-21T12:00:00Z' }, now), { kind: 'future', days: 10 });
  assert.equal(expiryOf({ expire_at: '2026-11-01T12:00:00Z' }, now).kind, 'past');
});

test('tone follows status, then usage and days left', () => {
  assert.equal(usageTone({ ...base, status: 'disabled' }, now), 'muted');
  assert.equal(usageTone({ ...base, status: 'expired' }, now), 'danger');
  assert.equal(usageTone({ ...base, status: 'limited' }, now), 'danger');
  assert.equal(usageTone({ ...base, status: 'on_hold' }, now), 'info');
  assert.equal(usageTone(base, now), 'accent');
  assert.equal(usageTone({ ...base, used: 80 }, now), 'warn');
  assert.equal(usageTone({ ...base, used: 95 }, now), 'danger');
  assert.equal(usageTone({ ...base, expire_at: '2026-11-15T12:00:00Z' }, now), 'warn');
  assert.equal(knownStatus('weird'), 'disabled');
});

test('bytes and Persian dates follow the admin panel formatting', () => {
  assert.equal(formatBytes('en', 12 * 1024 ** 3), '12 GB');
  assert.equal(formatBytes('fa', 1.5 * 1024 ** 2), '۱٫۵ MB');
  const fa = formatDate('fa', '2026-12-24T20:30:00Z', 'Asia/Tehran');
  assert.match(fa, /۱۴۰۵/); // Persian calendar year
  assert.match(fa, /دی/); // 4 Dey 1405 in Tehran (00:00 on the 25th)
  assert.ok(!fa.includes('\u200c'));
  assert.equal(formatDate('en', '2026-12-24T20:30:00Z', 'UTC'), '24 Dec 2026');
  assert.equal(formatDate('en', '2026-12-24T20:30:00Z', 'Asia/Tehran'), '25 Dec 2026');
});

test('link chips and copy all', () => {
  assert.deepEqual(linkChips({ name: 'a', protocol: 'vless', network: 'tcp', security: 'reality' }), ['VLESS', 'Reality']);
  assert.deepEqual(linkChips({ name: 'b', protocol: 'vmess', network: 'ws', security: 'tls' }), ['VMess', 'ws', 'TLS']);
  assert.deepEqual(linkChips({ name: 'c', protocol: 'wireguard' }), ['WireGuard']);
  assert.equal(allLinks([{ name: 'a', protocol: 'vless', uri: 'vless://a' }, { name: 'w', protocol: 'wireguard' }, { name: 'b', protocol: 'ss', uri: 'ss://b' }]), 'vless://a\nss://b');
});

test('dictionaries: same keys, Persian has no zero-width non-joiner', () => {
  assert.deepEqual(Object.keys(dictionaries.fa).sort(), Object.keys(dictionaries.en).sort());
  for (const [k, v] of Object.entries(dictionaries.fa)) assert.ok(!v.includes('\u200c'), k);
});

test('last online tile: the server online flag wins, then the last time seen', () => {
  assert.equal(onlineState({ online: true, last_online_at: 'Wed 2026-11-11 3:29 PM (UTC+03:30)' }), 'online');
  assert.equal(onlineState({ online: false, last_online_at: 'Wed 2026-11-11 1:00 PM (UTC+03:30)' }), 'seen');
  assert.equal(onlineState({ last_online_at: 'Wed 2026-11-11 1:00 PM (UTC+03:30)' }), 'seen'); // older servers: no flag
  assert.equal(onlineState({ online: false, last_online_at: null }), 'never');
  assert.equal(dictionaries.fa['usage.online.now'], 'آنلاین');
  assert.equal(dictionaries.en['usage.online.now'], 'Online');
});

test('live refresh: ?format=live every 5 s at most, merged without touching the configs', async () => {
  const { LIVE_POLL_MS, liveURL, mergeLive } = await import('../src/sub/format.ts');
  assert.ok(LIVE_POLL_MS <= 5000);
  assert.equal(liveURL('/sub/tok'), '/sub/tok?format=live');
  const d = { username: 'u', status: 'active', up: 1, down: 2, used: 3, limit: 0, expire_at: null, last_online_at: null, online: false, sub_url: 's', title: 't', update_hours: 12, timezone: 'UTC', links: [{ name: 'a', protocol: 'vless', uri: 'vless://x' }] };
  const seen = '2026-11-11T18:00:00Z';
  const on = mergeLive(d, { status: 'active', up: 5, down: 6, used: 11, limit: 0, expire_at: null, last_online_at: seen, online: true });
  assert.equal(on.online, true);
  assert.equal(on.used, 11);
  assert.equal(on.last_online_at, seen);
  assert.equal(on.links, d.links);
  assert.equal(mergeLive(on, { ...on }), on, 'unchanged answer keeps the same object');
  assert.equal(mergeLive(d, null), d);
  assert.equal(mergeLive(d, { error: 'x' }), d);
  const off = mergeLive(on, { status: 'active', online: false, last_online_at: seen });
  assert.equal(off.online, false);
  assert.equal(off.last_online_at, seen);
});
