import { ArrowDown, ArrowUp, Link2, Loader2, Pencil, Plus, Power, Star, Trash2 } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useParseLink } from '@/api/xrayConfig';
import { Field, PasswordInput, Section, Switch } from '@/components/misc';
import { Sheet } from '@/components/Overlay';
import { Select } from '@/components/Select';
import { useI18n, type MessageKey } from '@/i18n';
import { confirmAction, toast } from '@/store/feedback';
import {
  FINGERPRINTS,
  NETWORKS,
  OUT_PROTOCOLS,
  RESERVED,
  SS_METHODS,
  emptyOut,
  formToOut,
  hasStream,
  isDisabled,
  move,
  outErrors,
  outTarget,
  outToForm,
  outboundUsers,
  renameOutbound,
  tagOf,
  type Obj,
  type OutForm,
  type OutProtocol,
  type XrayTemplate,
} from './model';
import x from './Xray.module.css';
import s from '../pages.module.css';

type SetT = React.Dispatch<React.SetStateAction<XrayTemplate | null>>;

const PROTO_LABEL: Record<OutProtocol, string> = {
  vless: 'VLESS', vmess: 'VMess', trojan: 'Trojan', shadowsocks: 'Shadowsocks', socks: 'SOCKS5', http: 'HTTP', wireguard: 'WireGuard', freedom: 'Freedom', blackhole: 'Blackhole',
};

