import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Pencil, Plus, Power, Scale, Trash2 } from 'lucide-react';
import { useState } from 'react';
import type { TemplateInbound } from '@/api/xrayConfig';
import { EmptyState, Field, Section } from '@/components/misc';
import { Sheet } from '@/components/Overlay';
import { Select } from '@/components/Select';
import { useI18n } from '@/i18n';
import { confirmAction } from '@/store/feedback';
import {
  PORT_RE,
  RULE_PROTOCOLS,
  STRATEGIES,
  TAG_RE,
  balancerToForm,
  emptyRule,
  formToBalancer,
  formToRule,
  isDisabled,
  move,
  ruleHasCondition,
  ruleToForm,
  selected,
  splitWords,
  tagOf,
  type BalancerForm,
  type Obj,
  type RuleForm,
  type XrayTemplate,
} from './model';
import x from './Xray.module.css';
import s from '../pages.module.css';

type SetT = React.Dispatch<React.SetStateAction<XrayTemplate | null>>;
const str = (v: unknown) => (typeof v === 'string' ? v : '');
const lst = (v: unknown) => (Array.isArray(v) ? v.map(String) : v ? [String(v)] : []);
const toggleOff = (o: Obj) => {
  const c = { ...o };
  if (isDisabled(o)) delete c._disabled;
  else c._disabled = true;
  return c;
};

/** Short chips that say what a rule matches. */
function ruleChips(r: Obj, inbounds: TemplateInbound[], t: ReturnType<typeof useI18n>['t']): string[] {
  const out: string[] = [];
  const dom = lst(r.domain);
  const geos = dom.filter((d) => d.startsWith('geosite:')).map((d) => d.slice(8));
  const doms = dom.filter((d) => !d.startsWith('geosite:'));
  const ip = lst(r.ip);
  const geoip = ip.filter((d) => d.startsWith('geoip:')).map((d) => d.slice(6));
  const ips = ip.filter((d) => !d.startsWith('geoip:'));
  if (geos.length) out.push(`geosite: ${geos.slice(0, 3).join(', ')}${geos.length > 3 ? ' …' : ''}`);
  if (doms.length) out.push(doms.length === 1 ? doms[0] : t('xc.rule.nDomains', { n: doms.length }));
  if (geoip.length) out.push(`geoip: ${geoip.join(', ')}`);
  if (ips.length) out.push(ips.length === 1 ? ips[0] : t('xc.rule.nIPs', { n: ips.length }));
  if (r.port) out.push(`${t('xc.rule.port')} ${str(r.port)}`);
  if (r.sourcePort) out.push(`${t('xc.rule.sourcePort')} ${str(r.sourcePort)}`);
  if (r.network) out.push(str(r.network));
  for (const p of lst(r.protocol)) out.push(p);
  const ibs = lst(r.inboundTag).map((tag) => inbounds.find((i) => i.tag === tag)?.remark ?? tag);
  if (ibs.length) out.push(`${t('xc.rule.inbound')}: ${ibs.slice(0, 2).join(', ')}${ibs.length > 2 ? ' …' : ''}`);
  const us = lst(r.user);
  if (us.length) out.push(`${t('xc.rule.user')}: ${us.slice(0, 2).join(', ')}${us.length > 2 ? ' …' : ''}`);
  return out;
}

