import type { Protocol } from '@/api/types';

type Shape = 'orbit' | 'wave' | 'shield' | 'grid';
const META: Record<Protocol, { code: string; a: string; b: string; shape: Shape }> = {
  vless: { code: 'VL', a: 'var(--accent)', b: 'var(--violet)', shape: 'orbit' },
  vmess: { code: 'VM', a: 'var(--violet)', b: 'var(--info)', shape: 'orbit' },
  trojan: { code: 'TJ', a: 'var(--warn)', b: 'var(--danger)', shape: 'orbit' },
  shadowsocks: { code: 'SS', a: 'var(--info)', b: 'var(--accent)', shape: 'orbit' },
  hysteria: { code: 'H2', a: 'var(--danger)', b: 'var(--warn)', shape: 'wave' },
  tuic: { code: 'TU', a: 'var(--info)', b: 'var(--violet)', shape: 'wave' },
  masque: { code: 'MQ', a: 'var(--violet)', b: 'var(--danger)', shape: 'wave' },
  wireguard: { code: 'WG', a: 'var(--accent)', b: 'var(--info)', shape: 'shield' },
  amneziawg: { code: 'AW', a: 'var(--warn)', b: 'var(--accent)', shape: 'shield' },
  tun: { code: 'TN', a: 'var(--info)', b: 'var(--accent)', shape: 'shield' },
  http: { code: 'HT', a: 'var(--accent)', b: 'var(--warn)', shape: 'grid' },
  mixed: { code: 'MX', a: 'var(--violet)', b: 'var(--accent)', shape: 'grid' },
  socks: { code: 'S5', a: 'var(--info)', b: 'var(--warn)', shape: 'grid' },
  tunnel: { code: 'FW', a: 'var(--warn)', b: 'var(--info)', shape: 'grid' },
  mtproto: { code: 'TG', a: 'var(--info)', b: 'var(--info)', shape: 'grid' },
};

export const protoColor = (p: string) => META[p as Protocol]?.a ?? 'var(--accent)';

/** A small generative emblem per protocol: a family motif + monogram. */
export function ProtoGlyph({ protocol, size = 48 }: { protocol: Protocol | string; size?: number }) {
  const m = META[protocol as Protocol] ?? { code: String(protocol).slice(0, 2).toUpperCase(), a: 'var(--accent)', b: 'var(--violet)', shape: 'orbit' as Shape };
  const id = `pg-${protocol}`;
  const g = `url(#${id})`;
  return (
    <svg width={size} height={size} viewBox="0 0 48 48" aria-hidden="true" style={{ flex: 'none' }}>
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor={m.a} />
          <stop offset="1" stopColor={m.b} />
        </linearGradient>
      </defs>
      <rect x="1" y="1" width="46" height="46" rx="15" fill="var(--panel-solid)" stroke="var(--line-2)" />
      {m.shape === 'orbit' && (
        <>
          <circle cx="24" cy="24" r="16" fill="none" stroke={g} strokeWidth="2" strokeDasharray="70 31" strokeLinecap="round" transform="rotate(-40 24 24)" />
          <circle cx="24" cy="24" r="11" fill="none" stroke={g} strokeOpacity=".35" strokeWidth="1" />
        </>
      )}
      {m.shape === 'wave' && (
        <>
          <path d="M8 33c5-6 9-6 14 0s9 6 18 0" fill="none" stroke={g} strokeWidth="2" strokeLinecap="round" />
          <path d="M8 15c5 6 9 6 14 0s9-6 18 0" fill="none" stroke={g} strokeOpacity=".4" strokeWidth="1.2" strokeLinecap="round" />
        </>
      )}
      {m.shape === 'shield' && (
        <path d="M24 7l14 5v10c0 9-6 15-14 19C16 37 10 31 10 22V12z" fill="none" stroke={g} strokeWidth="1.8" strokeLinejoin="round" strokeOpacity=".9" />
      )}
      {m.shape === 'grid' && (
        <>
          <rect x="9" y="9" width="30" height="30" rx="9" fill="none" stroke={g} strokeWidth="1.8" strokeDasharray="22 8" />
          <path d="M9 24h4M35 24h4" stroke={g} strokeOpacity=".5" strokeWidth="1.4" strokeLinecap="round" />
        </>
      )}
      <text x="24" y="28.5" textAnchor="middle" fontSize="12.5" fontWeight="700" fontFamily="Space Grotesk Variable, sans-serif" fill="var(--text)" letterSpacing="-.5" stroke="var(--panel-solid)" strokeWidth="3.2" paintOrder="stroke" strokeLinejoin="round">
        {m.code}
      </text>
      <circle cx="37.5" cy="12" r="2.6" fill={m.a} />
    </svg>
  );
}