export function OutboundsTab({ tpl, set }: { tpl: XrayTemplate; set: SetT }) {
  const { t, n } = useI18n();
  const [edit, setEdit] = useState<{ index: number; form: OutForm } | null>(null);
  const [importing, setImporting] = useState(false);
  const outs = tpl.outbounds;
  const patch = (fn: (d: XrayTemplate) => XrayTemplate) => set((d) => (d ? fn(d) : d));

  const remove = async (i: number) => {
    const tag = tagOf(outs[i]);
    const used = outboundUsers(tpl, tag);
    if (used > 0) {
      toast.error(t('xc.out.inUse', { tag, n: n(used) }));
      return;
    }
    if (!(await confirmAction({ title: t('xc.out.deleteConfirm', { tag }), danger: true, confirmLabel: t('common.delete') }))) return;
    patch((d) => ({ ...d, outbounds: d.outbounds.filter((_, j) => j !== i) }));
  };
  const toggle = (i: number) =>
    patch((d) => ({ ...d, outbounds: d.outbounds.map((o, j) => (j === i ? (isDisabled(o) ? omit(o, '_disabled') : { ...o, _disabled: true }) : o)) }));
  const save = (form: OutForm, index: number) => {
    patch((d) => {
      const prev = index >= 0 ? d.outbounds[index] : undefined;
      const out = formToOut(form, prev);
      const outbounds = index >= 0 ? d.outbounds.map((o, j) => (j === index ? out : o)) : [...d.outbounds, out];
      const next = { ...d, outbounds };
      return prev ? renameOutbound(next, tagOf(prev), form.tag.trim()) : next;
    });
    setEdit(null);
  };

  return (
    <>
      <Section
        title={t('xc.out.title')} className={x.sec}
        actions={
          <span className={x.headBtns}>
            <button type="button" className="btn btn--sm" onClick={() => setImporting(true)}>
              <Link2 size={15} />
              {t('xc.out.import')}
            </button>
            <button type="button" className="btn btn--sm btn--primary" onClick={() => setEdit({ index: -1, form: emptyOut('vless') })}>
              <Plus size={15} />
              {t('xc.out.new')}
            </button>
          </span>
        }
      >
        <p className={x.note}>{t('xc.out.hint')}</p>
        <ol className={x.list}>
          {outs.map((o, i) => {
            const tag = tagOf(o);
            const reserved = RESERVED.includes(tag);
            const off = isDisabled(o) && !reserved;
            const target = outTarget(o);
            return (
              <li key={`${tag}-${i}`} className={`${x.item} ${off ? x.itemOff : ''}`}>
                <span className={x.idx}>{n(i + 1)}</span>
                <div className={x.itemMain}>
                  <span className={x.itemTitle}>
                    <strong className="ltr">{tag}</strong>
                    {i === 0 && (
                      <span className="chip chip--active">
                        <Star size={11} />
                        {t('xc.out.default')}
                      </span>
                    )}
                    {off && <span className="chip chip--disabled">{t('xc.off')}</span>}
                  </span>
                  <span className={x.itemMeta}>
                    <span className="chip">{PROTO_LABEL[o.protocol as OutProtocol] ?? String(o.protocol)}</span>
                    {target && <span className={`${x.mono} ltr`}>{target}</span>}
                    {reserved && <span className={x.dim}>{t(tag === 'direct' ? 'xc.out.directHint' : 'xc.out.blockedHint')}</span>}
                  </span>
                </div>
                <span className={x.actions}>
                  <button type="button" className="btn btn--sm btn--icon btn--ghost" disabled={i === 0} onClick={() => patch((d) => ({ ...d, outbounds: move(d.outbounds, i, -1) }))} aria-label={t('xc.up')} title={t('xc.up')}>
                    <ArrowUp size={16} />
                  </button>
                  <button type="button" className="btn btn--sm btn--icon btn--ghost" disabled={i === outs.length - 1} onClick={() => patch((d) => ({ ...d, outbounds: move(d.outbounds, i, 1) }))} aria-label={t('xc.down')} title={t('xc.down')}>
                    <ArrowDown size={16} />
                  </button>
                  {!reserved && (
                    <button type="button" className="btn btn--sm btn--icon btn--ghost" onClick={() => toggle(i)} aria-label={off ? t('xc.turnOn') : t('xc.turnOff')} title={off ? t('xc.turnOn') : t('xc.turnOff')}>
                      <Power size={16} />
                    </button>
                  )}
                  <button type="button" className="btn btn--sm btn--icon btn--ghost" onClick={() => setEdit({ index: i, form: outToForm(o) })} aria-label={`${t('common.edit')} ${tag}`} title={t('common.edit')}>
                    <Pencil size={16} />
                  </button>
                  {!reserved && (
                    <button type="button" className="btn btn--sm btn--icon btn--ghost" onClick={() => remove(i)} aria-label={`${t('common.delete')} ${tag}`} title={t('common.delete')}>
                      <Trash2 size={16} />
                    </button>
                  )}
                </span>
              </li>
            );
          })}
        </ol>
      </Section>
      <OutboundSheet
        edit={edit}
        otherTags={edit ? outs.filter((_, j) => j !== edit.index).map(tagOf) : []}
        reserved={edit && edit.index >= 0 ? RESERVED.includes(tagOf(outs[edit.index])) : false}
        onClose={() => setEdit(null)}
        onSave={save}
      />
      <ImportSheet
        open={importing}
        onClose={() => setImporting(false)}
        taken={outs.map(tagOf)}
        onParsed={(o) => {
          setImporting(false);
          setEdit({ index: -1, form: outToForm(o) });
        }}
      />
    </>
  );
}

const omit = (o: Obj, k: string) => {
  const c = { ...o };
  delete c[k];
  return c;
};

