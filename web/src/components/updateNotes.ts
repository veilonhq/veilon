// Release notes (GitHub markdown) as plain text for the update banner and dialog.

/** The first meaningful line of release notes (markdown headings and bullets stripped). */
export function noteLine(notes: string): string {
  for (const raw of notes.split('\n')) {
    if (/^\s*#/.test(raw)) continue;
    const l = raw.replace(/^[-*]\s+/, '').replace(/\*\*/g, '').trim();
    if (l && !/^(what'?s changed|changes|changelog)$/i.test(l)) return l.length > 120 ? `${l.slice(0, 117)}…` : l;
  }
  return '';
}
/** Release notes as plain lines for the dialog. */
export function noteLines(notes: string): string[] {
  return notes
    .split('\n')
    .map((l) => l.replace(/^#+\s*/, '').replace(/\*\*/g, '').replace(/^\s*[-*]\s+/, '• ').trimEnd())
    .filter((l, i, a) => l.trim() || (i > 0 && a[i - 1].trim()));
}