export function RulesTab({ tpl, set, inbounds }: { tpl: XrayTemplate; set: SetT; inbounds: TemplateInbound[] }) {
  const { t, n, dir } = useI18n();
  const [edit, setEdit] = useState<{ index: number; form: RuleForm } | null>(null);
  const rules = tpl.routing.rules;
  const patch = (fn: (r: Obj[]) => Obj[]) => set((d) => (d ? { ...d, routing: { ...d.routing, rules: fn(d.routing.rules) } } : d));
  const Arrow = dir === 'rtl' ? ArrowLeft : ArrowRight;
  const remove = async (i: number) => {
    if (!(await confirmAction({ title: t('xc.rule.deleteConfirm', { n: n(i + 1) }), danger: true, confirmLabel: t('common.delete') }))) return;
    patch((r) => r.filter((_, j) => j !== i));
  };
  const add = (
    <button type="button" className="btn btn--sm btn--primary" onClick={() => setEdit({ index: -1, form: emptyRule() })}>
      <Plus size={15} />
      {t('xc.rule.new')}
    </button>
  );
  return (
    <>
      <Section title={t('xc.rule.title')} className={x.sec} actions={add}>
        <p className={x.note}>{t('xc.rule.hint')}</p>
        {rules.length === 0 ? (
          <EmptyState kind="search" title={t('xc.rule.empty')} body={t('xc.rule.emptyBody')} />
        ) : (
          <ol className={x.list}>
            {rules.map((r, i) => {
              const off = isDisabled(r);
              const target = str(r.outboundTag) || str(r.balancerTag);
              const chips = ruleChips(r, inbounds, t);
              return (
                <li key={i} className={`${x.item} ${off ? x.itemOff : ''}`}>
                  <span className={x.idx}>{n(i + 1)}</span>
                  <div className={x.itemMain}>
                    <span className={x.itemTitle}>
                      <strong>{str(r._name) || t('xc.rule.unnamed')}</strong>
                      {off && <span className="chip chip--disabled">{t('xc.off')}</span>}
                    </span>
                    <span className={x.itemMeta}>
                      {chips.map((c) => (
                        <span key={c} className={`chip ${x.condChip}`}>
                          <bdi>{c}</bdi>
                        </span>
                      ))}
                      <span className={x.route}>
                        <Arrow size={14} aria-hidden="true" />
                        {!!r.balancerTag && <Scale size={13} aria-label={t('xc.bal.one')} />}
                        <strong className="ltr">{target}</strong>
                      </span>
                    </span>
                  </div>
                  <span className={x.actions}>
                    <button type="button" className="btn btn--sm btn--icon btn--ghost" disabled={i === 0} onClick={() => patch((a) => move(a, i, -1))} aria-label={t('xc.up')} title={t('xc.up')}>
                      <ArrowUp size={16} />
                    </button>
                    <button type="button" className="btn btn--sm btn--icon btn--ghost" disabled={i === rules.length - 1} onClick={() => patch((a) => move(a, i, 1))} aria-label={t('xc.down')} title={t('xc.down')}>
                      <ArrowDown size={16} />
                    </button>
                    <button type="button" className="btn btn--sm btn--icon btn--ghost" onClick={() => patch((a) => a.map((o, j) => (j === i ? toggleOff(o) : o)))} aria-label={off ? t('xc.turnOn') : t('xc.turnOff')} title={off ? t('xc.turnOn') : t('xc.turnOff')}>
                      <Power size={16} />
                    </button>
                    <button type="button" className="btn btn--sm btn--icon btn--ghost" onClick={() => setEdit({ index: i, form: ruleToForm(r) })} aria-label={t('common.edit')} title={t('common.edit')}>
                      <Pencil size={16} />
                    </button>
                    <button type="button" className="btn btn--sm btn--icon btn--ghost" onClick={() => remove(i)} aria-label={t('common.delete')} title={t('common.delete')}>
                      <Trash2 size={16} />
                    </button>
                  </span>
                </li>
              );
            })}
          </ol>
        )}
      </Section>
      <RuleSheet
        edit={edit}
        tpl={tpl}
        inbounds={inbounds}
        onClose={() => setEdit(null)}
        onSave={(f, i) => {
          patch((a) => {
            const prev = i >= 0 ? a[i] : undefined;
            const r = formToRule(f, prev);
            if (prev && isDisabled(prev)) r._disabled = true;
            return i >= 0 ? a.map((o, j) => (j === i ? r : o)) : [...a, r];
          });
          setEdit(null);
        }}
      />
    </>
  );
}

