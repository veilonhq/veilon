import { AlertTriangle, Braces, Check, FlaskConical, Globe, Loader2, Network, Plus, RotateCcw, Scale, Settings2, Split, Trash2, Wand2 } from 'lucide-react';
import { AnimatePresence, motion } from 'framer-motion';
import { useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useApplyTemplate, useTestTemplate, useXrayTemplate, type TemplateResult } from '@/api/xrayConfig';
import { ErrorState, Field, PageHeader, Section, Skeleton, Switch } from '@/components/misc';
import { Select } from '@/components/Select';
import { useI18n, type MessageKey } from '@/i18n';
import { toast } from '@/store/feedback';
import { dnsToForm, formToDns, tagOf, type DnsForm, type XrayTemplate } from './xray/model';
import { OutboundsTab } from './xray/Outbounds';
import { BalancersTab, RulesTab } from './xray/Rules';
import x from './xray/Xray.module.css';
import s from './pages.module.css';

type Tab = 'basic' | 'outbounds' | 'routing' | 'balancers' | 'dns' | 'advanced';
const TABS: { id: Tab; key: MessageKey; icon: typeof Globe }[] = [
  { id: 'basic', key: 'xc.tab.basic', icon: Settings2 },
  { id: 'outbounds', key: 'xc.tab.outbounds', icon: Network },
  { id: 'routing', key: 'xc.tab.routing', icon: Split },
  { id: 'balancers', key: 'xc.tab.balancers', icon: Scale },
  { id: 'dns', key: 'xc.tab.dns', icon: Globe },
  { id: 'advanced', key: 'xc.tab.advanced', icon: Braces },
];

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

