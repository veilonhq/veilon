// Release notes (GitHub markdown) for the update banner and dialog.
//
// Veilon release notes have one "## فارسی" and one "## English" section.
// Only the section for the panel's language is shown (all of the notes
// when they have no language sections), with markdown turned into plain
// blocks: headings, bullet items and paragraphs made of text and inline
// code spans. Nothing is rendered as HTML.

export type Lang = 'fa' | 'en';
export interface Span {
  code: boolean;
  text: string;
}
export interface Block {
  kind: 'heading' | 'item' | 'para';
  spans: Span[];
}

const FA_HEAD = /^(فارسی|persian|farsi)$/i;
const EN_HEAD = /^(english|انگلیسی)$/i;
const SKIP_HEAD = /^(what'?s changed|changes|changelog)$/i;

const headingOf = (l: string) => /^\s*#{1,6}\s+(.*?)\s*#*\s*$/.exec(l)?.[1]?.trim() ?? null;

/** The lines of the section for lang (the whole notes without language sections). */
export function sectionFor(notes: string, lang: Lang): string[] {
  const lines = notes.replace(/\r\n?/g, '\n').split('\n');
  const sections: { lang: Lang | null; lines: string[] }[] = [{ lang: null, lines: [] }];
  for (const l of lines) {
    const h = headingOf(l);
    if (h !== null && (FA_HEAD.test(h) || EN_HEAD.test(h))) {
      sections.push({ lang: FA_HEAD.test(h) ? 'fa' : 'en', lines: [] });
      continue;
    }
    sections[sections.length - 1].lines.push(l);
  }
  const mine = sections.filter((s) => s.lang === lang);
  if (mine.length) return mine.flatMap((s) => s.lines);
  const any = sections.some((s) => s.lang !== null);
  if (!any) return sections[0].lines;
  // only the other language exists: show it rather than nothing
  return sections.filter((s) => s.lang !== null).flatMap((s) => s.lines);
}

/** Inline markdown as text and code spans (bold, italics, links and images reduced to text). */
export function inline(s: string): Span[] {
  const out: Span[] = [];
  const parts = s.split(/(`+[^`]*?`+)/);
  for (const p of parts) {
    if (!p) continue;
    const m = /^(`+)([^`]*?)\1$/.exec(p);
    if (m) {
      out.push({ code: true, text: m[2].trim() });
      continue;
    }
    const text = p
      .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
      .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
      .replace(/\*\*([^*]+)\*\*|__([^_]+)__/g, '$1$2')
      .replace(/(^|[\s(])[*_]([^*_\s][^*_]*?)[*_](?=[\s).,;:!?،]|$)/g, '$1$2')
      .replace(/\\([\\`*_{}[\]()#+\-.!])/g, '$1');
    if (text) out.push({ code: false, text });
  }
  return out;
}

/** The notes for lang as blocks. */
export function noteBlocks(notes: string, lang: Lang): Block[] {
  const out: Block[] = [];
  let para: string[] = [];
  const flush = () => {
    if (para.length) out.push({ kind: 'para', spans: inline(para.join(' ')) });
    para = [];
  };
  for (const raw of sectionFor(notes, lang)) {
    const l = raw.trimEnd();
    const h = headingOf(l);
    if (h !== null) {
      flush();
      if (h && !SKIP_HEAD.test(h)) out.push({ kind: 'heading', spans: inline(h) });
      continue;
    }
    const item = /^\s*(?:[-*+]|\d+[.)])\s+(.*)$/.exec(l);
    if (item) {
      flush();
      out.push({ kind: 'item', spans: inline(item[1].trim()) });
      continue;
    }
    if (!l.trim() || /^\s*(-{3,}|\*{3,}|_{3,})\s*$/.test(l)) {
      flush();
      continue;
    }
    para.push(l.trim());
  }
  flush();
  return out.filter((b) => b.spans.some((s) => s.text));
}

export const plain = (spans: Span[]) => spans.map((s) => s.text).join('');

/** The first meaningful line of the notes for lang (for the banner). */
export function noteLine(notes: string, lang: Lang = 'en'): string {
  const b = noteBlocks(notes, lang).find((x) => x.kind !== 'heading');
  const l = b ? plain(b.spans).trim() : '';
  return l.length > 120 ? `${l.slice(0, 117)}…` : l;
}
