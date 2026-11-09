/** How many releases the server lists (keep in sync with xray.MaxReleases). */
export const MAX_RELEASES = 5;

/** "v26.10.31" → [26, 10, 31] */
function verParts(v: string) {
  return v
    .replace(/^v/, '')
    .split(/[.-]/)
    .map((x) => parseInt(x, 10))
    .filter((x) => !Number.isNaN(x));
}

export function cmpVer(a: string, b: string) {
  const x = verParts(a);
  const y = verParts(b);
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] ?? 0) - (y[i] ?? 0);
    if (d) return d;
  }
  return 0;
}

export const tagOf = (v?: string) => (v ? (v.startsWith('v') ? v : `v${v}`) : '');

export interface VersionItem {
  tag: string;
  current: boolean;
}

/**
 * The picker's entries: the newest MAX_RELEASES releases, plus the installed
 * version when it is not among them (older, or a build no longer listed),
 * newest first. The installed one is marked current.
 */
export function versionItems(releases: { tag: string }[], current: string): VersionItem[] {
  const tags = releases.slice(0, MAX_RELEASES).map((r) => r.tag);
  if (current && !tags.includes(current)) tags.push(current);
  return tags.sort((a, b) => cmpVer(b, a)).map((tag) => ({ tag, current: tag === current }));
}
