import { motion } from 'framer-motion';
import { AlertTriangle, Ban, Globe, Info, Loader2, Lock, RefreshCw, Server, ShieldCheck, Trash2, Upload, Zap } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { ApiError } from '@/api/client';
import { useAcme, useCerts, useDeleteCert, usePublicIP, useRenewCert, useRevokeCert, useSelfSignedCert, useSetCertAutoRenew, useSettings, useUploadCert } from '@/api/hooks';
import type { Cert } from '@/api/types';
import { EmptyState, ErrorState, Field, PageHeader, Section, Skeleton, Switch } from '@/components/misc';
import { useI18n } from '@/i18n';
import { confirmAction, toast } from '@/store/feedback';
import s from './pages.module.css';

const HOUR = 3_600_000;
const DAY = 24 * HOUR;
const msLeft = (c: Cert) => new Date(c.not_after).getTime() - Date.now();
const daysLeft = (c: Cert) => Math.ceil(msLeft(c) / DAY);
/** Validity period; estimated from the profile when not_before is unknown (as the panel does). */
function lifetime(c: Cert) {
  const na = new Date(c.not_after).getTime();
  const nb = c.not_before ? new Date(c.not_before).getTime() : na - (c.profile === 'shortlived' ? 160 * HOUR : 90 * DAY);
  return Math.max(HOUR, na - nb);
}
/** Short-lived (IP) certificates live ~6 days, so "4 days left" is healthy there: tone is relative to the lifetime. */
function toneOf(c: Cert) {
  const left = msLeft(c);
  if (left <= 0) return 'danger';
  if (left < Math.min(14 * DAY, lifetime(c) / 3) || (c.last_error && left < lifetime(c) / 2)) return left < DAY ? 'danger' : 'warn';
  return 'accent';
}
const isShort = (c: Cert) => c.profile === 'shortlived';

function useLeftLabel() {
  const { t } = useI18n();
  return (c: Cert) => {
    const left = msLeft(c);
    if (left <= 0) return t('certs.expired');
    if (left < 2 * DAY) return t('certs.hoursLeft', { n: Math.max(1, Math.floor(left / HOUR)) });
    return t('certs.daysLeft', { n: Math.floor(left / DAY) });
  };
}

