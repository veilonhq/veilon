import { test } from 'node:test';
import assert from 'node:assert/strict';
import { proxyPassProblem, proxyUserProblem } from '../src/lib/user.ts';
import { isProxyLogin } from '../src/lib/protocols.ts';

test('proxy username: empty = automatic, safe chars, not "<digits>."', () => {
  assert.equal(proxyUserProblem(''), '');
  assert.equal(proxyUserProblem('ali_px'), '');
  assert.equal(proxyUserProblem('a.b-c'), '');
  assert.equal(proxyUserProblem('ab'), 'chars');
  assert.equal(proxyUserProblem('has space'), 'chars');
  assert.equal(proxyUserProblem('ali@px'), 'chars');
  assert.equal(proxyUserProblem('7.alice'), 'auto');
});

test('proxy password: 8-64 safe chars', () => {
  assert.equal(proxyPassProblem(''), '');
  assert.equal(proxyPassProblem('Secret-123'), '');
  assert.equal(proxyPassProblem('short'), 'short');
  assert.equal(proxyPassProblem('pass:word1'), 'chars');
  assert.equal(proxyPassProblem('x'.repeat(65)), 'short');
});

test('only http, mixed and socks use the proxy login', () => {
  for (const p of ['http', 'mixed', 'socks']) assert.ok(isProxyLogin(p));
  for (const p of ['vless', 'masque', 'tunnel']) assert.ok(!isProxyLogin(p));
});
