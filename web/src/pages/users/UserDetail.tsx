import { motion } from 'framer-motion';
import { ArrowDown, ArrowUp, Download, KeyRound, MessageSquareText, Pencil, Power, PowerOff, RotateCcw, StickyNote, Trash2 } from 'lucide-react';
import { linkify } from '@/sub/format';
import { useEffect, useState } from 'react';
import { useDeleteUser, useResetTraffic, useRevokeSub, useUpdateUser, useUser, useUserLinks } from '@/api/hooks';
import type { Inbound, User, UserLink } from '@/api/types';
import { protoName } from '@/lib/protocols';
import { Sheet } from '@/components/Overlay';
import { CopyButton, EmptyState, ErrorState, QR, Skeleton, StatusChip } from '@/components/misc';
import { QuotaRing } from '@/components/viz';
import { useI18n } from '@/i18n';
import { expiryInfo, usageRatio, usageTone } from '@/lib/user';
import { can } from '@/lib/perms';
import { confirmAction, toast } from '@/store/feedback';
import { useSession } from '@/store/session';
import { ConnTab } from './ConnTab';
import { limitBadges } from './LimitBadges';
import { ExpiryText, LastSeen } from './PassCard';
import s from './Users.module.css';

type Tab = 'sub' | 'links' | 'conns' | 'details';

function protoOf(uri: string) {
  const m = uri.match(/^([a-z0-9]+):\/\//i);
  return m ? (m[1] === 'ss' ? 'SS' : m[1].toUpperCase()) : '';
}

/**
 * Downloads a WireGuard / AmneziaWG config as NAME.conf. The server endpoint
 * (application/octet-stream + Content-Disposition) is preferred because
 * Android browsers keep its .conf name; the local blob is octet-stream too,
 * never text/plain (which Android saves as .conf.txt).
 */
function downloadConf(name: string, text: string, url?: string) {
  if (url) {
    const a = document.createElement('a');
    a.href = url;
    a.download = name;
    a.rel = 'noopener';
    document.body.appendChild(a);
    a.click();
    a.remove();
    return;
  }
  const blobUrl = URL.createObjectURL(new Blob([text], { type: 'application/octet-stream' }));
  const a = document.createElement('a');
  a.href = blobUrl;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(blobUrl), 2000);
}

function LinkCard({ link: l, protocol }: { link: UserLink; protocol?: string }) {
  const { t } = useI18n();
  const hasConf = !!l.conf;
  const [qrMode, setQrMode] = useState<'link' | 'conf'>(l.uri ? 'link' : 'conf');
  const qrValue = qrMode === 'conf' && l.conf ? l.conf : l.uri;
  const tag = protocol ? protoName(protocol) : protoOf(l.uri);
  const confName = l.conf_name || `${(l.remark || 'veilon').replace(/[^\w.-]+/g, '_').slice(0, 24)}.conf`;
  return (
    <div className={s.linkCard}>
      {qrValue ? (
        <div className={s.linkQr}>
          <QR value={qrValue} size={132} label={t('user.qrFor', { name: l.remark })} />
          {hasConf && l.uri && (
            <div className="seg" role="group" aria-label={t('user.qrLink')}>
              <button type="button" aria-pressed={qrMode === 'link'} onClick={() => setQrMode('link')}>
                {t('user.qrLink')}
              </button>
              <button type="button" aria-pressed={qrMode === 'conf'} onClick={() => setQrMode('conf')}>
                {t('user.qrConf')}
              </button>
            </div>
          )}
        </div>
      ) : null}
      <div className={s.linkInfo}>
        <span className="chip chip--violet">{tag}</span>
        <strong className={s.linkRemark} title={l.remark}>
          <bdi>{l.remark}</bdi>
        </strong>
        {l.uri && <code className={`${s.linkUri} mono ltr`}>{l.uri}</code>}
        {!l.uri && protocol === 'masque' && <p className={s.linkNote}>{t('user.masqueNote')}</p>}
        {!l.uri && protocol === 'amneziawg' && <p className={s.linkNote}>{t('user.awgNoUri')}</p>}
        {!l.uri && protocol === 'shadowtls' && <p className={s.linkNote}>{t('user.stlsNote')}</p>}
        {!l.uri && !l.conf && protocol !== 'masque' && protocol !== 'amneziawg' && protocol !== 'shadowtls' && <p className={s.linkNote}>{t('user.masqueNote')}</p>}
        <div className={s.linkBtns}>
          {l.uri && <CopyButton text={l.uri} />}
          {hasConf && (
            <button type="button" className="btn btn--sm" onClick={() => downloadConf(confName, l.conf!, l.conf_url)} aria-label={t('user.downloadConf', { name: confName })}>
              <Download size={16} />
              <span className="ltr">{confName}</span>
            </button>
          )}
          {hasConf && !l.uri && <CopyButton text={l.conf!} />}
        </div>
      </div>
    </div>
  );
}

