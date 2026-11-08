import { AnimatePresence, motion } from 'framer-motion';
import { CalendarPlus, Layers, LayoutGrid, List, Loader2, Plus, Power, PowerOff, RotateCcw, Search, Trash2, X } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useBulk, useGroups, useInbounds, useUsersInfinite, type UserFilters } from '@/api/hooks';
import type { BulkAction, Inbound, UserStatus } from '@/api/types';
import { useIsMobile } from '@/components/Overlay';
import { EmptyState, ErrorState, PageHeader, Skeleton } from '@/components/misc';
import { useI18n } from '@/i18n';
import { confirmAction, toast } from '@/store/feedback';
import { useUi } from '@/store/ui';
import { useSession } from '@/store/session';
import { can } from '@/lib/perms';
import { CompactPass, PassCard, UserRow } from './users/PassCard';
import { DensityToggle } from '@/components/DensityToggle';
import { UserDetail } from './users/UserDetail';
import { UserForm } from './users/UserForm';
import { Select } from '@/components/Select';
import s from './users/Users.module.css';

const STATUSES: Array<UserStatus | 'online' | ''> = ['', 'online', 'active', 'on_hold', 'limited', 'expired', 'disabled'];
const SORTS = [
  { v: '-created_at', k: 'users.sort.newest' },
  { v: 'created_at', k: 'users.sort.oldest' },
  { v: '-used_traffic', k: 'users.sort.usage' },
  { v: 'expire_at', k: 'users.sort.expiry' },
  { v: 'username', k: 'users.sort.name' },
] as const;

function useDebounced<T>(v: T, ms: number) {
  const [d, setD] = useState(v);
  useEffect(() => {
    const t = setTimeout(() => setD(v), ms);
    return () => clearTimeout(t);
  }, [v, ms]);
  return d;
}

