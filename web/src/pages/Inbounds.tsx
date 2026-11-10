import { motion } from 'framer-motion';
import { Pencil, Plus, Trash2, Users as UsersIcon } from 'lucide-react';
import { useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useDeleteInbound, useInbounds, useToggleInbound } from '@/api/hooks';
import { EmptyState, ErrorState, PageHeader, Skeleton, Switch } from '@/components/misc';
import { useI18n } from '@/i18n';
import { confirmAction, toast } from '@/store/feedback';
import { ProtoGlyph, protoColor } from './inbounds/ProtoGlyph';
import { engineName, engineOf, hasUsers, isV2, isWG, protoName, transportLabel, transportsOf, withSettings } from '@/lib/protocols';
import { InboundWizard } from './inbounds/Wizard';
import { HostsIcon, HostsSheet } from './inbounds/HostsSheet';
import { DensityToggle } from '@/components/DensityToggle';
import { useUi } from '@/store/ui';
import s from './inbounds/Inbounds.module.css';

export default function InboundsPage() {
  const { t, n } = useI18n();
  const q = useInbounds();
  const toggle = useToggleInbound();
  const del = useDeleteInbound();
  const [params, setParams] = useSearchParams();
  const editId = Number(params.get('edit')) || null;
  const creating = params.get('new') === '1';
  const editing = useMemo(() => (editId ? q.data?.find((i) => i.id === editId) ?? null : null), [editId, q.data]);
  const close = () => setParams({}, { replace: false });
  const hostsId = Number(params.get('hosts')) || null;
  const hostsOf = useMemo(() => (hostsId ? q.data?.find((i) => i.id === hostsId) ?? null : null), [hostsId, q.data]);
  const compact = useUi((x) => x.density) === 'compact';

  const remove = async (id: number, name: string) => {
    const ok = await confirmAction({ title: t('inb.deleteConfirm', { name }), body: t('inb.deleteBody'), danger: true, confirmLabel: t('common.delete') });
    if (!ok) return;
    del.mutate(id, { onSuccess: () => toast.success(t('inb.deleted')), onError: (e) => toast.error(t('common.error'), (e as Error).message) });
  };

  return (
    <div>
      <PageHeader
        eyebrow={q.data ? t('inb.subtitle', { n: q.data.length }) : ' '}
        title={t('inb.title')}
        actions={
          <>
            <DensityToggle className={s.density} />
            <button className="btn btn--primary" onClick={() => setParams({ new: '1' })}>
              <Plus size={18} />
              {t('inb.new')}
            </button>
          </>
        }
      />
      {q.isError && !q.data ? (
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : q.isPending ? (
        <div className={compact ? s.gridCompact : s.grid}>
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} h={compact ? 112 : 230} r={compact ? 22 : 26} />
          ))}
        </div>
      ) : q.data.length === 0 ? (
        <EmptyState
          kind="gates"
          title={t('inb.empty.title')}
          body={t('inb.empty.body')}
          action={
            <button className="btn btn--primary" onClick={() => setParams({ new: '1' })}>
              <Plus size={18} />
              {t('inb.new')}
            </button>
          }
        />
      ) : (
        <div className={compact ? s.gridCompact : s.grid}>
          {q.data.map((ib, i) => {
            const st = withSettings(ib.settings);
            const trl = transportLabel(transportsOf({ ...ib, settings: st }));
            const eng = engineOf(ib.protocol);
            const sub =
              ib.protocol === 'tunnel'
                ? `→ ${st.target_address}:${st.target_port}`
                : ib.protocol === 'tun'
                  ? st.gateway.join(', ')
                  : isWG(ib.protocol)
                    ? st.address
                    : ib.protocol === 'mtproto'
                      ? st.faketls_domain
                      : '';
            const badges = (
              <>
                {isV2(ib.protocol) ? (
                  <>
                    <span className="chip mono">{ib.network}</span>
                    <span className={`chip ${ib.security === 'reality' ? 'chip--violet' : ib.security === 'tls' ? 'chip--active' : 'chip--disabled'}`}>
                      {ib.security === 'none' ? t('common.none') : ib.security === 'tls' ? 'TLS' : 'Reality'}
                    </span>
                    {ib.flow && <span className="chip mono">vision</span>}
                  </>
                ) : (
                  <>
                    {trl && <span className="chip mono">{trl}</span>}
                    {ib.security === 'tls' && <span className="chip chip--active">TLS</span>}
                    {eng !== 'xray' && <span className="chip chip--violet">{engineName(eng)}</span>}
                  </>
                )}
              </>
            );
            const usersLine = hasUsers(ib.protocol) ? (
              <>
                <UsersIcon size={15} /> {t('inb.users', { n: ib.user_count })}
              </>
            ) : (
              <span className={s.muted}>{ib.protocol === 'tun' ? t('inb.interface') : t('inb.forwardTo')}</span>
            );
            const actions = (
              <div className={s.cardActions}>
                {hasUsers(ib.protocol) && (
                  <button
                    className={`btn btn--sm btn--icon btn--ghost ${ib.host_count ? s.hostsOn : ''}`}
                    onClick={() => setParams({ hosts: String(ib.id) })}
                    aria-label={`${t('hosts.title')} ${ib.remark}`}
                    title={ib.host_count ? t('hosts.count', { n: n(ib.host_count) }) : t('hosts.title')}
                  >
                    <HostsIcon size={16} />
                    {!!ib.host_count && <span className={s.hostsBadge}>{n(ib.host_count)}</span>}
                  </button>
                )}
                <button className="btn btn--sm btn--icon btn--ghost" onClick={() => setParams({ edit: String(ib.id) })} aria-label={`${t('common.edit')} ${ib.remark}`}>
                  <Pencil size={16} />
                </button>
                <button className="btn btn--sm btn--icon btn--ghost" onClick={() => remove(ib.id, ib.remark)} aria-label={`${t('common.delete')} ${ib.remark}`}>
                  <Trash2 size={16} />
                </button>
              </div>
            );
            const port =
              ib.protocol === 'tun' ? (
                <span className={`${compact ? s.miniPort : s.port} display ltr`} title={ib.settings?.tun_name}>
                  {ib.settings?.tun_name || 'tun'}
                </span>
              ) : (
                <span className={`${compact ? s.miniPort : s.port} display num ltr`}>:{ib.port}</span>
              );
            if (compact) {
              return (
                <motion.article
                  key={ib.id}
                  className={`${s.mini} ${ib.enabled ? '' : s.cardOff}`}
                  style={{ '--pc': protoColor(ib.protocol) } as React.CSSProperties}
                  initial={{ y: 10 }}
                  animate={{ y: 0 }}
                  transition={{ delay: Math.min(i, 10) * 0.03, duration: 0.35, ease: [0.22, 1, 0.36, 1] }}
                >
                  <div className={s.miniTop}>
                    <ProtoGlyph protocol={ib.protocol} size={40} />
                    <div className={s.cardHead}>
                      <h2 className={`${s.miniTitle} display`} title={ib.remark} dir="auto">
                        {ib.remark}
                      </h2>
                      <span className={s.miniSub}>
                        <span className={s.protoName}>{protoName(ib.protocol)}</span>
                        {sub && (
                          <span className="ltr" title={sub}>
                            {sub}
                          </span>
                        )}
                      </span>
                    </div>
                    <div className={s.miniPortBox}>
                      {port}
                      <div className={`${s.signal} ${s.miniSignal}`} aria-hidden="true">
                        {Array.from({ length: 5 }, (_, k) => (
                          <i key={k} style={{ height: 3 + k * 2.5, opacity: ib.enabled ? (k < 2 + (ib.user_count % 4) ? 1 : 0.25) : 0.15 }} />
                        ))}
                      </div>
                    </div>
                    <Switch checked={ib.enabled} onChange={(v) => toggle.mutate({ id: ib.id, enabled: v })} label={t('inb.enabledToggle', { name: ib.remark })} />
                  </div>
                  <div className={s.miniFoot}>
                    <div className={s.miniBadges}>{badges}</div>
                    <span className={s.users}>{usersLine}</span>
                    {actions}
                  </div>
                </motion.article>
              );
            }
            return (
            <motion.article
              key={ib.id}
              className={`${s.card} ${ib.enabled ? '' : s.cardOff}`}
              style={{ '--pc': protoColor(ib.protocol) } as React.CSSProperties}
              initial={{ y: 16 }}
              animate={{ y: 0 }}
              transition={{ delay: i * 0.04, duration: 0.45, ease: [0.22, 1, 0.36, 1] }}
            >
              <div className={s.cardTop}>
                <ProtoGlyph protocol={ib.protocol} size={52} />
                <div className={s.cardHead}>
                  <h2 className={`${s.cardTitle} display`} title={ib.remark} dir="auto">
                    {ib.remark}
                  </h2>
                  <span className={s.protoName}>{protoName(ib.protocol)}</span>
                </div>
                <Switch checked={ib.enabled} onChange={(v) => toggle.mutate({ id: ib.id, enabled: v })} label={t('inb.enabledToggle', { name: ib.remark })} />
              </div>
              <div className={s.portLine}>
                {port}
                <div className={s.signal} aria-hidden="true">
                  {Array.from({ length: 5 }, (_, k) => (
                    <i key={k} style={{ height: 6 + k * 5, opacity: ib.enabled ? (k < 2 + (ib.user_count % 4) ? 1 : 0.25) : 0.15 }} />
                  ))}
                </div>
              </div>
              {sub && (
                <span className={`${s.subLine} ltr`} title={sub}>
                  {sub}
                </span>
              )}
              <div className={s.badges}>{badges}</div>
              <div className={s.cardFoot}>
                <span className={s.users}>{usersLine}</span>
                {actions}
              </div>
              <span className="sr-only">{n(ib.user_count)}</span>
            </motion.article>
            );
          })}
          <button className={`${s.addCard} ${compact ? s.addCardMini : ''}`} onClick={() => setParams({ new: '1' })}>
            <span className={s.addIcon}>
              <Plus size={compact ? 20 : 26} />
            </span>
            {t('inb.new')}
          </button>
        </div>
      )}
      <InboundWizard open={creating || !!editing} onClose={close} editing={editing} />
      <HostsSheet inbound={hostsOf} onClose={close} />
    </div>
  );
}
