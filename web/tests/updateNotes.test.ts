import { test } from 'node:test';
import assert from 'node:assert/strict';
import { inline, noteBlocks, noteLine, plain, sectionFor } from '../src/components/updateNotes.ts';

// The v0.3.1 release body, as GitHub serves it.
const V031 = `## فارسی

- برچسب زبانه های پیکربندی Xray در موبایل دیگر دو خطی نمی شوند
- منوی \`veilon\`: زدن Enter خالی دیگر منو را نمی بندد
- حذف کامل (\`--purge\`) فایل تنظیم BBR را هم پاک می کند

## English

- Xray config tab labels no longer wrap on phones
- \`veilon\` menu: pressing Enter alone no longer crashes it
`;

test('only the section of the panel language is shown', () => {
  const fa = noteBlocks(V031, 'fa');
  const en = noteBlocks(V031, 'en');
  assert.equal(fa.length, 3);
  assert.equal(en.length, 2);
  assert.ok(fa.every((b) => b.kind === 'item'));
  assert.ok(!fa.some((b) => /English|Xray config tab/.test(plain(b.spans))));
  assert.ok(!en.some((b) => /فارسی|منوی/.test(plain(b.spans))));
});

test('no raw markdown is left: headings and backticks are gone', () => {
  for (const lang of ['fa', 'en'] as const) {
    for (const b of noteBlocks(V031, lang)) {
      const t = plain(b.spans);
      assert.ok(!t.includes('`') && !t.includes('##') && !/^[-*] /.test(t), t);
      assert.ok(!t.includes('فارسی') && t !== 'English');
    }
  }
  const menu = noteBlocks(V031, 'en')[1].spans;
  assert.deepEqual(menu[0], { code: true, text: 'veilon' });
  assert.equal(menu[1].text, ' menu: pressing Enter alone no longer crashes it');
});

test('inline markdown', () => {
  assert.deepEqual(inline('**Hosts** for [nodes](https://x) and `a b`'), [
    { code: false, text: 'Hosts for nodes and ' },
    { code: true, text: 'a b' },
  ]);
});

test('notes without language sections are shown whole', () => {
  assert.equal(sectionFor('## What is new\n- a', 'fa').length, 2);
  const b = noteBlocks('## What is new\n\n- **Xray config** section\n- Hosts\n\nPlain para\nline two', 'fa');
  assert.deepEqual(b.map((x) => [x.kind, plain(x.spans)]), [
    ['heading', 'What is new'],
    ['item', 'Xray config section'],
    ['item', 'Hosts'],
    ['para', 'Plain para line two'],
  ]);
});

test('only the other language present: shown rather than nothing', () => {
  assert.equal(noteBlocks('## English\n- only', 'fa').length, 1);
});

test('banner line is the first item of the panel language', () => {
  assert.equal(noteLine(V031, 'en'), 'Xray config tab labels no longer wrap on phones');
  assert.equal(noteLine(V031, 'fa'), 'برچسب زبانه های پیکربندی Xray در موبایل دیگر دو خطی نمی شوند');
  assert.equal(noteLine('### Changes\n'), '');
  assert.ok(noteLine('- ' + 'x'.repeat(300)).length <= 120);
});