export default function UsersPage() {
  const { t, n } = useI18n();
  const [params, setParams] = useSearchParams();
  const view = useUi((x) => x.usersView);
  const setView = useUi((x) => x.setUsersView);
  const mobile = useIsMobile();
  const effectiveView = mobile ? 'cards' : view;
  const density = useUi((x) => x.density);
  const compact = density === 'compact';

  const [search, setSearch] = useState('');
  const [status, setStatus] = useState<UserStatus | 'online' | ''>('');
  const [sort, setSort] = useState('-created_at');
  const [group, setGroup] = useState(0);
  const dSearch = useDebounced(search.trim(), 250);
  const filters: UserFilters = useMemo(() => ({ search: dSearch, status, sort, group }), [dSearch, status, sort, group]);
  const me = useSession((x) => x.admin);
  const mayCreate = can(me, 'users_create');
  const mayEdit = can(me, 'users_edit');
  const mayDelete = can(me, 'users_delete');
  const groupsQ = useGroups();
  const groups = useMemo(() => groupsQ.data ?? [], [groupsQ.data]);
  const groupMap = useMemo(() => new Map(groups.map((g) => [g.id, g])), [groups]);

  const q = useUsersInfinite(filters);
  // Cards are keyed per result set: when a filter swaps the list, cards that
  // appear in both lists mount fresh in their new place instead of jumping
  // there (a moved node is a layout shift, a new one is not). Background
  // refreshes keep the same keys, so they update in place.
  const shownKey = useRef('');
  if (!q.isPlaceholderData && q.data) shownKey.current = `${filters.search}|${filters.status}|${filters.sort}|${filters.group}`;
  const listKey = shownKey.current;
  const inboundsQ = useInbounds();
  const inbounds = useMemo(() => new Map<number, Inbound>((inboundsQ.data ?? []).map((i) => [i.id, i])), [inboundsQ.data]);
  const users = useMemo(() => q.data?.pages.flatMap((p) => p.items) ?? [], [q.data]);
  const total = q.data?.pages[0]?.total ?? 0;

  /* selection */
  const [sel, setSel] = useState<Set<number>>(new Set());
  const lastClicked = useRef<number | null>(null);
  const toggle = useCallback(
    (id: number, shift: boolean) => {
      setSel((prev) => {
        const next = new Set(prev);
        if (shift && lastClicked.current !== null) {
          const ids = users.map((u) => u.id);
          const a = ids.indexOf(lastClicked.current),
            b = ids.indexOf(id);
          if (a >= 0 && b >= 0) {
            for (const x of ids.slice(Math.min(a, b), Math.max(a, b) + 1)) next.add(x);
            return next;
          }
        }
        if (next.has(id)) next.delete(id);
        else next.add(id);
        lastClicked.current = id;
        return next;
      });
    },
    [users],
  );
  useEffect(() => {
    setSel(new Set());
  }, [filters]);

  /* sheets via URL */
  const openId = Number(params.get('u')) || null;
  const editId = Number(params.get('edit')) || null;
  const creating = params.get('new') === '1';
  const setParam = useCallback(
    (k: string, v: string | null) => {
      const p = new URLSearchParams(params);
      ['u', 'edit', 'new'].forEach((x) => p.delete(x));
      if (v) p.set(k, v);
      setParams(p, { replace: false });
    },
    [params, setParams],
  );
  const open = useCallback((id: number) => setParam('u', String(id)), [setParam]);

  /* infinite scroll */
  const sentinel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = sentinel.current;
    if (!el) return;
    const io = new IntersectionObserver(
      ([e]) => {
        if (e.isIntersecting && q.hasNextPage && !q.isFetchingNextPage) q.fetchNextPage();
      },
      { rootMargin: '600px' },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [q.hasNextPage, q.isFetchingNextPage, q.fetchNextPage, q]);

  /* bulk */
  const bulk = useBulk();
  const runBulk = async (action: BulkAction) => {
    const ids = Array.from(sel);
    const count = ids.length;
    let days: number | undefined;
    let groupId: number | undefined;
    if (action === 'set_group') {
      const r = await confirmAction({
        title: t('bulk.groupTitle', { n: count }),
        choiceInput: {
          label: t('form.group'),
          initial: groups[0]?.id ?? 0,
          options: [...groups.map((g) => ({ value: g.id, label: g.name, color: g.color })), { value: 0, label: t('form.noGroup') }],
        },
        confirmLabel: t('bulk.group'),
      });
      if (!r) return;
      groupId = r.value ?? 0;
    } else if (action === 'delete') {
      const ok = await confirmAction({ title: t('bulk.deleteTitle', { n: count }), body: t('bulk.deleteBody'), danger: true, confirmLabel: t('bulk.delete') });
      if (!ok) return;
    } else if (action === 'reset_traffic') {
      const ok = await confirmAction({ title: t('bulk.resetTitle', { n: count }), confirmLabel: t('bulk.reset') });
      if (!ok) return;
    } else if (action === 'extend') {
      const r = await confirmAction({ title: t('bulk.extendTitle', { n: count }), numberInput: { label: t('bulk.extendDays'), initial: 30, min: 1 }, confirmLabel: t('bulk.extend') });
      if (!r) return;
      days = r.value;
    }
    bulk.mutate(
      { action, ids, days, group_id: groupId },
      {
        onSuccess: () => {
          toast.success(t('bulk.done', { n: count }));
          setSel(new Set());
        },
        onError: (e) => toast.error(t('common.error'), (e as Error).message),
      },
    );
  };

  const selectAll = () => setSel(new Set(users.map((u) => u.id)));
  const filtered = !!dSearch || !!status || !!group;
  const groupOpts = useMemo(
    () => [
      { value: 0, label: t('nav.groups'), text: t('users.allGroups') },
      ...groups.map((g) => ({
        value: g.id,
        label: (
          <span className={s.groupOpt}>
            <span className={s.groupDot} style={{ background: g.color }} aria-hidden="true" />
            {g.name}
          </span>
        ),
        text: g.name,
        hint: n(g.users),
      })),
      { value: -1, label: t('form.noGroup') },
    ],
    [groups, t, n],
  );

  return (
    <div>
      <PageHeader
        eyebrow={t('users.subtitle', { n: total })}
        title={t('users.title')}
        actions={
          mayCreate && (
            <button className="btn btn--primary" onClick={() => setParam('new', '1')}>
              <Plus size={18} />
              {t('users.new')}
            </button>
          )
        }
      />

      <div className={s.toolbar}>
        <div className={s.searchBox}>
          <Search size={18} aria-hidden="true" />
          <input
            className={s.searchInput}
            placeholder={t('users.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            aria-label={t('common.search')}
            type="search"
          />
          {q.isFetching && q.isPlaceholderData && <Loader2 size={16} className="spin" aria-hidden="true" />}
        </div>
        <div className={s.toolRight}>
          <Select<string> className={s.sort} value={sort} onChange={setSort} ariaLabel={t('users.sort')} options={SORTS.map((x) => ({ value: x.v, label: t(x.k) }))} />
          {groups.length > 0 && <Select<number> className={s.sort} value={group} onChange={setGroup} ariaLabel={t('users.filterGroup')} options={groupOpts} />}
          {!mobile && (
            <div className="seg" role="group" aria-label={t('users.viewCards')}>
              <button aria-pressed={view === 'cards'} onClick={() => setView('cards')} aria-label={t('users.viewCards')} title={t('users.viewCards')}>
                <LayoutGrid size={16} />
              </button>
              <button aria-pressed={view === 'list'} onClick={() => setView('list')} aria-label={t('users.viewList')} title={t('users.viewList')}>
                <List size={16} />
              </button>
            </div>
          )}
          <DensityToggle className={s.density} disabled={effectiveView !== 'cards'} />
        </div>
      </div>
      <div className={s.pills} role="group" aria-label={t('users.filterStatus')}>
        {STATUSES.map((st) => (
          <button key={st || 'all'} className="pill" aria-pressed={status === st} onClick={() => setStatus(st)}>
            {st && <span className={`${s.pillDot} ${s['dot_' + st]}`} />}
            {st === 'online' ? t('common.online') : st ? t(`users.status.${st}`) : t('common.all')}
          </button>
        ))}
      </div>

      {q.isError && !q.data ? (
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : q.isPending ? (
        <div className={compact ? s.gridCompact : s.grid} aria-busy="true">
          {Array.from({ length: 8 }, (_, i) => (
            <Skeleton key={i} h={compact ? 76 : 264} r={compact ? 20 : 26} />
          ))}
        </div>
      ) : users.length === 0 ? (
        filtered ? (
          <EmptyState kind="search" title={t('users.noResults.title')} body={t('users.noResults.body')} />
        ) : (
          <EmptyState
            kind="passes"
            title={t('users.empty.title')}
            body={t('users.empty.body')}
            action={
              mayCreate ? (
                <button className="btn btn--primary" onClick={() => setParam('new', '1')}>
                  <Plus size={18} />
                  {t('users.new')}
                </button>
              ) : undefined
            }
          />
        )
      ) : effectiveView === 'cards' ? (
        <div className={compact ? s.gridCompact : s.grid}>
          {/* no exit/layout animation: a filter change swaps the list in one
              frame (old list stays until the new page arrives) instead of
              cross-fading two lists on top of each other */}
          {users.map((u, i) =>
            compact ? (
              <CompactPass key={`${listKey}#${u.id}`} u={u} index={i} inbounds={inbounds} group={u.group_id ? groupMap.get(u.group_id) : undefined} selected={sel.has(u.id)} onToggle={toggle} onOpen={open} />
            ) : (
              <PassCard key={`${listKey}#${u.id}`} u={u} index={i} inbounds={inbounds} group={u.group_id ? groupMap.get(u.group_id) : undefined} selected={sel.has(u.id)} onToggle={toggle} onOpen={open} />
            ),
          )}
        </div>
      ) : (
        <div className={`glass ${s.list}`} role="table" aria-label={t('users.title')}>
          <div className={`${s.row} ${s.rowHead}`} role="row">
            <span role="columnheader">
              <input
                type="checkbox"
                className="check"
                checked={sel.size > 0 && sel.size === users.length}
                onChange={(e) => (e.target.checked ? selectAll() : setSel(new Set()))}
                aria-label={t('users.selectAll')}
              />
            </span>
            <span role="columnheader">{t('form.username')}</span>
            <span role="columnheader">{t('form.status')}</span>
            <span role="columnheader">{t('users.used')}</span>
            <span role="columnheader">{t('users.expires')}</span>
            <span role="columnheader">{t('user.inbounds')}</span>
          </div>
          {users.map((u) => (
            <UserRow key={u.id} u={u} inbounds={inbounds} selected={sel.has(u.id)} onToggle={toggle} onOpen={open} />
          ))}
        </div>
      )}

      <div ref={sentinel} className={s.sentinel}>
        {q.isFetchingNextPage ? <Loader2 className="spin" size={22} aria-label={t('common.loading')} /> : !q.hasNextPage && users.length > 24 ? <span>{t('users.endOfList')}</span> : null}
      </div>

      {/* bulk bar */}
      <AnimatePresence>
        {sel.size > 0 && (
          <motion.div
            className={s.bulk}
            role="toolbar"
            aria-label={t('bulk.bar')}
            initial={{ y: 40, opacity: 0, x: '-50%' }}
            animate={{ y: 0, opacity: 1, x: '-50%' }}
            exit={{ y: 40, opacity: 0, x: '-50%' }}
            transition={{ type: 'spring', stiffness: 380, damping: 32 }}
          >
            <div className={s.bulkCount}>
              <button className="btn btn--sm btn--icon btn--ghost" onClick={() => setSel(new Set())} aria-label={t('users.clearSelection')}>
                <X size={16} />
              </button>
              <span className="num">{t('common.selected', { n: sel.size })}</span>
              {sel.size < users.length && (
                <button className={s.bulkAll} onClick={selectAll}>
                  {t('users.selectAll')} ({n(users.length)})
                </button>
              )}
            </div>
            <div className={s.bulkActions}>
              {mayEdit && (
                <>
                  <button className="btn btn--sm" onClick={() => runBulk('enable')} disabled={bulk.isPending}>
                    <Power size={15} />
                    <span>{t('bulk.enable')}</span>
                  </button>
                  <button className="btn btn--sm" onClick={() => runBulk('disable')} disabled={bulk.isPending}>
                    <PowerOff size={15} />
                    <span>{t('bulk.disable')}</span>
                  </button>
                  <button className="btn btn--sm" onClick={() => runBulk('extend')} disabled={bulk.isPending}>
                    <CalendarPlus size={15} />
                    <span>{t('bulk.extend')}</span>
                  </button>
                  <button className="btn btn--sm" onClick={() => runBulk('reset_traffic')} disabled={bulk.isPending}>
                    <RotateCcw size={15} />
                    <span>{t('bulk.reset')}</span>
                  </button>
                  {groups.length > 0 && (
                    <button className="btn btn--sm" onClick={() => runBulk('set_group')} disabled={bulk.isPending}>
                      <Layers size={15} />
                      <span>{t('bulk.group')}</span>
                    </button>
                  )}
                </>
              )}
              {mayDelete && (
                <button className="btn btn--sm btn--danger" onClick={() => runBulk('delete')} disabled={bulk.isPending}>
                  <Trash2 size={15} />
                  <span>{t('bulk.delete')}</span>
                </button>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      <UserDetail id={openId} onClose={() => setParam('u', null)} onEdit={(id) => setParam('edit', String(id))} inbounds={inbounds} />
      <UserForm
        open={creating || !!editId}
        editId={editId}
        onClose={() => (editId ? setParam('u', String(editId)) : setParam('new', null))}
        onSaved={(id) => setParam('u', String(id))}
        inbounds={inboundsQ.data ?? []}
      />
    </div>
  );
}
