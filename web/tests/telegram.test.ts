import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formatAdminIds, parseAdminIds, validProxy, validToken } from '../src/lib/telegram.ts';

test('admin ids: commas, Persian comma, spaces and Persian digits', () => {
  assert.deepEqual(parseAdminIds('123, 456'), { ids: [123, 456], bad: [] });
  assert.deepEqual(parseAdminIds('۱۲۳،۴۵۶ 789\n10'), { ids: [123, 456, 789, 10], bad: [] });
  assert.deepEqual(parseAdminIds('٣٤٥'), { ids: [345], bad: [] });
  assert.deepEqual(parseAdminIds('  '), { ids: [], bad: [] });
});

test('admin ids: duplicates dropped, junk reported', () => {
  assert.deepEqual(parseAdminIds('5, 5, ۵'), { ids: [5], bad: [] });
  assert.deepEqual(parseAdminIds('12, abc, -3, 0, @user'), { ids: [12], bad: ['abc', '-3', '0', '@user'] });
  assert.equal(formatAdminIds([1, 22]), '1, 22');
});

test('proxy and token checks match the server', () => {
  assert.ok(validProxy(''));
  assert.ok(validProxy('socks5://127.0.0.1:1080'));
  assert.ok(validProxy('http://user:pass@proxy.local:8080'));
  assert.ok(!validProxy('127.0.0.1:1080'));
  assert.ok(!validProxy('ftp://x'));
  assert.ok(validToken('123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw0'));
  assert.ok(!validToken('123456:short'));
  assert.ok(!validToken('not a token'));
});