function OutboundSheet({ edit, otherTags, reserved, onClose, onSave }: { edit: { index: number; form: OutForm } | null; otherTags: string[]; reserved: boolean; onClose: () => void; onSave: (f: OutForm, index: number) => void }) {
  const { t } = useI18n();
  const [f, setF] = useState<OutForm | null>(null);
  const [key, setKey] = useState<unknown>(null);
  if (edit !== key) {
    setKey(edit);
    setF(edit?.form ?? null);
  }
  const errs = useMemo(() => (f ? outErrors(f, otherTags) : {}), [f, otherTags]);
  const [tried, setTried] = useState(false);
  if (!f) return <Sheet open={false} onClose={onClose} title=""><div /></Sheet>;
  const up = <K extends keyof OutForm>(k: K, v: OutForm[K]) => setF((d) => (d ? { ...d, [k]: v } : d));
  const err = (k: keyof OutForm) => (tried && errs[k] ? t(errs[k] as MessageKey) : undefined);
  const remote = f.protocol !== 'freedom' && f.protocol !== 'blackhole' && f.protocol !== 'wireguard';
  const submit = () => {
    setTried(true);
    if (Object.keys(errs).length) return;
    setTried(false);
    onSave(f, edit!.index);
  };
  const protoOpts = (reserved ? [f.protocol] : OUT_PROTOCOLS).map((p) => ({ value: p, label: PROTO_LABEL[p] }));
  return (
    <Sheet
      open={!!edit}
      onClose={onClose}
      title={edit && edit.index >= 0 ? t('xc.out.edit') : t('xc.out.new')}
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
        <div className={s.row2}>
          <Field label={t('xc.out.tag')} hint={t('xc.out.tagHint')} error={err('tag')}>
            <input className="input ltr mono" value={f.tag} disabled={reserved} onChange={(e) => up('tag', e.target.value.replace(/\s/g, '-'))} aria-invalid={!!err('tag')} />
          </Field>
          <Field label={t('xc.out.protocol')}>
            <Select<OutProtocol> value={f.protocol} disabled={reserved} onChange={(v) => setF((d) => (d ? { ...emptyOut(v), tag: d.tag, address: d.address, port: d.port } : d))} options={protoOpts} ariaLabel={t('xc.out.protocol')} />
          </Field>
        </div>
        {remote && (
          <div className={x.addrRow}>
            <Field label={t('xc.out.address')} error={err('address')}>
              <input className="input ltr" value={f.address} onChange={(e) => up('address', e.target.value.trim())} aria-invalid={!!err('address')} />
            </Field>
            <Field label={t('xc.out.port')} error={err('port')}>
              <input className="input ltr num" inputMode="numeric" value={f.port} onChange={(e) => up('port', e.target.value.replace(/\D/g, ''))} aria-invalid={!!err('port')} />
            </Field>
          </div>
        )}
        {(f.protocol === 'vless' || f.protocol === 'vmess') && (
          <Field label="UUID" error={err('uuid')}>
            <input className="input ltr mono" value={f.uuid} onChange={(e) => up('uuid', e.target.value.trim())} aria-invalid={!!err('uuid')} />
          </Field>
        )}
        {f.protocol === 'vless' && (
          <Field label={t('xc.out.flow')}>
            <Select<string> value={f.flow} onChange={(v) => up('flow', v)} ariaLabel={t('xc.out.flow')} options={[{ value: '', label: t('common.none') }, { value: 'xtls-rprx-vision', label: <span className="ltr">xtls-rprx-vision</span>, text: 'xtls-rprx-vision' }]} />
          </Field>
        )}
        {f.protocol === 'shadowsocks' && (
          <Field label={t('xc.out.method')}>
            <Select<string> value={f.method} onChange={(v) => up('method', v)} ariaLabel={t('xc.out.method')} options={SS_METHODS.map((m) => ({ value: m, label: <span className="ltr">{m}</span>, text: m }))} />
          </Field>
        )}
        {(f.protocol === 'socks' || f.protocol === 'http') && (
          <Field label={t('xc.out.user')} optional>
            <input className="input ltr" value={f.user} autoComplete="off" onChange={(e) => up('user', e.target.value)} />
          </Field>
        )}
        {(f.protocol === 'trojan' || f.protocol === 'shadowsocks' || ((f.protocol === 'socks' || f.protocol === 'http') && f.user)) && (
          <Field label={t('xc.out.password')} error={err('password')}>
            <PasswordInput value={f.password} autoComplete="new-password" onChange={(e) => up('password', e.target.value)} />
          </Field>
        )}
        {f.protocol === 'freedom' && (
          <Field label={t('xc.out.freedomDS')} hint={t('xc.out.freedomDSHint')}>
            <Select<string> value={f.domainStrategy} onChange={(v) => up('domainStrategy', v)} ariaLabel={t('xc.out.freedomDS')} options={['AsIs', 'UseIP', 'UseIPv4', 'UseIPv6', 'UseIPv4v6', 'UseIPv6v4'].map((v) => ({ value: v, label: <span className="ltr">{v}</span>, text: v }))} />
          </Field>
        )}
        {f.protocol === 'blackhole' && <p className={x.note}>{t('xc.out.blackholeHint')}</p>}
        {f.protocol === 'wireguard' && (
          <>
            <Field label={t('xc.out.wgSecret')} error={err('secretKey')}>
              <PasswordInput value={f.secretKey} autoComplete="off" onChange={(e) => up('secretKey', e.target.value.trim())} />
            </Field>
            <div className={s.row2}>
              <Field label={t('xc.out.wgAddress')} hint={t('xc.out.wgAddressHint')}>
                <input className="input ltr mono" value={f.wgAddress} onChange={(e) => up('wgAddress', e.target.value)} />
              </Field>
              <Field label="MTU">
                <input className="input ltr num" inputMode="numeric" value={f.mtu} onChange={(e) => up('mtu', e.target.value.replace(/\D/g, ''))} />
              </Field>
            </div>
            <Field label={t('xc.out.wgPeer')} error={err('peerPublicKey')}>
              <input className="input ltr mono" value={f.peerPublicKey} onChange={(e) => up('peerPublicKey', e.target.value.trim())} aria-invalid={!!err('peerPublicKey')} />
            </Field>
            <div className={s.row2}>
              <Field label={t('xc.out.wgEndpoint')} error={err('endpoint')}>
                <input className="input ltr" value={f.endpoint} placeholder="engage.example.com:2408" onChange={(e) => up('endpoint', e.target.value.trim())} aria-invalid={!!err('endpoint')} />
              </Field>
              <Field label={t('xc.out.wgReserved')} error={err('reserved')} optional>
                <input className="input ltr num" value={f.reserved} placeholder="0,0,0" onChange={(e) => up('reserved', e.target.value)} />
              </Field>
            </div>
            <Field label={t('xc.out.wgPsk')} optional>
              <input className="input ltr mono" value={f.preSharedKey} onChange={(e) => up('preSharedKey', e.target.value.trim())} />
            </Field>
          </>
        )}
        {hasStream(f.protocol) && (
          <>
            <h3 className={s.subhead}>{t('xc.out.transport')}</h3>
            <div className={s.row2}>
              <Field label={t('xc.out.network')}>
                <Select<OutForm['network']> value={f.network} onChange={(v) => up('network', v)} ariaLabel={t('xc.out.network')} options={NETWORKS.map((v) => ({ value: v, label: <span className="ltr">{v === 'raw' ? 'TCP (raw)' : v}</span>, text: v }))} />
              </Field>
              <Field label={t('xc.out.security')}>
                <Select<OutForm['security']>
                  value={f.security}
                  onChange={(v) => up('security', v)}
                  ariaLabel={t('xc.out.security')}
                  options={(['none', 'tls', ...(f.protocol === 'vmess' ? [] : ['reality'])] as OutForm['security'][]).map((v) => ({ value: v, label: v === 'none' ? t('common.none') : v === 'tls' ? 'TLS' : 'Reality' }))}
                />
              </Field>
            </div>
            {(f.network === 'ws' || f.network === 'httpupgrade' || f.network === 'xhttp') && (
              <div className={s.row2}>
                <Field label={t('xc.out.path')}>
                  <input className="input ltr mono" value={f.path} onChange={(e) => up('path', e.target.value)} />
                </Field>
                <Field label={t('xc.out.host')} optional>
                  <input className="input ltr" value={f.host} onChange={(e) => up('host', e.target.value.trim())} />
                </Field>
              </div>
            )}
            {f.network === 'grpc' && (
              <div className={s.row2}>
                <Field label={t('xc.out.service')}>
                  <input className="input ltr mono" value={f.serviceName} onChange={(e) => up('serviceName', e.target.value.trim())} />
                </Field>
                <div className={`${s.switchCard} ${x.inlineSwitch}`}>
                  <span>
                    <strong>{t('xc.out.multi')}</strong>
                  </span>
                  <Switch checked={f.mode === 'multi'} onChange={(v) => up('mode', v ? 'multi' : '')} label={t('xc.out.multi')} />
                </div>
              </div>
            )}
            {f.security !== 'none' && (
              <>
                <div className={s.row2}>
                  <Field label="SNI" optional={f.security === 'tls'}>
                    <input className="input ltr" value={f.sni} onChange={(e) => up('sni', e.target.value.trim())} />
                  </Field>
                  <Field label={t('xc.out.fp')}>
                    <Select<string> value={f.fingerprint} onChange={(v) => up('fingerprint', v)} ariaLabel={t('xc.out.fp')} options={[...(f.security === 'tls' ? [{ value: '', label: t('common.none') }] : []), ...FINGERPRINTS.map((v) => ({ value: v, label: <span className="ltr">{v}</span>, text: v }))]} />
                  </Field>
                </div>
                {f.security === 'reality' ? (
                  <>
                    <Field label={t('xc.out.pbk')} error={err('publicKey')}>
                      <input className="input ltr mono" value={f.publicKey} onChange={(e) => up('publicKey', e.target.value.trim())} aria-invalid={!!err('publicKey')} />
                    </Field>
                    <div className={s.row2}>
                      <Field label="Short ID" optional>
                        <input className="input ltr mono" value={f.shortId} onChange={(e) => up('shortId', e.target.value.trim())} />
                      </Field>
                      <Field label="SpiderX" optional>
                        <input className="input ltr mono" value={f.spiderX} onChange={(e) => up('spiderX', e.target.value.trim())} />
                      </Field>
                    </div>
                  </>
                ) : (
                  <>
                    <Field label="ALPN" optional>
                      <input className="input ltr mono" value={f.alpn} placeholder="h2,http/1.1" onChange={(e) => up('alpn', e.target.value)} />
                    </Field>
                    <div className={s.switchCard}>
                      <span>
                        <strong>{t('xc.out.insecure')}</strong>
                        <small>{t('xc.out.insecureHint')}</small>
                      </span>
                      <Switch checked={f.insecure} onChange={(v) => up('insecure', v)} label={t('xc.out.insecure')} />
                    </div>
                  </>
                )}
              </>
            )}
          </>
        )}
      </div>
    </Sheet>
  );
}