function Timeline({ certs }: { certs: Cert[] }) {
  const { t, date, n } = useI18n();
  const ref = useRef<HTMLDivElement>(null);
  const [W, setW] = useState(480);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setW(Math.max(1, e.contentRect.width)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const horizon = Math.max(120, ...certs.map(daysLeft).map((d) => Math.min(d, 400) + 10));
  // fewer ticks on a narrow track so the day labels never touch
  const step = horizon > 200 ? (W < 420 ? 120 : 60) : W < 420 ? 60 : 30;
  const ticks = Array.from({ length: Math.floor(horizon / step) + 1 }, (_, i) => i * step);
  const pct = (d: number) => Math.max(0, Math.min(100, (d / horizon) * 100));
  const pos = (d: number) => `${pct(d)}%`;
  // Greedy lanes: each label occupies [x, x + width] (or to the inner side when
  // flipped near the far end); put it in the first lane where it fits.
  const sorted = [...certs].sort((a, b) => msLeft(a) - msLeft(b));
  const lanes: Array<Array<[number, number]>> = [[], [], [], []];
  const placed = sorted.map((c) => {
    const d = Math.max(0, Math.min(daysLeft(c), horizon));
    const flip = pct(d) > 62;
    const x = (pct(d) / 100) * W;
    const w = 26 + c.domain.length * 7.4;
    const span: [number, number] = flip ? [x - w, x + 8] : [x - 8, x + w];
    let lane = lanes.findIndex((l) => l.every(([a, b]) => span[1] + 6 < a || span[0] > b + 6));
    if (lane < 0) lane = lanes.reduce((best, l, i) => (l.length < lanes[best].length ? i : best), 0);
    lanes[lane].push(span);
    return { c, d, flip, lane };
  });
  return (
    <div className={s.timeline} role="list" aria-label={t('certs.timeline')}>
      <div className={s.tlTrack} ref={ref}>
        <span className={s.tlDanger} style={{ width: pos(14) }} />
        {ticks.map((d) => (
          <span key={d} className={s.tlTick} style={{ insetInlineStart: pos(d) }}>
            <i />
            <small>{d === 0 ? t('certs.today') : t('certs.tick', { n: n(d) })}</small>
          </span>
        ))}
        {placed.map(({ c, d, flip, lane }, i) => (
          <motion.span
            key={c.id}
            role="listitem"
            className={`${s.tlMark} ${flip ? s.tlFlip : ''} ${s['tl_' + toneOf(c)]}`}
            style={flip ? { insetInlineEnd: `calc(${100 - pct(d)}% - 7px)`, top: `${10 + lane * 28}px` } : { insetInlineStart: pos(d), top: `${10 + lane * 28}px` }}
            initial={{ scale: 0 }}
            animate={{ scale: 1 }}
            transition={{ delay: i * 0.03, type: 'spring', stiffness: 420, damping: 24 }}
            title={`${c.domain} · ${date(c.not_after)}`}
          >
            <b />
            <span className="ltr">{c.domain}</span>
          </motion.span>
        ))}
      </div>
    </div>
  );
}

function errText(e: unknown) {
  return e instanceof Error ? e.message : String(e);
}

function CertCard({ c }: { c: Cert }) {
  const { t, date, dateTime, relative } = useI18n();
  const leftLabel = useLeftLabel();
  const autoRenew = useSetCertAutoRenew();
  const renew = useRenewCert();
  const revoke = useRevokeCert();
  const del = useDeleteCert();
  const tone = toneOf(c);
  const acme = c.source === 'acme';
  const ids = c.identifiers?.length ? c.identifiers : [c.domain];
  const ratio = Math.max(0, Math.min(1, msLeft(c) / lifetime(c)));
  const Icon = c.source === 'self' ? ShieldCheck : c.source === 'upload' ? Upload : c.type === 'ip' ? Server : Globe;

  const onRenew = () =>
    renew.mutate(c.id, {
      onSuccess: () => toast.success(t('certs.renewed'), c.domain),
      onError: (e) => toast.error(t('common.error'), errText(e)),
    });
  const onRevoke = async () => {
    if (!(await confirmAction({ title: t('certs.revokeConfirm', { name: c.domain }), body: t('certs.revokeBody'), danger: true, confirmLabel: t('certs.revoke') }))) return;
    revoke.mutate(c.id, {
      onSuccess: () => toast.success(t('certs.revoked'), c.domain),
      onError: (e) => toast.error(t('common.error'), e instanceof ApiError && e.status === 409 ? t('certs.inUseHint') : errText(e)),
    });
  };
  const onDelete = async () => {
    if (!(await confirmAction({ title: t('certs.deleteConfirm', { name: c.domain }), body: t('certs.deleteBody'), danger: true, confirmLabel: t('common.delete') }))) return;
    del.mutate(c.id, {
      onSuccess: () => toast.success(t('certs.deleted')),
      onError: (e) => toast.error(t('common.error'), e instanceof ApiError && e.status === 409 ? t('certs.inUseHint') : errText(e)),
    });
  };
  const onAuto = (v: boolean) =>
    autoRenew.mutate(
      { id: c.id, auto_renew: v },
      { onSuccess: () => toast.success(v ? t('certs.autoRenewOn') : t('certs.autoRenewOff'), c.domain), onError: (e) => toast.error(t('common.error'), errText(e)) },
    );
  const renewDue = c.renew_at ? new Date(c.renew_at).getTime() <= Date.now() : false;
  const inUseTitle = c.in_use ? t('certs.inUseHint') : undefined;

  return (
    <article className={`glass ${s.certCard}`}>
      <div className={s.certTop}>
        <span className={`${s.certIcon} ${s['bgSoft_' + tone]}`}>
          <Icon size={20} />
        </span>
        <div className={s.certMain}>
          <strong className="ltr" title={ids.join(', ')}>
            {c.domain}
          </strong>
          <span className={s.certMeta}>
            <bdi>{c.issuer}</bdi> · {t(`certs.source.${c.source}`)}
          </span>
        </div>
        <div className={s.certSide}>
          <strong className={`num ${s['fg_' + tone]}`}>{leftLabel(c)}</strong>
          <span>{date(c.not_after)}</span>
        </div>
      </div>

      {(ids.length > 1 || isShort(c) || c.in_use) && (
        <div className={s.certChips}>
          {ids.length > 1 && ids.slice(1).map((x) => (
            <span key={x} className="chip ltr">
              {x}
            </span>
          ))}
          {isShort(c) && <span className="chip chip--on_hold">{t('certs.profile.shortlived')}</span>}
          {c.in_use && (
            <span className="chip chip--violet">
              <Lock size={11} />
              {t('certs.inUse')}
            </span>
          )}
        </div>
      )}

      <div className={s.lifeBar} aria-hidden="true">
        <i className={s['bg_' + tone]} style={{ transform: `scaleX(${Math.max(0.03, ratio)})` }} />
      </div>

      {/* always one line for ACME certs, so toggling auto-renew never changes the card height */}
      {acme && (
        <p className={`${s.certNote} ${c.auto_renew ? '' : s.certNoteOff}`}>
          <RefreshCw size={13} />
          {!c.auto_renew ? t('certs.autoRenewOffNote') : !c.renew_at ? t('certs.autoRenew') : renewDue ? t('certs.renewSoon') : t('certs.nextRenew', { when: relative(c.renew_at) })}
        </p>
      )}
      {c.last_error && (
        <div className={s.certError} role="note">
          <AlertTriangle size={15} />
          <div>
            <strong>{t('certs.lastError', { when: c.last_attempt_at ? dateTime(c.last_attempt_at) : '—' })}</strong>
            <code className="ltr">{c.last_error}</code>
          </div>
        </div>
      )}

      <div className={s.certActions}>
        {acme && (
          <label className={s.certAuto}>
            <Switch checked={c.auto_renew} onChange={onAuto} label={t('certs.autoRenew')} />
            <span>{t('certs.autoRenew')}</span>
          </label>
        )}
        <span className={s.certActionsSpacer} />
        {acme && (
          <button className="btn btn--sm" onClick={onRenew} disabled={renew.isPending}>
            {renew.isPending ? <Loader2 size={15} className="spin" /> : <RefreshCw size={15} />}
            {t('certs.renew')}
          </button>
        )}
        {acme ? (
          <button className="btn btn--sm btn--ghost btn--danger-ghost" onClick={onRevoke} disabled={c.in_use || revoke.isPending} title={inUseTitle} aria-label={`${t('certs.revoke')} ${c.domain}`}>
            {revoke.isPending ? <Loader2 size={15} className="spin" /> : <Ban size={15} />}
            {t('certs.revoke')}
          </button>
        ) : (
          <button className="btn btn--sm btn--icon btn--ghost" onClick={onDelete} disabled={c.in_use} title={inUseTitle ?? t('common.delete')} aria-label={`${t('common.delete')} ${c.domain}`}>
            <Trash2 size={16} />
          </button>
        )}
      </div>
    </article>
  );
}

type Mode = 'domain' | 'ip' | 'self' | 'upload';

function Elapsed({ since }: { since: number }) {
  const { n } = useI18n();
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const sec = Math.max(0, Math.floor((now - since) / 1000));
  return (
    <span className="num ltr">
      {n(Math.floor(sec / 60))}:{n(sec % 60, { minimumIntegerDigits: 2 })}
    </span>
  );
}

function IssuePanel() {
  const { t, n } = useI18n();
  const acme = useAcme();
  const upload = useUploadCert();
  const self = useSelfSignedCert();
  const settings = useSettings();
  const [mode, setMode] = useState<Mode>('domain');
  const [domain, setDomain] = useState('');
  const [email, setEmail] = useState('');
  const [ip, setIp] = useState('');
  const [ipTouched, setIpTouched] = useState(false);
  const [withV6, setWithV6] = useState(false);
  const [ip6, setIp6] = useState('');
  const [forPanel, setForPanel] = useState(false);
  const [certPem, setCertPem] = useState('');
  const [keyPem, setKeyPem] = useState('');
  const [err, setErr] = useState<{ msg: string; acme: boolean } | null>(null);
  const started = useRef(0);
  const pub = usePublicIP(mode === 'ip');

  // prefill the detected address once (the admin can still edit it)
  useEffect(() => {
    if (!ipTouched && pub.data?.ipv4 && !ip) setIp(pub.data.ipv4);
  }, [pub.data, ipTouched, ip]);
  useEffect(() => {
    if (withV6 && !ip6 && pub.data?.ipv6) setIp6(pub.data.ipv6);
  }, [withV6, pub.data, ip6]);

  const switchMode = (m: Mode) => {
    if (acme.isPending) return;
    setMode(m);
    setErr(null);
  };

  const panelUrl = (host: string) => {
    const st = settings.data;
    const h = host.includes(':') ? `[${host}]` : host;
    return st ? `https://${h}:${st.panel_port}/${st.panel_path}/` : `https://${h}/`;
  };

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    const onError = (x: unknown) => setErr({ msg: errText(x), acme: x instanceof ApiError && (x.status === 502 || x.code === 'acme_failed') });
    if (mode === 'self') {
      self.mutate({ domain: domain.trim() }, { onSuccess: () => { toast.success(t('certs.selfCreated'), domain.trim()); setDomain(''); }, onError });
      return;
    }
    if (mode === 'upload') {
      upload.mutate({ domain: domain.trim(), cert_pem: certPem, key_pem: keyPem }, { onSuccess: () => { toast.success(t('certs.uploaded')); setCertPem(''); setKeyPem(''); setDomain(''); }, onError });
      return;
    }
    started.current = Date.now();
    const body =
      mode === 'ip'
        ? { type: 'ip' as const, ip: ip.trim() || 'auto', ipv6: withV6 ? ip6.trim() || 'auto' : '', email: email.trim(), use_for_panel: forPanel }
        : { type: 'domain' as const, domain: domain.trim(), email: email.trim(), use_for_panel: forPanel };
    acme.mutate(body, {
      onSuccess: (c) => {
        toast.success(t('certs.issued'), c.identifiers?.join(', ') || c.domain);
        if (forPanel) toast.info(t('certs.panelSwitching', { url: panelUrl(c.domain) }));
        if (mode === 'domain') setDomain('');
      },
      onError,
    });
  };
  const busy = acme.isPending || upload.isPending || self.isPending;
  const v4 = pub.data?.candidates.filter((x) => x.family === 'ipv4') ?? [];
  const v6 = pub.data?.candidates.filter((x) => x.family === 'ipv6') ?? [];
  const tab = mode === 'upload' ? 'self' : mode;

  return (
    <Section className={s.sticky} title={t('certs.issueTitle')}>
      <div className={`seg ${s.certTabs}`} role="tablist" aria-label={t('certs.issueTitle')}>
        <button role="tab" type="button" aria-selected={tab === 'domain'} onClick={() => switchMode('domain')} disabled={acme.isPending && mode !== 'domain'}>
          <Globe size={15} />
          {t('certs.mode.domain')}
        </button>
        <button role="tab" type="button" aria-selected={tab === 'ip'} onClick={() => switchMode('ip')} disabled={acme.isPending && mode !== 'ip'}>
          <Server size={15} />
          {t('certs.mode.ip')}
        </button>
        <button role="tab" type="button" aria-selected={tab === 'self'} onClick={() => switchMode('self')} disabled={acme.isPending}>
          <ShieldCheck size={15} />
          {t('certs.selfSigned')}
        </button>
      </div>

      {/* The progress card sits on top of the (hidden) form in the same grid
          cell: the panel keeps its height while a certificate is issued, so
          nothing below or around it jumps. */}
      <div className={s.issueStack}>
        {acme.isPending && (
          <div className={s.issuing} role="status" aria-live="polite">
            <div className={s.issuingHead}>
              <Loader2 size={20} className="spin" />
              <strong>{t('certs.issuing')}</strong>
              <Elapsed since={started.current} />
            </div>
            <div className={s.indeterminate} aria-hidden="true">
              <i />
            </div>
            <p>{t('certs.issuingBody')}</p>
            <code className="ltr">{mode === 'ip' ? [ip || 'auto', withV6 ? ip6 || 'auto' : ''].filter(Boolean).join(' + ') : domain}</code>
          </div>
        )}
        <form className={`${s.form} ${acme.isPending ? s.formHidden : ''}`} onSubmit={submit} aria-hidden={acme.isPending || undefined}>
          {mode === 'domain' && (
            <>
              <Field label={t('certs.domain')} hint={t('certs.domainHint')}>
                <input className="input ltr" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="vpn.example.com" required autoCapitalize="none" spellCheck={false} inputMode="url" />
              </Field>
              <p className={s.hintBox}>
                <Info size={14} />
                <span>
                  <b className={s.validity}>{t('certs.validity90')}</b> {t('certs.domainValidity')}
                </span>
              </p>
            </>
          )}

          {mode === 'ip' && (
            <>
              <Field label={t('certs.ipv4')} hint={t('certs.ipConfirm')}>
                <span className={s.ipInput}>
                  <input
                    className="input ltr num"
                    value={ip}
                    onChange={(e) => {
                      setIpTouched(true);
                      setIp(e.target.value);
                    }}
                    placeholder={pub.isPending ? t('certs.detecting') : '203.0.113.10'}
                    autoCapitalize="none"
                    spellCheck={false}
                    inputMode="decimal"
                  />
                  {pub.isFetching && <Loader2 size={16} className={`spin ${s.ipSpin}`} />}
                </span>
              </Field>
              {/* one fixed-height row in every state (detecting / found / failed): nothing below moves when detection finishes */}
              <div className={s.ipChips} role="group" aria-label={t('certs.ipv4')} aria-busy={pub.isPending || undefined}>
                {pub.isPending ? (
                  <>
                    <span className={s.chipSkel} />
                    <span className={s.chipSkel} />
                  </>
                ) : v4.length ? (
                  v4.map((c) => (
                    <button key={c.ip} type="button" className="pill" aria-pressed={ip === c.ip} onClick={() => { setIpTouched(true); setIp(c.ip); }} title={t('certs.seenBy', { list: c.sources.join(', ') })}>
                      <span className="ltr num">{c.ip}</span>
                      <small>{n(c.sources.length)}×</small>
                    </button>
                  ))
                ) : (
                  <span className={s.ipChipsNote}>{t('certs.detectFailed')}</span>
                )}
              </div>
              <div className={s.switchRow}>
                <span>{t('certs.addIpv6')}</span>
                <Switch checked={withV6} onChange={setWithV6} label={t('certs.addIpv6')} />
              </div>
              {withV6 && (
                <>
                  <Field label={t('certs.ipv6')} hint={pub.data && !pub.data.ipv6 && !ip6 ? t('certs.noIpv6') : undefined}>
                    <input className="input ltr num" value={ip6} onChange={(e) => setIp6(e.target.value)} placeholder="2001:db8::1" autoCapitalize="none" spellCheck={false} />
                  </Field>
                  {v6.length > 1 && (
                    <div className={s.ipChips}>
                      {v6.map((c) => (
                        <button key={c.ip} type="button" className="pill" aria-pressed={ip6 === c.ip} onClick={() => setIp6(c.ip)} title={t('certs.seenBy', { list: c.sources.join(', ') })}>
                          <span className="ltr num">{c.ip}</span>
                        </button>
                      ))}
                    </div>
                  )}
                </>
              )}
              <p className={`${s.hintBox} ${s.hintWarn}`}>
                <Info size={14} />
                <span>
                  <b className={s.validity}>{t('certs.validity6')}</b> {t('certs.ipValidity')}
                </span>
              </p>
            </>
          )}

          {(mode === 'domain' || mode === 'ip') && (
            <>
              <Field label={t('certs.email')} optional hint={t('certs.emailHint')}>
                <input className="input ltr" type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" autoCapitalize="none" spellCheck={false} />
              </Field>
              <div className={s.switchRow}>
                <span>
                  {t('certs.useForPanel')}
                  <small>{t('certs.useForPanelHint')}</small>
                </span>
                <Switch checked={forPanel} onChange={setForPanel} label={t('certs.useForPanel')} />
              </div>
              <p className={s.port80}>
                <AlertTriangle size={14} />
                <span>{t('certs.port80')}</span>
              </p>
            </>
          )}

          {mode === 'self' && (
            <>
              <Field label={t('certs.domain')}>
                <input className="input ltr" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="www.example.com" required autoCapitalize="none" spellCheck={false} />
              </Field>
              <p className={s.hintBox}>
                <Info size={14} />
                <span>{t('certs.selfSignedHint')}</span>
              </p>
            </>
          )}

          {mode === 'upload' && (
            <>
              <Field label={t('certs.domain')} optional hint={t('certs.domainFromCert')}>
                <input className="input ltr" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="vpn.example.com" autoCapitalize="none" spellCheck={false} />
              </Field>
              <Field label={t('certs.certPem')}>
                <textarea className="input mono ltr" rows={5} value={certPem} onChange={(e) => setCertPem(e.target.value)} placeholder="-----BEGIN CERTIFICATE-----" required />
              </Field>
              <Field label={t('certs.keyPem')}>
                <textarea className="input mono ltr" rows={5} value={keyPem} onChange={(e) => setKeyPem(e.target.value)} placeholder="-----BEGIN PRIVATE KEY-----" required />
              </Field>
            </>
          )}

          {err && (
            <div className={s.certError} role="alert">
              <AlertTriangle size={15} />
              <div>
                {err.acme && <strong>{t('certs.failed')}</strong>}
                <code className="ltr">{err.msg}</code>
                {err.acme && <span>{t('certs.failedHint')}</span>}
              </div>
            </div>
          )}

          <button className="btn btn--primary btn--block" disabled={busy || (mode === 'ip' && !ip.trim() && pub.isPending)}>
            {busy ? <Loader2 size={17} className="spin" /> : mode === 'self' ? <ShieldCheck size={17} /> : mode === 'upload' ? <Upload size={17} /> : <Zap size={17} />}
            {busy ? t('common.saving') : mode === 'domain' ? t('certs.issueDomain') : mode === 'ip' ? t('certs.issueIp') : mode === 'self' ? t('certs.selfSignedBtn') : t('certs.uploadBtn')}
          </button>
          {(mode === 'self' || mode === 'upload') && (
            <button type="button" className={s.linkBtn} onClick={() => switchMode(mode === 'upload' ? 'self' : 'upload')}>
              {mode === 'upload' ? t('certs.backToIssue') : t('certs.uploadInstead')}
            </button>
          )}
        </form>
      </div>
    </Section>
  );
}

