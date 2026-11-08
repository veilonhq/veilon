import { CornerDownLeft, Languages, LogOut, Moon, Plus, RotateCw, Search, UserRound } from 'lucide-react';
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { useRestartCore, useUserSearch } from '@/api/hooks';
import { useI18n } from '@/i18n';
import { confirmAction, toast } from '@/store/feedback';
import { useSession } from '@/store/session';
import { useUi } from '@/store/ui';
import { Modal } from './Overlay';
import { StatusChip } from './misc';
import { PRIMARY, SECONDARY, allowed } from './nav';
import { can, type Need } from '@/lib/perms';
import s from './CommandPalette.module.css';

interface Cmd {
  id: string;
  group: 'pages' | 'actions' | 'users';
  label: string;
  hint?: ReactNode;
  icon: ReactNode;
  keywords?: string;
  run: () => void;
}

function useDebounced<T>(v: T, ms: number) {
  const [d, setD] = useState(v);
  useEffect(() => {
    const t = setTimeout(() => setD(v), ms);
    return () => clearTimeout(t);
  }, [v, ms]);
  return d;
}

export function CommandPalette() {
  const { t, lang } = useI18n();
  const open = useUi((x) => x.paletteOpen);
  const setPalette = useUi((x) => x.setPalette);
  const { toggleTheme, setLang } = useUi();
  const logout = useSession((x) => x.logout);
  const admin = useSession((x) => x.admin);
  const nav = useNavigate();
  const restart = useRestartCore();
  const [q, setQ] = useState('');
  const [idx, setIdx] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);
  const dq = useDebounced(q, 180);
  const users = useUserSearch(open && can(admin, 'users_view') ? dq : '');

  useEffect(() => {
    if (open) {
      setQ('');
      setIdx(0);
    }
  }, [open]);

  const close = () => setPalette(false);
  const go = (to: string) => {
    close();
    nav(to);
  };

  const cmds = useMemo<Cmd[]>(() => {
    const pages: Cmd[] = allowed([...PRIMARY, ...SECONDARY], admin).map((p) => ({
      id: 'p' + p.to,
      group: 'pages',
      label: t(p.key),
      icon: <p.icon size={18} />,
      keywords: p.to,
      run: () => go(p.to),
    }));
    const actions: Cmd[] = ([
      { id: 'a-new-user', need: 'users_create', group: 'actions', label: t('users.new'), icon: <Plus size={18} />, keywords: 'create user add', run: () => go('/users?new=1') },
      { id: 'a-new-inb', need: 'inbounds', group: 'actions', label: t('inb.new'), icon: <Plus size={18} />, keywords: 'create inbound add', run: () => go('/inbounds?new=1') },
      { id: 'a-theme', group: 'actions', label: t('cmd.toggleTheme'), icon: <Moon size={18} />, keywords: 'dark light theme', run: () => (toggleTheme(), close()) },
      { id: 'a-lang', group: 'actions', label: t('cmd.switchLang'), icon: <Languages size={18} />, keywords: 'language farsi english', run: () => (setLang(lang === 'fa' ? 'en' : 'fa'), close()) },
      {
        id: 'a-restart',
        need: 'settings',
        group: 'actions',
        label: t('cmd.restartCore'),
        icon: <RotateCw size={18} />,
        keywords: 'xray restart core',
        run: async () => {
          close();
          const ok = await confirmAction({ title: t('dash.restartConfirm'), body: t('dash.restartBody'), confirmLabel: t('dash.restart') });
          if (ok) restart.mutate(undefined, { onSuccess: () => toast.success(t('dash.restarted')), onError: (e) => toast.error(t('common.error'), (e as Error).message) });
        },
      },
      { id: 'a-logout', group: 'actions', label: t('common.logout'), icon: <LogOut size={18} />, keywords: 'logout sign out exit', run: () => (close(), logout()) },
    ] as Array<Cmd & { need?: Need }>).filter((c) => can(admin, c.need));
    const needle = q.trim().toLowerCase();
    const match = (c: Cmd) => !needle || c.label.toLowerCase().includes(needle) || (c.keywords ?? '').includes(needle);
    const userCmds: Cmd[] = (needle && can(admin, 'users_view') ? users.data?.items ?? [] : []).map((u) => ({
      id: 'u' + u.id,
      group: 'users',
      label: u.username,
      icon: <UserRound size={18} />,
      hint: <StatusChip status={u.status} />,
      run: () => go(`/users?u=${u.id}`),
    }));
    return [...pages.filter(match), ...actions.filter(match), ...userCmds];
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, users.data, t, lang, admin]);

  useEffect(() => {
    setIdx(0);
  }, [q]);
  useEffect(() => {
    listRef.current?.querySelector(`[data-idx="${idx}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [idx]);

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setIdx((i) => Math.min(cmds.length - 1, i + 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setIdx((i) => Math.max(0, i - 1));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      cmds[idx]?.run();
    }
  };

  const groups: Array<[Cmd['group'], string]> = [
    ['pages', t('cmd.pages')],
    ['actions', t('cmd.actions')],
    ['users', t('cmd.users')],
  ];
  let running = -1;

  return (
    <Modal open={open} onClose={close} label={t('cmd.open')} className={s.palette} align="top">
      <div className={s.inputRow}>
        <Search size={20} className={s.searchIcon} />
        <input
          className={s.input}
          placeholder={t('cmd.placeholder')}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={onKey}
          data-autofocus
          role="combobox"
          aria-expanded="true"
          aria-controls="cmd-list"
          aria-activedescendant={cmds[idx] ? `cmd-${cmds[idx].id}` : undefined}
          aria-autocomplete="list"
        />
        <kbd className="kbd">Esc</kbd>
      </div>
      <div className={s.list} id="cmd-list" role="listbox" ref={listRef}>
        {cmds.length === 0 && !users.isFetching && <div className={s.empty}>{t('cmd.empty')}</div>}
        {groups.map(([g, label]) => {
          const items = cmds.filter((c) => c.group === g);
          if (!items.length) return null;
          return (
            <div key={g} role="group" aria-label={label}>
              <div className={s.groupLabel}>{label}</div>
              {items.map((c) => {
                running++;
                const i = running;
                return (
                  <div
                    key={c.id}
                    id={`cmd-${c.id}`}
                    role="option"
                    aria-selected={i === idx}
                    data-idx={i}
                    className={`${s.item} ${i === idx ? s.itemActive : ''}`}
                    onMouseMove={() => setIdx(i)}
                    onClick={() => c.run()}
                  >
                    <span className={s.icon}>{c.icon}</span>
                    <span className={s.label}>{c.label}</span>
                    {c.hint}
                    {i === idx && <CornerDownLeft size={15} className={s.enter} />}
                  </div>
                );
              })}
            </div>
          );
        })}
        {users.isFetching && <div className={s.empty}>{t('common.loading')}</div>}
      </div>
      <div className={s.foot}>{t('cmd.hint')}</div>
    </Modal>
  );
}