function ImportSheet({ open, onClose, onParsed, taken }: { open: boolean; onClose: () => void; onParsed: (o: Obj) => void; taken: string[] }) {
  const { t } = useI18n();
  const parse = useParseLink();
  const [link, setLink] = useState('');
  const submit = () =>
    parse.mutate(
      { link },
      {
        onSuccess: (r) => {
          let tag = tagOf(r.outbound);
          for (let i = 2; taken.includes(tag); i++) tag = `${tagOf(r.outbound)}-${i}`;
          setLink('');
          onParsed({ ...r.outbound, tag });
        },
        onError: (e) => toast.error(t('xc.out.importFail'), (e as Error).message),
      },
    );
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={t('xc.out.import')}
      size="auto"
      footer={
        <>
          <button type="button" className="btn btn--ghost" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="button" className="btn btn--primary" disabled={!link.trim() || parse.isPending} onClick={submit}>
            {parse.isPending ? <Loader2 size={16} className="spin" /> : <Link2 size={16} />}
            {t('xc.out.read')}
          </button>
        </>
      }
    >
      <div className={s.form}>
        <Field label={t('xc.out.link')} hint={t('xc.out.linkHint')}>
          <textarea className="input ltr mono" rows={4} value={link} spellCheck={false} placeholder="vless://… vmess://… trojan://… ss://… socks://… wireguard://…" onChange={(e) => setLink(e.target.value)} />
        </Field>
      </div>
    </Sheet>
  );
}
