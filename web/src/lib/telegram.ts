/** Telegram bot settings helpers (pure, unit tested). */

/** Most admin ids the panel keeps (same as the server). */
export const MAX_TG_ADMINS = 20;

const latin = (v: string) =>
  v.replace(/[۰-۹]/g, (d) => String(d.charCodeAt(0) - 0x06f0)).replace(/[٠-٩]/g, (d) => String(d.charCodeAt(0) - 0x0660));

export type AdminIdsResult = { ids: number[]; bad: string[] };

/**
 * Reads "123, 456 ۷۸۹" into numeric Telegram ids. Commas (Latin or Persian),
 * spaces and new lines separate; Persian and Arabic-Indic digits are read;
 * duplicates are dropped. Anything that is not a positive whole number is
 * returned in `bad`.
 */
export function parseAdminIds(text: string): AdminIdsResult {
  const ids: number[] = [];
  const bad: string[] = [];
  for (const raw of text.split(/[,،\s]+/)) {
    if (!raw) continue;
    const v = latin(raw);
    if (!/^[0-9]{1,15}$/.test(v) || Number(v) <= 0) {
      bad.push(raw);
      continue;
    }
    const id = Number(v);
    if (!ids.includes(id)) ids.push(id);
  }
  return { ids, bad };
}

/** The ids as the field shows them. */
export const formatAdminIds = (ids: number[]) => ids.join(', ');

/** Same check as the server: http, https or socks5 URL with a host. */
export function validProxy(v: string): boolean {
  const s = v.trim();
  if (!s) return true;
  try {
    const u = new URL(s);
    return ['http:', 'https:', 'socks5:', 'socks5h:'].includes(u.protocol) && !!u.hostname;
  } catch {
    return false;
  }
}

/** Same shape check as the server for a token from @BotFather. */
export const validToken = (v: string) => /^[0-9]{3,15}:[A-Za-z0-9_-]{30,64}$/.test(v.trim());