export function UserDetail({ id, onClose, onEdit, inbounds }: { id: number | null; onClose: () => void; onEdit: (id: number) => void; inbounds: Map<number, Inbound> }) {
  const { t } = useI18n();
  const q = useUser(id);
  const u = q.data;
  return (
    <Sheet
      open={!!id}
      onClose={onClose}
      width={600}
      title={u ? <span className="ltr">{u.username}</span> : id ? '…' : ''}
      subtitle={
        u && (
          <>
            <StatusChip status={u.status} />
            {u.suspend_reason && <span className="chip chip--limited">{u.suspend_reason === 'window' ? t('badge.suspendWindow') : t('badge.suspendDaily')}</span>}
            {u.online ? (
              <span className="chip chip--active">
                <span className="chip__dot" />
                {t('common.online')}
              </span>
            ) : (
              <span className="chip chip--disabled">{u.last_online_at ? <LastSeen u={u} /> : t('common.offline')}</span>
            )}
          </>
        )
      }
    >
      {q.isError ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : !u ? <DetailSkeleton /> : <DetailBody u={u} onEdit={onEdit} onClose={onClose} inbounds={inbounds} />}
    </Sheet>
  );
}

function DetailSkeleton() {
  return (
    <div style={{ display: 'grid', gap: 16 }} aria-busy="true">
      <Skeleton h={150} r={24} />
      <Skeleton h={48} r={16} />
      <Skeleton h={260} r={24} />
    </div>
  );
}