export default function XrayConfigPage() {
  const { t } = useI18n();
  const q = useXrayTemplate();
  const test = useTestTemplate();
  const apply = useApplyTemplate();
  const [params, setParams] = useSearchParams();
  const tab = (TABS.find((x) => x.id === params.get('tab'))?.id ?? 'basic') as Tab;
  const [draft, setDraft] = useState<XrayTemplate | null>(null);
  const [result, setResult] = useState<TemplateResult | null>(null);
  const [jsonBad, setJsonBad] = useState(false);

  useEffect(() => {
    if (q.data && !draft) setDraft(q.data.template);
  }, [q.data, draft]);

  const base = q.data?.template;
  const dirty = !!draft && !!base && !same(draft, base);
  const busy = test.isPending || apply.isPending;

  const go = (id: Tab) => {
    const p = new URLSearchParams(params);
    if (id === 'basic') p.delete('tab');
    else p.set('tab', id);
    setParams(p, { replace: true });
  };

  const runTest = () => {
    if (!draft) return;
    setResult(null);
    test.mutate(draft, {
      onSuccess: (r) => {
        setResult(r);
        if (r.ok) toast.success(r.tested ? t('xc.testOk') : t('xc.testUntested'));
      },
      onError: (e) => setResult({ ok: false, tested: false, applied: false, rolled_back: false, error: (e as Error).message, warnings: [] }),
    });
  };
  const runApply = () => {
    if (!draft) return;
    setResult(null);
    apply.mutate(draft, {
      onSuccess: (r) => {
        setResult(r.result);
        if (r.result.applied) {
          setDraft(r.template);
          toast.success(t('xc.applied'));
        }
      },
      onError: (e) => setResult({ ok: false, tested: false, applied: false, rolled_back: false, error: (e as Error).message, warnings: [] }),
    });
  };

  if (q.isError) return <ErrorState error={q.error} onRetry={() => q.refetch()} />;

  return (
    <div className={x.page}>
      <PageHeader title={t('xc.title')} subtitle={t('xc.subtitle')} />
      <div className={`seg seg--wrap ${x.tabs}`} role="tablist" aria-label={t('xc.title')}>
        {TABS.map((it) => (
          <button key={it.id} role="tab" type="button" aria-selected={tab === it.id} onClick={() => go(it.id)}>
            <it.icon size={15} aria-hidden="true" />
            {t(it.key)}
          </button>
        ))}
      </div>

      {result && !result.ok && (
        <div className={x.resultBad} role="alert">
          <AlertTriangle size={18} aria-hidden="true" />
          <div>
            <strong>{result.rolled_back ? t('xc.rolledBack') : t('xc.rejected')}</strong>
            {result.error && <pre className={`${x.errText} ltr`}>{result.error}</pre>}
          </div>
          <button className="btn btn--sm btn--ghost" onClick={() => setResult(null)}>
            {t('common.close')}
          </button>
        </div>
      )}
      {result && result.warnings.length > 0 && (
        <div className={x.resultWarn}>
          <AlertTriangle size={16} aria-hidden="true" />
          <ul>
            {result.warnings.map((w) => (
              <li key={w} className="ltr">
                {w}
              </li>
            ))}
          </ul>
        </div>
      )}

      {!draft || !q.data ? (
        <div className={x.stack}>
          <Skeleton h={220} r={24} />
          <Skeleton h={160} r={24} />
        </div>
      ) : (
        <div className={x.stack}>
          {tab === 'basic' && <BasicTab t={draft} set={setDraft} />}
          {tab === 'outbounds' && <OutboundsTab tpl={draft} set={setDraft} />}
          {tab === 'routing' && <RulesTab tpl={draft} set={setDraft} inbounds={q.data.inbounds} />}
          {tab === 'balancers' && <BalancersTab tpl={draft} set={setDraft} />}
          {tab === 'dns' && <DnsTab t={draft} set={setDraft} />}
          {tab === 'advanced' && <AdvancedTab t={draft} set={setDraft} onBad={setJsonBad} />}
        </div>
      )}

      <AnimatePresence>
        {dirty && (
          <motion.div className={`${s.saveBar} ${x.saveBar}`} initial={{ y: 30, opacity: 0, x: '-50%' }} animate={{ y: 0, opacity: 1, x: '-50%' }} exit={{ y: 30, opacity: 0, x: '-50%' }}>
            <span className={x.saveText}>{jsonBad ? t('xc.jsonInvalid') : t('set.unsaved')}</span>
            <button type="button" className="btn btn--sm btn--ghost" disabled={busy} onClick={() => { setDraft(base!); setResult(null); }} title={t('xc.discard')} aria-label={t('xc.discard')}>
              <RotateCcw size={15} />
            </button>
            <button type="button" className="btn btn--sm btn--ghost" disabled={busy || jsonBad} onClick={runTest}>
              {test.isPending ? <Loader2 size={15} className="spin" /> : <FlaskConical size={15} />}
              {t('xc.test')}
            </button>
            <button type="button" className="btn btn--sm btn--primary" disabled={busy || jsonBad} onClick={runApply}>
              {apply.isPending ? <Loader2 size={15} className="spin" /> : <Check size={15} />}
              {t('xc.apply')}
            </button>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

type SetT = React.Dispatch<React.SetStateAction<XrayTemplate | null>>;

function BasicTab({ t: tpl, set }: { t: XrayTemplate; set: SetT }) {
  const { t } = useI18n();
  const tags = tpl.outbounds.map(tagOf);
  const def = tags[0] ?? 'direct';
  const patch = (fn: (d: XrayTemplate) => XrayTemplate) => set((d) => (d ? fn(d) : d));
  const makeDefault = (tag: string) =>
    patch((d) => {
      const i = d.outbounds.findIndex((o) => tagOf(o) === tag);
      if (i <= 0) return d;
      const outs = [...d.outbounds];
      const [it] = outs.splice(i, 1);
      return { ...d, outbounds: [{ ...it, _disabled: undefined }, ...outs].map((o) => JSON.parse(JSON.stringify(o))) };
    });
  return (
    <>
      <Section title={t('xc.basic.routing')}>
        <div className={s.form}>
          <div className={s.row2}>
            <Field label={t('xc.basic.default')} hint={t('xc.basic.defaultHint')}>
              <Select<string> value={def} onChange={makeDefault} ariaLabel={t('xc.basic.default')} ltr options={tags.map((v) => ({ value: v, label: <span className="ltr">{v}</span>, text: v }))} />
            </Field>
            <Field label={t('xc.basic.strategy')} hint={t(`xc.ds.${tpl.routing.domainStrategy ?? 'AsIs'}` as MessageKey)}>
              <Select<string>
                value={tpl.routing.domainStrategy ?? 'AsIs'}
                onChange={(v) => patch((d) => ({ ...d, routing: { ...d.routing, domainStrategy: v } }))}
                ariaLabel={t('xc.basic.strategy')}
                options={['AsIs', 'IPIfNonMatch', 'IPOnDemand'].map((v) => ({ value: v, label: <span className="ltr">{v}</span>, text: v }))}
              />
            </Field>
          </div>
          <div className={s.switchCard}>
            <span>
              <strong>{t('xc.basic.private')}</strong>
              <small>{t('xc.basic.privateHint')}</small>
            </span>
            <Switch checked={tpl.block_private !== false} onChange={(v) => patch((d) => ({ ...d, block_private: v ? undefined : false }))} label={t('xc.basic.private')} />
          </div>
          <p className={x.note}>
            {t('xc.basic.adNote')} <Link to="/settings">{t('nav.settings')}</Link>
          </p>
        </div>
      </Section>
      <Section title={t('xc.basic.log')}>
        <div className={s.form}>
          <Field label={t('xc.basic.level')}>
            <Select<string>
              value={tpl.log.loglevel ?? 'warning'}
              onChange={(v) => patch((d) => ({ ...d, log: { ...d.log, loglevel: v } }))}
              ariaLabel={t('xc.basic.level')}
              options={['debug', 'info', 'warning', 'error', 'none'].map((v) => ({ value: v, label: t(`xc.lv.${v}` as MessageKey), hint: v }))}
            />
          </Field>
          <div className={s.switchCard}>
            <span>
              <strong>{t('xc.basic.dnsLog')}</strong>
              <small>{t('xc.basic.dnsLogHint')}</small>
            </span>
            <Switch checked={!!tpl.log.dnsLog} onChange={(v) => patch((d) => ({ ...d, log: { ...d.log, dnsLog: v || undefined } }))} label={t('xc.basic.dnsLog')} />
          </div>
        </div>
      </Section>
    </>
  );
}

function DnsTab({ t: tpl, set }: { t: XrayTemplate; set: SetT }) {
  const { t } = useI18n();
  const f = useMemo(() => dnsToForm(tpl.dns), [tpl.dns]);
  const put = (nf: DnsForm) => set((d) => (d ? { ...d, dns: formToDns(nf, d.dns) } : d));
  const upd = (i: number, k: 'address' | 'domains', v: string) => put({ ...f, servers: f.servers.map((r, j) => (j === i ? { ...r, [k]: v } : r)) });
  return (
    <Section title={t('xc.dns.title')}>
      <div className={s.form}>
        <div className={s.switchCard}>
          <span>
            <strong>{t('xc.dns.custom')}</strong>
            <small>{t('xc.dns.customHint')}</small>
          </span>
          <Switch
            checked={f.enabled}
            onChange={(v) => put(v ? { ...f, enabled: true, servers: f.servers.length ? f.servers : [{ address: '1.1.1.1', domains: '' }, { address: '8.8.8.8', domains: '' }] } : { ...f, enabled: false })}
            label={t('xc.dns.custom')}
          />
        </div>
        {f.enabled && (
          <>
            <div className={x.dnsRows}>
              {f.servers.map((r, i) => (
                <div key={i} className={x.dnsRow}>
                  <Field label={t('xc.dns.server', { n: i + 1 })}>
                    <input className="input ltr mono" value={r.address} placeholder="https://1.1.1.1/dns-query" onChange={(e) => upd(i, 'address', e.target.value)} />
                  </Field>
                  <Field label={t('xc.dns.domains')} optional>
                    <input className="input ltr" value={r.domains} placeholder="geosite:ir, domain:example.com" onChange={(e) => upd(i, 'domains', e.target.value)} />
                  </Field>
                  <button type="button" className={`btn btn--icon btn--ghost ${x.rowDel}`} aria-label={t('common.delete')} title={t('common.delete')} onClick={() => put({ ...f, servers: f.servers.filter((_, j) => j !== i) })}>
                    <Trash2 size={16} />
                  </button>
                </div>
              ))}
            </div>
            <button type="button" className="btn btn--sm" onClick={() => put({ ...f, servers: [...f.servers, { address: '', domains: '' }] })}>
              <Plus size={15} />
              {t('xc.dns.add')}
            </button>
            <div className={s.row2}>
              <Field label={t('xc.dns.query')}>
                <Select<DnsForm['queryStrategy']>
                  value={f.queryStrategy}
                  onChange={(v) => put({ ...f, queryStrategy: v })}
                  ariaLabel={t('xc.dns.query')}
                  options={[
                    { value: 'UseIP', label: t('xc.dns.qBoth') },
                    { value: 'UseIPv4', label: t('xc.dns.q4') },
                    { value: 'UseIPv6', label: t('xc.dns.q6') },
                  ]}
                />
              </Field>
              <div className={`${s.switchCard} ${x.inlineSwitch}`}>
                <span>
                  <strong>{t('xc.dns.noCache')}</strong>
                </span>
                <Switch checked={f.disableCache} onChange={(v) => put({ ...f, disableCache: v })} label={t('xc.dns.noCache')} />
              </div>
            </div>
            <Field label={t('xc.dns.hosts')} hint={t('xc.dns.hostsHint')} optional>
              <textarea className="input ltr mono" rows={3} value={f.hosts} placeholder={'example.com 1.2.3.4\ndomain:ads.example 127.0.0.1'} onChange={(e) => put({ ...f, hosts: e.target.value })} />
            </Field>
          </>
        )}
      </div>
    </Section>
  );
}

function AdvancedTab({ t: tpl, set, onBad }: { t: XrayTemplate; set: SetT; onBad: (b: boolean) => void }) {
  const { t } = useI18n();
  const pretty = (v: unknown) => JSON.stringify(v, null, 2);
  const [text, setText] = useState(() => pretty(tpl));
  const [err, setErr] = useState<string | null>(null);
  // follow outside changes (discard, apply) while the text is valid
  useEffect(() => {
    if (!err) setText(pretty(tpl));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tpl]);
  useEffect(() => () => onBad(false), [onBad]);
  const change = (v: string) => {
    setText(v);
    try {
      const p = JSON.parse(v) as XrayTemplate;
      if (!p || typeof p !== 'object' || Array.isArray(p)) throw new Error(t('xc.adv.notObject'));
      if (!Array.isArray(p.outbounds)) throw new Error(t('xc.adv.needOutbounds'));
      p.log ??= {};
      p.routing ??= { rules: [], balancers: [] };
      p.routing.rules ??= [];
      p.routing.balancers ??= [];
      setErr(null);
      onBad(false);
      set(p);
    } catch (e) {
      setErr((e as Error).message);
      onBad(true);
    }
  };
  return (
    <Section
      title={t('xc.adv.title')} className={x.sec}
      actions={
        <button type="button" className="btn btn--sm btn--ghost" disabled={!!err} onClick={() => setText(pretty(tpl))}>
          <Wand2 size={15} />
          {t('xc.adv.format')}
        </button>
      }
    >
      <p className={x.note}>{t('xc.adv.hint')}</p>
      <textarea className={`input ltr mono ${x.json}`} spellCheck={false} value={text} onChange={(e) => change(e.target.value)} aria-invalid={!!err} aria-label={t('xc.adv.title')} />
      {err && (
        <p className={`${s.warn} ${x.jsonErr}`} role="alert">
          <AlertTriangle size={15} /> <span className="ltr">{err}</span>
        </p>
      )}
    </Section>
  );
}
