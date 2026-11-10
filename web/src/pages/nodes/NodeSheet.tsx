import { Check, KeyRound, Loader2, ShieldCheck, Terminal, Trash2, TriangleAlert } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useDeleteNode, useNodeInstall, useSaveNode } from '@/api/hooks';
import type { Inbound, NodeInstall, VNode } from '@/api/types';
import { Link } from 'react-router-dom';
import { CopyButton, Field, Switch } from '@/components/misc';
import { Sheet } from '@/components/Overlay';
import { useI18n } from '@/i18n';
import { confirmAction, toast } from '@/store/feedback';
import u from '../users/Users.module.css';
import s from './Nodes.module.css';

const HOST_RE = /^([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/;
const IPV4_RE = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;
const IPV6_RE = /^[0-9a-fA-F:]+$/;
export const okNodeAddr = (v: string) => {
  const a = v.trim().replace(/^\[|\]$/g, '');
  return IPV4_RE.test(a) || (a.includes(':') && IPV6_RE.test(a)) || (HOST_RE.test(a) && !/^\d+(\.\d+)*$/.test(a));
};
const DEFAULT_PORT = 62050;

interface Props {
  open: boolean;
  node: VNode | null;
  onClose: () => void;
  inbounds: Inbound[];
  onCreated: (id: number) => void;
}

export function NodeSheet({ open, node, onClose, inbounds, onCreated }: Props) {
  const { t, n } = useI18n();
  const save = useSaveNode();
  const del = useDeleteNode();
  const reinstall = useNodeInstall();
  const editing = !!node;
  const [name, setName] = useState('');
  const [addr, setAddr] = useState('');
  const [port, setPort] = useState(DEFAULT_PORT);
  const [enabled, setEnabled] = useState(true);
  const [touched, setTouched] = useState(false);
  const [serverErr, setServerErr] = useState<string | null>(null);
  const [install, setInstall] = useState<NodeInstall | null>(null);

  useEffect(() => {
    if (!open) {
      setInstall(null);
      return;
    }
    setTouched(false);
    setServerErr(null);
    setName(node?.name ?? '');
    setAddr(node?.address ?? '');
    setPort(node?.api_port ?? DEFAULT_PORT);
    setEnabled(node?.enabled ?? true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, node?.id]);

  const onNode = node ? inbounds.filter((i) => node.inbound_ids.includes(i.id)) : [];
  const nameErr = !name.trim() || name.trim().length > 48 ? t('node.nameErr') : null;
  const addrErr = !okNodeAddr(addr) ? t('node.addrErr') : null;
  const portErr = !Number.isInteger(port) || port < 1 || port > 65535 ? t('node.portErr') : null;
  const valid = !nameErr && !addrErr && !portErr;
  const busy = save.isPending;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!valid) return;
    setServerErr(null);
    save.mutate(
      {
        id: node?.id ?? null,
        body: {
          name: name.trim(),
          address: addr.trim().replace(/^\[|\]$/g, ''),
          api_port: port,
          enabled,
        },
      },
      {
        onSuccess: (r) => {
          if (r.install) {
            toast.success(t('node.created', { name: r.node.name }));
            setInstall(r.install);
            onCreated(r.node.id);
          } else {
            toast.success(t('node.saved'));
            onClose();
          }
        },
        onError: (x) => setServerErr((x as Error).message),
      },
    );
  };

  const remove = async () => {
    if (!node) return;
    const ok = await confirmAction({
      title: t('node.deleteTitle', { name: node.name }),
      body: t('node.deleteBody'),
      danger: true,
      confirmLabel: t('common.delete'),
    });
    if (!ok) return;
    del.mutate(node.id, {
      onSuccess: () => toast.success(t('node.deleted')),
      onError: (x) => toast.error(t('common.error'), (x as Error).message),
    });
    onClose();
  };

  const newCommand = async () => {
    if (!node) return;
    const ok = await confirmAction({
      title: t('node.installAgainTitle'),
      body: t('node.installAgainBody'),
      confirmLabel: t('node.installAgain'),
    });
    if (!ok) return;
    reinstall.mutate(node.id, {
      onSuccess: (r) => setInstall(r),
      onError: (x) => toast.error(t('common.error'), (x as Error).message),
    });
  };

  if (install) {
    return (
      <Sheet
        open={open}
        onClose={onClose}
        title={t('node.installTitle')}
        subtitle={node?.name ?? name}
        footer={
          <button type="button" className="btn btn--primary" onClick={onClose}>
            <Check size={17} />
            {t('node.done')}
          </button>
        }
      >
        <div className={u.form}>
          <NoPanelWarning />
          <p className={s.sheetIntro}>{t('node.installIntro')}</p>
          <div className={s.cmd}>
            <code className="ltr" dir="ltr">
              {install.command}
            </code>
            <CopyButton text={install.command} />
          </div>
          <ol className={s.steps}>
            <li>
              <Terminal size={16} aria-hidden="true" />
              <span>{t('node.installStep1')}</span>
            </li>
            <li>
              <ShieldCheck size={16} aria-hidden="true" />
              <span>
                {t('node.installStep2', {
                  port: n(install.port, { useGrouping: false }),
                })}
              </span>
            </li>
            <li className={s.warnStep}>
              <KeyRound size={16} aria-hidden="true" />
              <span>{t('node.installStep3')}</span>
            </li>
          </ol>
        </div>
      </Sheet>
    );
  }

  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={editing ? t('node.edit') : t('node.new')}
      subtitle={editing ? node?.name : undefined}
      footer={
        <>
          {editing && (
            <button type="button" className="btn btn--danger-ghost" onClick={remove} style={{ marginInlineEnd: 'auto' }}>
              <Trash2 size={16} />
              {t('common.delete')}
            </button>
          )}
          <button type="button" className="btn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="submit" form="node-form" className="btn btn--primary" disabled={busy || (touched && !valid)}>
            {busy ? <Loader2 size={17} className="spin" /> : <Check size={17} />}
            {busy ? t('common.saving') : editing ? t('common.save') : t('common.create')}
          </button>
        </>
      }
    >
      <form id="node-form" className={u.form} onSubmit={submit} noValidate>
        {!editing && <NoPanelWarning />}
        {serverErr && (
          <div className={u.formErr} role="alert">
            {serverErr}
          </div>
        )}
        {node?.state.error && node.state.status !== 'disabled' && (
          <div className={s.stateErr} role="status">
            <TriangleAlert size={16} aria-hidden="true" />
            <span>{node.state.error}</span>
          </div>
        )}
        {!!node?.state.skipped?.length && (
          <div className={s.stateErr} role="status">
            <TriangleAlert size={16} aria-hidden="true" />
            <span>
              {t('node.skipped')} {node.state.skipped.join('، ')}
            </span>
          </div>
        )}
        <Field label={t('node.name')} error={touched ? (nameErr ?? undefined) : undefined}>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} maxLength={48} data-autofocus aria-invalid={touched && !!nameErr} />
        </Field>
        <Field label={t('node.address')} hint={t('node.addressHint')} error={touched ? (addrErr ?? undefined) : undefined}>
          <input
            className="input ltr"
            dir="ltr"
            value={addr}
            onChange={(e) => setAddr(e.target.value)}
            placeholder="node1.example.com"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            aria-invalid={touched && !!addrErr}
          />
        </Field>
        <Field label={t('node.port')} hint={t('node.portHint')} error={touched ? (portErr ?? undefined) : undefined}>
          <input
            className="input ltr"
            dir="ltr"
            inputMode="numeric"
            value={Number.isNaN(port) ? '' : String(port)}
            onChange={(e) => setPort(e.target.value === '' ? NaN : Number(e.target.value.replace(/\D/g, '')))}
            aria-invalid={touched && !!portErr}
          />
        </Field>
        <div className={u.switchRow}>
          <span>
            <strong>{t('node.enabled')}</strong>
            <small>{t('node.enabledHint')}</small>
          </span>
          <Switch checked={enabled} onChange={setEnabled} label={t('node.enabled')} />
        </div>
        {editing && (
          <div className={u.fs}>
            <span className="field__label">{t('node.inbounds')}</span>
            {onNode.length ? (
              <span className={s.roChips}>
                {onNode.map((ib) => (
                  <span key={ib.id} className={s.chip}>
                    {ib.remark}
                  </span>
                ))}
              </span>
            ) : (
              <span className="field__hint">{t('node.inboundsN', { n: n(0) })}</span>
            )}
            <span className="field__hint">
              {t('node.inboundsHint')}{' '}
              <Link to="/inbounds" onClick={onClose}>
                {t('node.toInbounds')}
              </Link>
            </span>
          </div>
        )}
        {editing && (
          <button type="button" className={`btn ${s.reinstall}`} onClick={newCommand} disabled={reinstall.isPending}>
            {reinstall.isPending ? <Loader2 size={16} className="spin" /> : <Terminal size={16} />}
            {t('node.installAgain')}
          </button>
        )}
      </form>
    </Sheet>
  );
}

/** The strong warning against installing the panel on a node server. */
export function NoPanelWarning() {
  const { t } = useI18n();
  return (
    <div className={s.noPanel} role="note">
      <TriangleAlert size={20} aria-hidden="true" />
      <span>
        <strong>{t('node.noPanelTitle')}</strong>
        <span>{t('node.noPanelBody')}</span>
      </span>
    </div>
  );
}
