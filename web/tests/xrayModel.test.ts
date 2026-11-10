import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  balancerToForm,
  dnsToForm,
  formToBalancer,
  formToDns,
  formToOut,
  formToRule,
  outErrors,
  outToForm,
  renameOutbound,
  ruleToForm,
  type XrayTemplate,
} from '../src/pages/xray/model.ts';

test('vless reality outbound round-trips and keeps unknown keys', () => {
  const x = {
    tag: 'nl',
    protocol: 'vless',
    mux: { enabled: true },
    settings: { vnext: [{ address: '1.2.3.4', port: 443, users: [{ id: 'uuid-1', encryption: 'none', flow: 'xtls-rprx-vision' }] }] },
    streamSettings: { network: 'raw', security: 'reality', sockopt: { mark: 255 }, realitySettings: { publicKey: 'PBK', shortId: 'ab', serverName: 'a.com', fingerprint: 'chrome' } },
  };
  const f = outToForm(x);
  assert.equal(f.protocol, 'vless');
  assert.equal(f.address, '1.2.3.4');
  assert.equal(f.flow, 'xtls-rprx-vision');
  assert.equal(f.security, 'reality');
  assert.equal(f.publicKey, 'PBK');
  const back = formToOut(f, x);
  assert.deepEqual(back.mux, { enabled: true });
  assert.deepEqual((back.streamSettings as Record<string, unknown>).sockopt, { mark: 255 });
  assert.deepEqual(back.settings, x.settings);
  assert.deepEqual(outErrors(f, ['direct']), {});
});

test('outbound validation', () => {
  const f = outToForm({ tag: 'a b', protocol: 'trojan', settings: { servers: [{ address: '', port: 0 }] } });
  const e = outErrors(f, []);
  assert.ok(e.tag && e.address && e.port && e.password);
  const w = outToForm({ tag: 'wg', protocol: 'wireguard', settings: { secretKey: 'k', address: ['10.0.0.2/32'], peers: [{ publicKey: 'p', endpoint: 'x.com:51820' }], reserved: [1, 2, 3] } });
  assert.equal(w.reserved, '1,2,3');
  assert.deepEqual(outErrors(w, []), {});
  const wb = formToOut(w);
  assert.deepEqual((wb.settings as Record<string, unknown>).reserved, [1, 2, 3]);
  assert.equal(outErrors({ ...w, tag: 'direct' }, ['direct']).tag, 'xc.err.tagUsed');
});

test('rule form splits geosite/geoip and writes them back', () => {
  const r = { _name: 'ai', domain: ['geosite:openai', 'full:x.com'], ip: ['geoip:ir', '10.0.0.0/8'], port: '443', inboundTag: ['inbound-1'], user: ['alice'], outboundTag: 'warp', attrs: { ':method': 'GET' } };
  const f = ruleToForm(r);
  assert.equal(f.geosite, 'openai');
  assert.equal(f.domains, 'full:x.com');
  assert.equal(f.geoip, 'ir');
  assert.equal(f.ips, '10.0.0.0/8');
  const back = formToRule(f, r);
  assert.deepEqual(back, { attrs: { ':method': 'GET' }, _name: 'ai', domain: ['full:x.com', 'geosite:openai'], ip: ['10.0.0.0/8', 'geoip:ir'], port: '443', inboundTag: ['inbound-1'], user: ['alice'], outboundTag: 'warp' });
  const b = formToRule({ ...f, target: 'balancer', balancer: 'lb' });
  assert.equal(b.balancerTag, 'lb');
  assert.equal(b.outboundTag, undefined);
});

test('balancer and dns forms', () => {
  const b = balancerToForm({ tag: 'lb', selector: ['nl-'], strategy: { type: 'leastPing' }, fallbackTag: 'direct' });
  assert.deepEqual(formToBalancer(b), { tag: 'lb', selector: ['nl-'], strategy: { type: 'leastPing' }, fallbackTag: 'direct' });
  assert.equal(formToBalancer({ ...b, strategy: 'random', fallback: '' }).strategy, undefined);
  const d = dnsToForm({ servers: ['1.1.1.1', { address: '8.8.8.8', domains: ['geosite:google'], skipFallback: true }], queryStrategy: 'UseIPv4', hosts: { 'a.com': '1.2.3.4' } });
  assert.equal(d.enabled, true);
  assert.equal(d.servers[1].domains, 'geosite:google');
  const back = formToDns(d, { servers: [{ address: '8.8.8.8', skipFallback: true }] });
  assert.deepEqual(back, { servers: ['1.1.1.1', { address: '8.8.8.8', skipFallback: true, domains: ['geosite:google'] }], queryStrategy: 'UseIPv4', hosts: { 'a.com': '1.2.3.4' } });
  assert.equal(formToDns({ ...d, enabled: false }), undefined);
});

test('renaming an outbound updates rules and fallbacks', () => {
  const t: XrayTemplate = { log: {}, outbounds: [{ tag: 'a' }], routing: { rules: [{ port: '1', outboundTag: 'a' }], balancers: [{ tag: 'b', selector: ['a'], fallbackTag: 'a' }] } };
  const r = renameOutbound(t, 'a', 'z');
  assert.equal(r.routing.rules[0].outboundTag, 'z');
  assert.equal(r.routing.balancers[0].fallbackTag, 'z');
});
