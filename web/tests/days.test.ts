import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseDays, persianDigits } from '../src/lib/days.ts';

test('a free number of days takes 1 and any whole number', () => {
  assert.deepEqual(parseDays('1'), { draft: '1', days: 1 });
  assert.deepEqual(parseDays('45'), { draft: '45', days: 45 });
  assert.deepEqual(parseDays('3650'), { draft: '3650', days: 3650 });
});

test('Persian and Arabic-Indic digits are read', () => {
  assert.deepEqual(parseDays('۱'), { draft: '1', days: 1 });
  assert.deepEqual(parseDays('۱۲'), { draft: '12', days: 12 });
  assert.deepEqual(parseDays('٣'), { draft: '3', days: 3 });
});

test('empty, zero and junk mean no value; huge values are capped', () => {
  assert.deepEqual(parseDays(''), { draft: '', days: 0 });
  assert.deepEqual(parseDays('0'), { draft: '', days: 0 });
  assert.deepEqual(parseDays('07'), { draft: '7', days: 7 });
  assert.deepEqual(parseDays('-2.5 روز'), { draft: '25', days: 25 });
  assert.equal(parseDays('99999').days, 36500);
});

test('the Persian panel shows what was typed in Persian digits and reads it back', () => {
  assert.equal(persianDigits('1'), '۱');
  assert.equal(persianDigits('365'), '۳۶۵');
  assert.deepEqual(parseDays(persianDigits('120')), { draft: '120', days: 120 });
});