function DetailBody({ u, onEdit, onClose, inbounds }: { u: User; onEdit: (id: number) => void; onClose: () => void; inbounds: Map<number, Inbound> }) {
  const { t, n, bytes, date, dateTime } = useI18n();
  const [tab, setTab] = useState<Tab>('sub');
  const links = useUserLinks(u.id);
  const reset = useResetTraffic();
  const revoke = useRevokeSub();
  const update = useUpdateUser();
  const del = useDeleteUser();
  const ratio = usageRatio(u);
  const tone = usageTone(u);
  const exp = expiryInfo(u);
  const lims = limitBadges(u, { t, n, bytes });
  // Same rules as the users page: actions the signed-in admin may not use
  // are hidden (the server refuses them anyway).
  const me = useSession((x) => x.admin);
  const mayEdit = can(me, 'users_edit');
  const mayDelete = can(me, 'users_delete');
  useEffect(() => {
    setTab('sub');
  }, [u.id]);

  const err = (e: unknown) => toast.error(t('common.error'), (e as Error).message);

  const doReset = async () => {
    if (!(await confirmAction({ title: t('user.resetConfirm', { name: u.username }), confirmLabel: t('user.resetTraffic') }))) return;
    reset.mutate(u.id, { onSuccess: () => toast.success(t('user.resetDone')), onError: err });
  };
  const doRevoke = async () => {
    if (!(await confirmAction({ title: t('user.revokeConfirm', { name: u.username }), body: t('user.revokeBody'), danger: true, confirmLabel: t('user.revoke') }))) return;
    revoke.mutate(u.id, { onSuccess: () => toast.success(t('user.revokeDone')), onError: err });
  };
  const doToggle = () => {
    const next = u.status === 'disabled' ? 'active' : 'disabled';
    update.mutate({ id: u.id, patch: { status: next } }, { onSuccess: () => toast.success(t('user.saved')), onError: err });
  };
  const doDelete = async () => {
    if (!(await confirmAction({ title: t('user.deleteConfirm', { name: u.username }), body: t('user.deleteBody'), danger: true, typeToConfirm: u.username, confirmLabel: t('common.delete') })))
      return;
    onClose();
    del.mutate(u.id, { onSuccess: () => toast.success(t('user.deleted')), onError: err });
  };

  return (
    <div className={s.detail}>
      {/* quota hero */}
      <div className={`${s.dHero} ${s['tone_' + tone]}`}>
        <QuotaRing ratio={ratio} size={124} stroke={11} tone={tone} label={`${t('users.used')} ${bytes(u.used_traffic)}`}>
          <div className={s.dRingInner}>
            <strong className="display num">{ratio === null ? '∞' : n(ratio, { style: 'percent', maximumFractionDigits: 0 })}</strong>
            <span>{t('users.used')}</span>
          </div>
        </QuotaRing>
        <div className={s.dStats}>
          <div>
            <span className={s.statLabel}>{t('users.used')}</span>
            <strong className={`num display ${s.dBig}`}>{bytes(u.used_traffic)}</strong>
            <span className={s.statSub}>
              {t('common.of')} {u.data_limit ? bytes(u.data_limit, 0) : t('common.unlimited')}
              {u.data_limit ? ` · ${t('user.remaining')} ${bytes(Math.max(0, u.data_limit - u.used_traffic))}` : ''}
            </span>
          </div>
          <div className={s.dUpDown}>
            <span>
              <ArrowDown size={14} /> <span className="num">{bytes(u.down)}</span>
            </span>
            <span>
              <ArrowUp size={14} /> <span className="num">{bytes(u.up)}</span>
            </span>
          </div>
          <div className={s.dExp}>
            <span className={s.statLabel}>{t('users.expires')}</span>
            <strong className={s['fg_' + exp.tone]}>
              <ExpiryText u={u} />
            </strong>
            {u.expire_at && <span className={s.statSub}>{date(u.expire_at, { year: 'numeric', month: 'long', day: 'numeric' })}</span>}
          </div>
        </div>
      </div>

      {/* actions */}
      {mayEdit && (
        <div className={s.dActions}>
          <button className="btn btn--primary btn--sm" onClick={() => onEdit(u.id)}>
            <Pencil size={15} />
            {t('user.edit')}
          </button>
          <button className="btn btn--sm" onClick={doReset} disabled={reset.isPending}>
            <RotateCcw size={15} />
            {t('user.resetTraffic')}
          </button>
          <button className="btn btn--sm" onClick={doRevoke} disabled={revoke.isPending}>
            <KeyRound size={15} />
            {t('user.revoke')}
          </button>
          <button className="btn btn--sm" onClick={doToggle} disabled={update.isPending}>
            {u.status === 'disabled' ? <Power size={15} /> : <PowerOff size={15} />}
            {u.status === 'disabled' ? t('user.enable') : t('user.disable')}
          </button>
        </div>
      )}

      {/* private note (admins only) and description for the user (subscription page) */}
      {(u.note.trim() || (u.description ?? '').trim()) && (
        <div className={s.dTexts}>
          {u.note.trim() && (
            <figure className={s.dText}>
              <figcaption className={s.dTextLabel}>
                <StickyNote size={14} aria-hidden="true" />
                {t('user.note')}
              </figcaption>
              <p className={s.dTextBody} dir="auto">
                {u.note}
              </p>
            </figure>
          )}
          {(u.description ?? '').trim() && (
            <figure className={s.dText}>
              <figcaption className={s.dTextLabel}>
                <MessageSquareText size={14} aria-hidden="true" />
                {t('user.description')}
              </figcaption>
              <p className={s.dTextBody} dir="auto">
                {linkify(u.description.trim()).map((p, i) =>
                  p.href ? (
                    <a key={i} href={p.href} target="_blank" rel="noopener noreferrer nofollow" dir="ltr" className={s.dTextLink}>
                      {p.text}
                    </a>
                  ) : (
                    <span key={i}>{p.text}</span>
                  ),
                )}
              </p>
            </figure>
          )}
        </div>
      )}

      {/* tabs */}
      <div className={`seg ${s.dTabs}`} role="tablist" aria-label={t('user.details')}>
        {(
          [
            ['sub', t('user.subscription'), null],
            ['links', t('user.links'), links.data ? links.data.links.length : null],
            ['conns', t('user.conns'), null],
            ['details', t('user.details'), null],
          ] as Array<[Tab, string, number | null]>
        ).map(([k, label, count]) => (
          <button key={k} role="tab" aria-selected={tab === k} aria-controls={`tab-${k}`} id={`tabbtn-${k}`} onClick={() => setTab(k)}>
            {label}
            {count !== null && <span className={s.tabCount}>{n(count)}</span>}
          </button>
        ))}
      </div>

      <>
        <motion.div
          key={tab}
          id={`tab-${tab}`}
          role="tabpanel"
          aria-labelledby={`tabbtn-${tab}`}
          initial={{ y: 8 }}
          animate={{ y: 0 }}
          transition={{ duration: 0.2 }}
        >
          {tab === 'sub' && (
            <div className={s.subPanel}>
              <QR value={u.sub_url} size={200} label={t('user.qrFor', { name: u.username })} />
              <div className={s.subInfo}>
                <span className={s.statLabel}>{t('user.subUrl')}</span>
                <code className={`${s.codeBox} mono ltr`}>{u.sub_url}</code>
                <CopyButton text={u.sub_url} />
                <p className={s.subHint}>{t('user.subHint')}</p>
              </div>
            </div>
          )}
          {tab === 'links' &&
            (links.isPending ? (
              <Skeleton h={200} r={20} />
            ) : links.isError ? (
              <ErrorState error={links.error} onRetry={() => links.refetch()} />
            ) : links.data.links.length === 0 ? (
              <EmptyState kind="gates" title={t('user.noLinks')} />
            ) : (
              <div className={s.linkGrid}>
                {links.data.links.map((l) => (
                  <LinkCard key={l.inbound_id} link={l} protocol={l.protocol ?? inbounds.get(l.inbound_id)?.protocol} />
                ))}
              </div>
            ))}
          {tab === 'conns' && <ConnTab u={u} mayEdit={mayEdit} />}
          {tab === 'details' && (
            <dl className={s.dl}>
              <div>
                <dt>{t('user.uuid')}</dt>
                <dd className={s.ddCopy}>
                  <code className="mono ltr">{u.uuid}</code>
                  <CopyButton text={u.uuid} compact />
                </dd>
              </div>
              <div>
                <dt>{t('user.password')}</dt>
                <dd className={s.ddCopy}>
                  <code className="mono ltr">{u.password}</code>
                  <CopyButton text={u.password} compact />
                </dd>
              </div>
              <div>
                <dt>{t('user.resetStrategy')}</dt>
                <dd>{t(`form.reset.${u.reset_strategy}`)}</dd>
              </div>
              {lims.length > 0 && (
                <div>
                  <dt>{t('user.limits')}</dt>
                  <dd className={s.ddChips}>
                    {lims.map((b) => (
                      <span key={b.key} className={`chip ${b.alert ? 'chip--limited' : ''}`}>
                        {b.Icon && <b.Icon size={13} aria-hidden="true" />}
                        {b.label}
                      </span>
                    ))}
                  </dd>
                </div>
              )}
              <div>
                <dt>{t('user.inbounds')}</dt>
                <dd className={s.ddChips}>
                  {u.inbound_ids.map((id) => (
                    <span key={id} className="chip">
                      {inbounds.get(id)?.remark ?? `#${id}`}
                    </span>
                  ))}
                </dd>
              </div>
              <div>
                <dt>{t('users.lastSeen')}</dt>
                <dd>
                  <LastSeen u={u} label={false} />
                </dd>
              </div>
              <div>
                <dt>{t('user.createdAt')}</dt>
                <dd>{dateTime(u.created_at)}</dd>
              </div>
            </dl>
          )}
        </motion.div>
      </>

      {mayDelete && (
        <div className={s.danger}>
          <div>
            <strong>{t('user.dangerZone')}</strong>
            <p>{t('user.deleteBody')}</p>
          </div>
          <button className="btn btn--danger btn--sm" onClick={doDelete}>
            <Trash2 size={15} />
            {t('user.delete')}
          </button>
        </div>
      )}
    </div>
  );
}
