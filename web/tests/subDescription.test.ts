import { test } from 'node:test';
import assert from 'node:assert/strict';
import { linkify } from '../src/sub/format.ts';

const links = (s: string) => linkify(s).filter((p) => p.href).map((p) => [p.text, p.href]);
const joined = (s: string) => linkify(s).map((p) => p.text).join('');

test('linkify: URLs, t.me and @handles', () => {
  assert.deepEqual(links('کانال: https://example.com/a?b=1.'), [['https://example.com/a?b=1', 'https://example.com/a?b=1']]);
  assert.deepEqual(links('t.me/veilon_support، پشتیبانی'), [['t.me/veilon_support', 'https://t.me/veilon_support']]);
  assert.deepEqual(links('پشتیبانی @Support_Bot'), [['@Support_Bot', 'https://t.me/Support_Bot']]);
  assert.deepEqual(links('(http://a.example)'), [['http://a.example', 'http://a.example']]);
});

test('linkify: no links where there are none', () => {
  assert.deepEqual(links('mail me: a.b@example.com'), []);
  assert.deepEqual(links('@ab short'), []);
  assert.deepEqual(links('javascript:alert(1) data:text/html,x'), []);
  assert.deepEqual(links('<a href="javascript:x">x</a>'), []);
});

test('linkify: text is kept exactly, line breaks included', () => {
  for (const s of ['', 'خط اول\nخط دوم https://x.example/\n@channel_one!', '<script>alert(1)</script> & @handle_x']) {
    assert.equal(joined(s), s);
  }
  for (const p of linkify('a https://x.example b t.me/y c @zzzz d')) {
    if (p.href) assert.match(p.href, /^https?:\/\//);
  }
});
