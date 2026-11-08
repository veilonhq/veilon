/** Veilon mark: a signal target with a violet "V" sweep. */
export function Logo({ size = 32 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden="true" style={{ flex: 'none' }}>
      <rect width="64" height="64" rx="19" fill="var(--panel-solid)" stroke="var(--line-2)" />
      <circle cx="32" cy="32" r="20" fill="none" stroke="var(--accent)" strokeWidth="2.5" opacity=".35" />
      <circle cx="32" cy="32" r="12" fill="none" stroke="var(--accent)" strokeWidth="2.5" opacity=".7" />
      <circle cx="32" cy="32" r="4.5" fill="var(--accent)" />
      <path d="M19 21 L32 45 L45 21" fill="none" stroke="var(--violet)" strokeWidth="4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
