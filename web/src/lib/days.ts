/** Persian / Arabic-Indic digits to ASCII, everything else dropped. */
export const asciiDigits = (v: string) =>
  v
    .replace(/[۰-۹]/g, (d) => String(d.charCodeAt(0) - 0x06f0))
    .replace(/[٠-٩]/g, (d) => String(d.charCodeAt(0) - 0x0660))
    .replace(/\D/g, '');

/** Max days the free field takes (100 years). */
export const MAX_DAYS = 36500;

/**
 * Reads what was typed in a "number of days" field: the cleaned text to show
 * and the days (0 = empty, which the form rejects). Any whole number from 1.
 */
export function parseDays(typed: string): { draft: string; days: number } {
  const draft = asciiDigits(typed).replace(/^0+/, '').slice(0, 5);
  return { draft, days: draft ? Math.min(MAX_DAYS, Number(draft)) : 0 };
}

/** ASCII digits shown as Persian digits (for the Persian panel). */
export const persianDigits = (v: string) => v.replace(/[0-9]/g, (d) => String.fromCharCode(0x06f0 + Number(d)));
