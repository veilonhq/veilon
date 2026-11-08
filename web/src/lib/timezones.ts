const FALLBACK = [
  'UTC', 'Asia/Tehran', 'Asia/Dubai', 'Asia/Baghdad', 'Asia/Kabul', 'Asia/Karachi', 'Asia/Kolkata', 'Asia/Tashkent', 'Asia/Baku', 'Asia/Yerevan',
  'Asia/Tbilisi', 'Asia/Riyadh', 'Asia/Qatar', 'Asia/Shanghai', 'Asia/Hong_Kong', 'Asia/Singapore', 'Asia/Tokyo', 'Asia/Seoul', 'Asia/Bangkok',
  'Asia/Jakarta', 'Europe/Istanbul', 'Europe/Moscow', 'Europe/London', 'Europe/Paris', 'Europe/Berlin', 'Europe/Amsterdam', 'Europe/Stockholm',
  'Europe/Helsinki', 'Europe/Kyiv', 'Europe/Madrid', 'Europe/Rome', 'Africa/Cairo', 'Africa/Johannesburg', 'Africa/Lagos', 'America/New_York',
  'America/Chicago', 'America/Denver', 'America/Los_Angeles', 'America/Toronto', 'America/Sao_Paulo', 'America/Mexico_City', 'Australia/Sydney',
  'Australia/Perth', 'Pacific/Auckland',
];

export interface TzOption {
  zone: string;
  /** "UTC+03:30" */
  offset: string;
  minutes: number;
}

function offsetMinutes(zone: string, at: Date): number | null {
  try {
    const parts = new Intl.DateTimeFormat('en-US', { timeZone: zone, timeZoneName: 'longOffset' }).formatToParts(at);
    const name = parts.find((p) => p.type === 'timeZoneName')?.value ?? '';
    if (name === 'GMT' || name === 'UTC') return 0;
    const m = /([+-])(\d{1,2})(?::?(\d{2}))?/.exec(name);
    if (!m) return null;
    const v = Number(m[2]) * 60 + Number(m[3] ?? 0);
    return m[1] === '-' ? -v : v;
  } catch {
    return null;
  }
}

export function formatOffset(min: number) {
  const sign = min < 0 ? '−' : '+';
  const a = Math.abs(min);
  return `UTC${sign}${String(Math.floor(a / 60)).padStart(2, '0')}:${String(a % 60).padStart(2, '0')}`;
}

let cache: TzOption[] | null = null;

/** Every IANA zone the browser knows, with its current UTC offset, sorted by offset then name. */
export function listTimeZones(): TzOption[] {
  if (cache) return cache;
  let zones: string[] = [];
  try {
    const sv = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf;
    if (sv) zones = sv('timeZone');
  } catch {
    zones = [];
  }
  if (!zones.length) zones = FALLBACK;
  if (!zones.includes('UTC')) zones = ['UTC', ...zones];
  if (!zones.includes('Asia/Tehran')) zones = [...zones, 'Asia/Tehran'];
  const now = new Date();
  const out: TzOption[] = [];
  for (const zone of zones) {
    const m = offsetMinutes(zone, now);
    if (m === null) continue;
    out.push({ zone, minutes: m, offset: formatOffset(m) });
  }
  out.sort((a, b) => a.minutes - b.minutes || a.zone.localeCompare(b.zone));
  cache = out;
  return out;
}