export default function CertsPage() {
  const { t } = useI18n();
  const q = useCerts();
  const sorted = [...(q.data ?? [])].sort((a, b) => msLeft(a) - msLeft(b));
  // ids present on first render appear instantly; certificates added later
  // (issued, uploaded) unfold in place instead of pushing the list in one jump
  const known = useRef<Set<number> | null>(null);
  if (!known.current && q.data) known.current = new Set(q.data.map((c) => c.id));

  return (
    <div>
      <PageHeader eyebrow={t('certs.subtitle', { n: q.data?.length ?? 0 })} title={t('certs.title')} />
      <div className={s.twoCol}>
        <div className={s.stack}>
          {(q.isPending || (q.data && q.data.length > 0)) && (
            <Section title={t('certs.timeline')}>{q.data ? <Timeline certs={q.data} /> : <Skeleton h={166} r={16} />}</Section>
          )}
          {q.isError ? (
            <ErrorState error={q.error} onRetry={() => q.refetch()} />
          ) : q.isPending ? (
            <div className={s.certList} aria-busy="true">
              <Skeleton h={150} r={22} />
              <Skeleton h={150} r={22} />
            </div>
          ) : sorted.length === 0 ? (
            <Section>
              <EmptyState kind="certs" title={t('certs.empty.title')} body={t('certs.empty.body')} />
            </Section>
          ) : (
            <div className={s.certList}>
              {sorted.map((c) =>
                known.current?.has(c.id) ? (
                  <CertCard key={c.id} c={c} />
                ) : (
                  <motion.div key={c.id} className={s.certUnfold} initial={{ height: 0 }} animate={{ height: 'auto' }} transition={{ duration: 0.35, ease: [0.22, 1, 0.36, 1] }} onAnimationComplete={() => known.current?.add(c.id)}>
                    <CertCard c={c} />
                  </motion.div>
                ),
              )}
            </div>
          )}
        </div>
        <IssuePanel />
      </div>
    </div>
  );
}