function RuleSheet({ edit, tpl, inbounds, onClose, onSave }: { edit: { index: number; form: RuleForm } | null; tpl: XrayTemplate; inbounds: TemplateInbound[]; onClose: () => void; onSave: (f: RuleForm, i: number) => void }) {
  const { t } = useI18n();
  const [f, setF] = useState<RuleForm>(emptyRule());
  const [key, setKey] = useState<unknown>(null);
  const [tried, setTried] = useState(false);
  if (edit !== key) {
    setKey(edit);
    if (edit) setF(edit.form);
    setTried(false);
  }
  const up = <K extends keyof RuleForm>(k: K, v: RuleForm[K]) => setF((d) => ({ ...d, [k]: v }));
  const flip = (k: 'protocols' | 'inbounds', v: string) => setF((d) => ({ ...d, [k]: d[k].includes(v) ? d[k].filter((x) => x !== v) : [...d[k], v] }));
  const outs = tpl.outbounds.map(tagOf);
  const bals = tpl.routing.balancers.map(tagOf);
  const portErr = f.port.trim() && !PORT_RE.test(f.port) ? t('xc.err.ports') : undefined;
  const sportErr = f.sourcePort.trim() && !PORT_RE.test(f.sourcePort) ? t('xc.err.ports') : undefined;
  const noCond = !ruleHasCondition(f);
  const noTarget = f.target === 'balancer' ? !f.balancer : !f.outbound;
  const submit = () => {
    setTried(true);
    if (portErr || sportErr || noCond || noTarget) return;
    onSave(f, edit!.index);
  };
  return (
    <Sheet
      open={!!edit}
      onClose={onClose}
      title={edit && edit.index >= 0 ? t('xc.rule.edit') : t('xc.rule.new')}
      footer={
        <>
          <button type="button" className="btn btn--ghost" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="button" className="btn btn--primary" onClick={submit}>
            {t('xc.done')}
          </button>
        </>
      }
    >
      <div className={s.form}>
        <Field label={t('xc.rule.name')} optional>
          <input className="input" value={f.name} onChange={(e) => up('name', e.target.value)} placeholder={t('xc.rule.namePh')} />
        </Field>
        <h3 className={s.subhead}>{t('xc.rule.when')}</h3>
        {tried && noCond && <p className={s.warn}>{t('xc.err.noCondition')}</p>}
        <Field label={t('xc.rule.geosite')} hint={t('xc.rule.geositeHint')} optional>
          <input className="input ltr" value={f.geosite} placeholder="category-ads-all, openai, ir" onChange={(e) => up('geosite', e.target.value)} />
        </Field>
        <Field label={t('xc.rule.domains')} hint={t('xc.rule.domainsHint')} optional>
          <textarea className="input ltr mono" rows={3} value={f.domains} placeholder={'domain:example.com\nfull:www.example.org\nkeyword:ads'} onChange={(e) => up('domains', e.target.value)} />
        </Field>
        <div className={s.row2}>
          <Field label={t('xc.rule.geoip')} hint={t('xc.rule.geoipHint')} optional>
            <input className="input ltr" value={f.geoip} placeholder="ir, private" onChange={(e) => up('geoip', e.target.value)} />
          </Field>
          <Field label={t('xc.rule.ips')} optional>
            <textarea className="input ltr mono" rows={2} value={f.ips} placeholder={'10.0.0.0/8\n1.1.1.1'} onChange={(e) => up('ips', e.target.value)} />
          </Field>
        </div>
        <div className={s.row2}>
          <Field label={t('xc.rule.port')} hint={t('xc.rule.portHint')} error={portErr} optional>
            <input className="input ltr num" value={f.port} placeholder="443, 8000-9000" onChange={(e) => up('port', e.target.value)} aria-invalid={!!portErr} />
          </Field>
          <Field label={t('xc.rule.network')}>
            <Select<RuleForm['network']>
              value={f.network}
              onChange={(v) => up('network', v)}
              ariaLabel={t('xc.rule.network')}
              options={[
                { value: '', label: t('xc.rule.anyNet') },
                { value: 'tcp', label: 'TCP' },
                { value: 'udp', label: 'UDP' },
                { value: 'tcp,udp', label: 'TCP + UDP' },
              ]}
            />
          </Field>
        </div>
        <div className="field">
          <span className="field__label">{t('xc.rule.protocol')}</span>
          <div className={`seg seg--wrap ${x.pick}`}>
            {RULE_PROTOCOLS.map((p) => (
              <button key={p} type="button" aria-pressed={f.protocols.includes(p)} onClick={() => flip('protocols', p)}>
                <span className="ltr">{p === 'bittorrent' ? 'BitTorrent' : p.toUpperCase()}</span>
              </button>
            ))}
          </div>
        </div>
        {inbounds.length > 0 && (
          <div className="field">
            <span className="field__label">{t('xc.rule.inbounds')}</span>
            <div className={`seg seg--wrap ${x.pick}`}>
              {inbounds.map((ib) => (
                <button key={ib.tag} type="button" aria-pressed={f.inbounds.includes(ib.tag)} onClick={() => flip('inbounds', ib.tag)}>
                  <bdi>{ib.remark}</bdi>
                </button>
              ))}
            </div>
          </div>
        )}
        <Field label={t('xc.rule.users')} hint={t('xc.rule.usersHint')} optional>
          <input className="input ltr" value={f.users} placeholder="alice, bob" onChange={(e) => up('users', e.target.value)} />
        </Field>
        <Field label={t('xc.rule.sourcePort')} error={sportErr} optional>
          <input className="input ltr num" value={f.sourcePort} onChange={(e) => up('sourcePort', e.target.value)} aria-invalid={!!sportErr} />
        </Field>
        <h3 className={s.subhead}>{t('xc.rule.then')}</h3>
        <div className="seg" role="radiogroup" aria-label={t('xc.rule.then')}>
          <button type="button" role="radio" aria-checked={f.target === 'outbound'} onClick={() => up('target', 'outbound')}>
            {t('xc.rule.toOutbound')}
          </button>
          <button type="button" role="radio" aria-checked={f.target === 'balancer'} disabled={!bals.length} onClick={() => up('target', 'balancer')}>
            {t('xc.rule.toBalancer')}
          </button>
        </div>
        {f.target === 'outbound' ? (
          <Field label={t('xc.rule.outbound')} error={tried && noTarget ? t('xc.err.required') : undefined}>
            <Select<string> value={f.outbound} onChange={(v) => up('outbound', v)} ariaLabel={t('xc.rule.outbound')} options={outs.map((o) => ({ value: o, label: <span className="ltr">{o}</span>, text: o, hint: o === 'blocked' ? t('xc.out.blockedHint') : o === 'direct' ? t('xc.out.directHint') : undefined }))} />
          </Field>
        ) : (
          <Field label={t('xc.rule.balancer')} error={tried && noTarget ? t('xc.err.required') : undefined}>
            <Select<string> value={f.balancer} onChange={(v) => up('balancer', v)} placeholder={t('xc.pick')} ariaLabel={t('xc.rule.balancer')} options={bals.map((o) => ({ value: o, label: <span className="ltr">{o}</span>, text: o }))} />
          </Field>
        )}
      </div>
    </Sheet>
  );
}

