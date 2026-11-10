import { test } from 'node:test';
import assert from 'node:assert/strict';
import { noteLine, noteLines } from '../src/components/updateNotes.ts';

test('banner line skips headings and bullets', () => {
  assert.equal(noteLine('## What is new\n\n- **Xray config** section\n- Hosts'), 'Xray config section');
  assert.equal(noteLine('### Changes\n'), '');
  assert.equal(noteLine('Plain first line'), 'Plain first line');
  assert.ok(noteLine('- ' + 'x'.repeat(300)).length <= 120);
});

test('dialog lines turn bullets into dots and drop headings marks', () => {
  assert.deepEqual(noteLines('## New\n- a\n* b\n\n\nend'), ['New', '• a', '• b', '', 'end']);
});