export function BalancersTab({ tpl, set }: { tpl: XrayTemplate; set: SetT }) {
  const { t, n } = useI18n();
  const [edit, setEdit] = useState<{ index: number; form: BalancerForm } | null>(null);
  const bals = tpl.routing.balancers;
  const outs = tpl.outbounds.map(tagOf);
  const patch = (fn: (b: Obj[]) => Obj[]) => set((d) => (d ? { ...d, routing: { ...d.routing, balancers: fn(d.routing.balancers) } } : d));
  const remove = async (i: number) => {
    const tag = tagOf(bals[i]);
    if (!(await confirmAction({ title: t('xc.bal.deleteConfirm', { tag }), body: t('xc.bal.deleteBody'), danger: true, confirmLabel: t('common.delete') }))) return;
    set((d) => (d ? { ...d, routing: { ...d.routing, balancers: d.routing.balancers.filter((_, j) => j !== i), rules: d.routing.rules.filter((r) => r.balancerTag !== tag) } } : d));
  };
  const add = (
    <button type="button" className="btn btn--sm btn--primary" disabled={outs.length < 2} onClick={() => setEdit({ index: -1, form: { tag: '', selector: [], strategy: 'leastPing', fallback: 'direct' } })}>
      <Plus size={15} />
      {t('xc.bal.new')}
    </button>
  );
  return (
    <>
      <Section title={t('xc.bal.title')} className={x.sec} actions={add}>
        <p className={x.note}>{t('xc.bal.hint')}</p>
        {bals.length === 0 ? (
          <EmptyState kind="search" title={t('xc.bal.empty')} body={t('xc.bal.emptyBody')} />
        ) : (
          <ol className={x.list}>
            {bals.map((b, i) => {
              const f = balancerToForm(b);
              const picks = selected(f.selector, outs);
              const off = isDisabled(b);
              const uses = tpl.routing.rules.filter((r) => r.balancerTag === f.tag).length;
              return (
                <li key={`${f.tag}-${i}`} className={`${x.item} ${off ? x.itemOff : ''}`}>
                  <span className={x.idx}>
                    <Scale size={15} />
                  </span>
                  <div className={x.itemMain}>
                    <span className={x.itemTitle}>
                      <strong className="ltr">{f.tag}</strong>
                      <span className="chip chip--violet">{t(`xc.bal.s.${f.strategy}`)}</span>
                      {off && <span className="chip chip--disabled">{t('xc.off')}</span>}
                    </span>
                    <span className={x.itemMeta}>
                      {picks.map((p) => (
                        <span key={p} className="chip ltr">
                          {p}
                        </span>
                      ))}
                      {!picks.length && <span className={x.dim}>{t('xc.bal.none')}</span>}
                      <span className={x.dim}>{t('xc.bal.usedBy', { n: n(uses) })}</span>
                    </span>
                  </div>
                  <span className={x.actions}>
                    <button type="button" className="btn btn--sm btn--icon btn--ghost" onClick={() => patch((a) => a.map((o, j) => (j === i ? toggleOff(o) : o)))} aria-label={off ? t('xc.turnOn') : t('xc.turnOff')} title={off ? t('xc.turnOn') : t('xc.turnOff')}>
                      <Power size={16} />
                    </button>
                    <button type="button" className="btn btn--sm btn--icon btn--ghost" onClick={() => setEdit({ index: i, form: f })} aria-label={t('common.edit')} title={t('common.edit')}>
                      <Pencil size={16} />
                    </button>
                    <button type="button" className="btn btn--sm btn--icon btn--ghost" onClick={() => remove(i)} aria-label={t('common.delete')} title={t('common.delete')}>
                      <Trash2 size={16} />
                    </button>
                  </span>
                </li>
              );
            })}
          </ol>
        )}
      </Section>
      <BalancerSheet
        edit={edit}
        outs={outs}
        taken={[...outs, ...bals.filter((_, j) => j !== edit?.index).map(tagOf)]}
        onClose={() => setEdit(null)}
        onSave={(f, i) => {
          set((d) => {
            if (!d) return d;
            const prev = i >= 0 ? d.routing.balancers[i] : undefined;
            const b = formToBalancer(f, prev);
            const balancers = i >= 0 ? d.routing.balancers.map((o, j) => (j === i ? b : o)) : [...d.routing.balancers, b];
            const old = prev ? tagOf(prev) : '';
            const rules = old && old !== f.tag ? d.routing.rules.map((r) => (r.balancerTag === old ? { ...r, balancerTag: f.tag } : r)) : d.routing.rules;
            return { ...d, routing: { ...d.routing, balancers, rules } };
          });
          setEdit(null);
        }}
      />
    </>
  );
}

function BalancerSheet({ edit, outs, taken, onClose, onSave }: { edit: { index: number; form: BalancerForm } | null; outs: string[]; taken: string[]; onClose: () => void; onSave: (f: BalancerForm, i: number) => void }) {
  const { t } = useI18n();
  const [f, setF] = useState<BalancerForm>({ tag: '', selector: [], strategy: 'leastPing', fallback: '' });
  const [extra, setExtra] = useState('');
  const [key, setKey] = useState<unknown>(null);
  const [tried, setTried] = useState(false);
  if (edit !== key) {
    setKey(edit);
    if (edit) {
      setF(edit.form);
      setExtra(edit.form.selector.filter((p) => !outs.includes(p)).join(', '));
    }
    setTried(false);
  }
  const exact = f.selector.filter((p) => outs.includes(p));
  const flip = (tag: string) => setF((d) => ({ ...d, selector: d.selector.includes(tag) ? d.selector.filter((x) => x !== tag) : [...d.selector, tag] }));
  const fullSel = [...exact, ...splitWords(extra).filter((p) => !exact.includes(p))];
  const tagErr = !TAG_RE.test(f.tag) ? t('xc.err.tag') : taken.includes(f.tag) ? t('xc.err.tagUsed') : undefined;
  const selErr = selected(fullSel, outs).length === 0 ? t('xc.err.selector') : undefined;
  const submit = () => {
    setTried(true);
    if (tagErr || selErr) return;
    onSave({ ...f, tag: f.tag.trim(), selector: fullSel }, edit!.index);
  };
  return (
    <Sheet
      open={!!edit}
      onClose={onClose}
      title={edit && edit.index >= 0 ? t('xc.bal.edit') : t('xc.bal.new')}
      footer={
        <>
          <button type="button" className="btn btn--ghost" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="button" className="btn btn--primary" onClick={submit}>
            {t('xc.done')}
          </button>
        </>
      }
    >
      <div className={s.form}>
        <Field label={t('xc.out.tag')} error={tried ? tagErr : undefined}>
          <input className="input ltr mono" value={f.tag} onChange={(e) => setF({ ...f, tag: e.target.value.replace(/\s/g, '-') })} aria-invalid={tried && !!tagErr} />
        </Field>
        <div className="field">
          <span className="field__label">{t('xc.bal.outbounds')}</span>
          <div className={`seg seg--wrap ${x.pick}`}>
            {outs
              .filter((o) => o !== 'blocked')
              .map((o) => (
                <button key={o} type="button" aria-pressed={f.selector.includes(o)} onClick={() => flip(o)}>
                  <span className="ltr">{o}</span>
                </button>
              ))}
          </div>
          {tried && selErr && <span className="field__error">{selErr}</span>}
        </div>
        <Field label={t('xc.bal.prefix')} hint={t('xc.bal.prefixHint')} optional>
          <input className="input ltr mono" value={extra} placeholder="nl-, de-" onChange={(e) => setExtra(e.target.value)} />
        </Field>
        <div className={s.row2}>
          <Field label={t('xc.bal.strategy')} hint={t(`xc.bal.h.${f.strategy}`)}>
            <Select<BalancerForm['strategy']> value={f.strategy} onChange={(v) => setF({ ...f, strategy: v })} ariaLabel={t('xc.bal.strategy')} options={STRATEGIES.map((v) => ({ value: v, label: t(`xc.bal.s.${v}`) }))} />
          </Field>
          <Field label={t('xc.bal.fallback')} hint={t('xc.bal.fallbackHint')}>
            <Select<string> value={f.fallback} onChange={(v) => setF({ ...f, fallback: v })} ariaLabel={t('xc.bal.fallback')} options={[{ value: '', label: t('common.none') }, ...outs.map((o) => ({ value: o, label: <span className="ltr">{o}</span>, text: o }))]} />
          </Field>
        </div>
      </div>
    </Sheet>
  );
}
