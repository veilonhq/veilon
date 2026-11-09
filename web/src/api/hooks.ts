import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient, type InfiniteData } from '@tanstack/react-query';
import { useEffect } from 'react';
import { api } from './client';
import { useSession } from '@/store/session';
import { useUi } from '@/store/ui';
import { can } from '@/lib/perms';
import type {
  AcmeRequest,
  Admin,
  AdminPatch,
  GroupInput,
  AuditEntry,
  BulkAction,
  Cert,
  Inbound,
  InboundInput,
  LoginResponse,
  Paged,
  PublicIP,
  RealityKeys,
  Settings,
  SystemStats,
  TrafficPoint,
  TrafficRange,
  User,
  UserInput,
  UserLink,
  UserPatch,
  UserStatus,
  WgKeys,
  Group,
  UserConnections,
} from './types';

export const qk = {
  stats: ['stats'] as const,
  traffic: (r: TrafficRange) => ['traffic', r] as const,
  users: ['users'] as const,
  usersList: (f: UserFilters) => ['users', 'list', f] as const,
  user: (id: number) => ['users', 'one', id] as const,
  links: (id: number) => ['users', 'links', id] as const,
  conns: (id: number) => ['users', 'conns', id] as const,
  groups: ['groups'] as const,
  inbounds: ['inbounds'] as const,
  certs: ['certs'] as const,
  settings: ['settings'] as const,
  admins: ['admins'] as const,
  audit: (p: number) => ['audit', p] as const,
  logs: (core: string) => ['logs', core] as const,
  config: (core: string) => ['core-config', core] as const,
  me: ['me'] as const,
};

/* ---------- auth ---------- */
export const login = (username: string, password: string) =>
  api<LoginResponse>('/auth/login', { method: 'POST', body: { username, password } });

export function useMe(enabled: boolean) {
  return useQuery({ queryKey: qk.me, queryFn: () => api<Admin>('/auth/me'), enabled, staleTime: 60_000 });
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (b: { current_password: string; new_password: string }) =>
      api<{ ok: boolean; token?: string; expires_at?: string }>('/auth/password', { method: 'POST', body: b }),
    onSuccess: (r) => {
      // tokens issued before the change stop working; the response carries a fresh one
      const s = useSession.getState();
      if (r?.token && s.admin) s.login(r.token, r.expires_at ?? s.expiresAt ?? '', s.admin);
    },
  });
}

/** Rename the signed-in admin (PATCH /auth/me). Requires the current password. */
export function useUpdateMe() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: { username: string; current_password: string }) =>
      api<LoginResponse>('/auth/me', { method: 'PATCH', body: b }),
    onSuccess: (r) => {
      useSession.getState().login(r.token, r.expires_at, r.admin);
      qc.setQueryData(qk.me, r.admin);
      qc.invalidateQueries({ queryKey: qk.admins });
    },
  });
}

/* ---------- system ---------- */
export function useStats(intervalMs = 2000) {
  return useQuery({ queryKey: qk.stats, queryFn: () => api<SystemStats>('/system/stats'), refetchInterval: intervalMs });
}
export function useTraffic(range: TrafficRange) {
  return useQuery({
    queryKey: qk.traffic(range),
    queryFn: () => api<{ points: TrafficPoint[]; bucket_sec?: number }>('/system/traffic', { query: { range } }),
    refetchInterval: 60_000,
  });
}
export function useRestartCore() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api('/core/restart', { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.stats }),
  });
}
/** One log line (GET /core/logs entries). */
export interface LogEntry {
  /** orders lines across sources; 0 when unknown */
  seq: number;
  /** RFC 3339 UTC; null when the line carries no time */
  t: string | null;
  source: string;
  level: 'debug' | 'info' | 'warning' | 'error';
  msg: string;
}
/** The most lines the server keeps per source (and returns per request). */
export const LOG_LINES = 2000;
export function useCoreLogs(live: boolean, core = 'xray') {
  return useQuery({
    queryKey: qk.logs(core),
    queryFn: async () => {
      const r = await api<{ lines: string[]; entries?: LogEntry[] }>('/core/logs', { query: { core, lines: LOG_LINES } });
      // older servers send only plain lines
      const entries: LogEntry[] = r.entries ?? r.lines.map((msg) => ({ seq: 0, t: null, source: core, level: 'info', msg }));
      return { entries };
    },
    refetchInterval: live ? 2000 : false,
  });
}
export function useCoreConfig(enabled: boolean, core = 'xray') {
  return useQuery({ queryKey: qk.config(core), queryFn: () => api<unknown>('/core/config', { query: { core } }), enabled });
}

/* ---------- users ---------- */
/** How often user lists and details re-read online status (the server polls cores every 10s). */
export const ONLINE_POLL_MS = 8_000;
export interface UserFilters {
  search: string;
  status: UserStatus | 'online' | '';
  sort: string;
  /** 0 = any group, -1 = no group */
  group: number;
}
const PER_PAGE = 24;

export function useUsersInfinite(f: UserFilters) {
  return useInfiniteQuery({
    queryKey: qk.usersList(f),
    initialPageParam: 1,
    queryFn: ({ pageParam, signal }) =>
      api<Paged<User>>('/users', { query: { search: f.search, status: f.status, sort: f.sort, group_id: f.group || undefined, page: pageParam, per_page: PER_PAGE }, signal }),
    getNextPageParam: (last, all) => {
      const loaded = all.reduce((s, p) => s + p.items.length, 0);
      return loaded < last.total ? all.length + 1 : undefined;
    },
    placeholderData: keepPreviousData,
    // online status and usage move constantly: refresh in place (the list
    // keeps its data while refetching, so nothing flickers)
    refetchInterval: ONLINE_POLL_MS,
  });
}

export function useUserSearch(search: string) {
  return useQuery({
    queryKey: ['users', 'search', search],
    queryFn: ({ signal }) => api<Paged<User>>('/users', { query: { search, per_page: 6 }, signal }),
    enabled: search.trim().length > 0,
    staleTime: 10_000,
  });
}

/** The user as already shown in any loaded list page (instant detail sheet, no skeleton). */
function userFromLists(qc: ReturnType<typeof useQueryClient>, id: number): User | undefined {
  for (const [, d] of qc.getQueriesData<InfiniteData<Paged<User>>>({ queryKey: ['users', 'list'] })) {
    for (const p of d?.pages ?? []) {
      const u = p.items.find((x) => x.id === id);
      if (u) return u;
    }
  }
  return undefined;
}

export function useUser(id: number | null) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: qk.user(id ?? 0),
    queryFn: () => api<User>(`/users/${id}`),
    enabled: !!id,
    refetchInterval: ONLINE_POLL_MS,
    placeholderData: () => (id ? userFromLists(qc, id) : undefined),
  });
}

/**
 * Warm the data the dock pages open with, once the shell is idle, so the
 * first visit renders content instead of skeletons that then pop in.
 */
export function prefetchPageData(qc: ReturnType<typeof useQueryClient>) {
  const me = useSession.getState().admin;
  const f: UserFilters = { search: '', status: '', sort: '-created_at', group: 0 };
  if (can(me, 'users_view'))
    void qc.prefetchInfiniteQuery({
      queryKey: qk.usersList(f),
      initialPageParam: 1,
      queryFn: ({ pageParam }) => api<Paged<User>>('/users', { query: { search: '', status: '', sort: f.sort, page: pageParam as number, per_page: PER_PAGE } }),
    });
  void qc.prefetchQuery({ queryKey: qk.inbounds, queryFn: () => api<Inbound[]>('/inbounds') });
  // the users toolbar shows the group filter once groups are known: have them ready
  if (can(me, ['users_view', 'groups'])) void qc.prefetchQuery({ queryKey: qk.groups, queryFn: () => api<Group[]>('/groups'), staleTime: 30_000 });
  if (can(me, 'certs')) void qc.prefetchQuery({ queryKey: qk.certs, queryFn: () => api<Cert[]>('/certs') });
  void qc.prefetchQuery({ queryKey: qk.settings, queryFn: () => api<Settings>('/settings') });
}
export function useUserLinks(id: number | null) {
  return useQuery({
    queryKey: qk.links(id ?? 0),
    queryFn: () => api<{ links: UserLink[]; sub_url: string }>(`/users/${id}/links`),
    enabled: !!id,
  });
}

type UsersPages = InfiniteData<Paged<User>, number>;

/** Apply a patch to every cached copy of a user (lists + detail). */
function patchUserCaches(qc: ReturnType<typeof useQueryClient>, ids: number[], fn: (u: User) => User | null) {
  const set = new Set(ids);
  qc.setQueriesData<UsersPages>({ queryKey: ['users', 'list'] }, (old) => {
    if (!old) return old;
    let removed = 0;
    const pages = old.pages.map((p) => {
      const items: User[] = [];
      for (const u of p.items) {
        if (!set.has(u.id)) {
          items.push(u);
          continue;
        }
        const n = fn(u);
        if (n) items.push(n);
        else removed++;
      }
      return { ...p, items };
    });
    return { ...old, pages: pages.map((p) => ({ ...p, total: p.total - removed })) };
  });
  for (const id of ids) {
    qc.setQueryData<User>(qk.user(id), (old) => (old ? fn(old) ?? old : old));
  }
}

function useOptimisticUserMutation<V>(
  mutationFn: (v: V) => Promise<unknown>,
  ids: (v: V) => number[],
  apply: (u: User, v: V) => User | null,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn,
    onMutate: async (v: V) => {
      await qc.cancelQueries({ queryKey: qk.users });
      const snapshot = qc.getQueriesData({ queryKey: qk.users });
      patchUserCaches(qc, ids(v), (u) => apply(u, v));
      return { snapshot };
    },
    onError: (_e, _v, ctx) => {
      ctx?.snapshot.forEach(([k, d]) => qc.setQueryData(k, d));
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: qk.users });
      qc.invalidateQueries({ queryKey: qk.stats });
      // member counts move with deletes and group changes
      qc.invalidateQueries({ queryKey: qk.groups });
    },
  });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: UserInput) => api<User>('/users', { method: 'POST', body: b }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.users });
      qc.invalidateQueries({ queryKey: qk.inbounds });
    },
  });
}

export function useUpdateUser() {
  return useOptimisticUserMutation(
    (v: { id: number; patch: UserPatch }) => api<User>(`/users/${v.id}`, { method: 'PATCH', body: v.patch }),
    (v) => [v.id],
    (u, v) => ({ ...u, ...v.patch, status: v.patch.status ?? u.status }) as User,
  );
}

export function useDeleteUser() {
  return useOptimisticUserMutation(
    (id: number) => api(`/users/${id}`, { method: 'DELETE' }),
    (id) => [id],
    () => null,
  );
}

export function useResetTraffic() {
  return useOptimisticUserMutation(
    (id: number) => api<User>(`/users/${id}/reset-traffic`, { method: 'POST' }),
    (id) => [id],
    (u) => ({ ...u, used_traffic: 0, up: 0, down: 0, status: u.status === 'limited' ? 'active' : u.status }),
  );
}

export function useRevokeSub() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api<User>(`/users/${id}/revoke-sub`, { method: 'POST' }),
    onSuccess: (u) => {
      qc.setQueryData(qk.user(u.id), u);
      qc.invalidateQueries({ queryKey: qk.links(u.id) });
      qc.invalidateQueries({ queryKey: ['users', 'list'] });
    },
  });
}

/* ---------- connection log (IPs and HWID devices) ---------- */
export function useUserConnections(id: number | null) {
  return useQuery({
    queryKey: qk.conns(id ?? 0),
    queryFn: ({ signal }) => api<UserConnections>(`/users/${id}/connections`, { signal }),
    enabled: !!id,
    refetchInterval: 15_000,
  });
}

/** Optimistic edit of one user's connection log; refetches on settle. */
function useConnMutation<V>(fn: (v: V) => Promise<unknown>, uid: (v: V) => number, apply: (c: UserConnections, v: V) => UserConnections) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onMutate: async (v: V) => {
      const key = qk.conns(uid(v));
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<UserConnections>(key);
      if (prev) qc.setQueryData(key, apply(prev, v));
      return { prev };
    },
    onError: (_e, v, ctx) => {
      if (ctx?.prev) qc.setQueryData(qk.conns(uid(v)), ctx.prev);
    },
    onSettled: (_d, _e, v) => qc.invalidateQueries({ queryKey: qk.conns(uid(v)) }),
  });
}

export function useBlockIP() {
  return useConnMutation(
    (v: { uid: number; ip: string; blocked: boolean }) => api(`/users/${v.uid}/ips`, { method: 'POST', body: { ip: v.ip, blocked: v.blocked } }),
    (v) => v.uid,
    (c, v) => ({ ...c, ips: c.ips.map((x) => (x.ip === v.ip ? { ...x, blocked: v.blocked } : x)) }),
  );
}
export function useDeleteIP() {
  return useConnMutation(
    (v: { uid: number; id: number }) => api(`/users/${v.uid}/ips/${v.id}`, { method: 'DELETE' }),
    (v) => v.uid,
    (c, v) => ({ ...c, ips: c.ips.filter((x) => x.id !== v.id) }),
  );
}
export function useBlockDevice() {
  return useConnMutation(
    (v: { uid: number; id: number; blocked: boolean }) => api(`/users/${v.uid}/devices/${v.id}`, { method: 'PATCH', body: { blocked: v.blocked } }),
    (v) => v.uid,
    (c, v) => ({ ...c, devices: c.devices.map((x) => (x.id === v.id ? { ...x, blocked: v.blocked } : x)) }),
  );
}
export function useDeleteDevice() {
  return useConnMutation(
    (v: { uid: number; id: number }) => api(`/users/${v.uid}/devices/${v.id}`, { method: 'DELETE' }),
    (v) => v.uid,
    (c, v) => ({ ...c, devices: c.devices.filter((x) => x.id !== v.id) }),
  );
}

/* ---------- groups ---------- */
export function useGroups(enabled = true) {
  return useQuery({ queryKey: qk.groups, queryFn: () => api<Group[]>('/groups'), enabled, staleTime: 30_000 });
}

export function useSaveGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: number | null; body: Partial<GroupInput> }) =>
      api<Group>(id ? `/groups/${id}` : '/groups', { method: id ? 'PATCH' : 'POST', body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.groups }),
  });
}
export function useDeleteGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api(`/groups/${id}`, { method: 'DELETE' }),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: qk.groups });
      const prev = qc.getQueryData<Group[]>(qk.groups);
      qc.setQueryData<Group[]>(qk.groups, (old) => old?.filter((g) => g.id !== id));
      return { prev };
    },
    onError: (_e, _v, ctx) => qc.setQueryData(qk.groups, ctx?.prev),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: qk.groups });
      qc.invalidateQueries({ queryKey: qk.users });
    },
  });
}

export function useBulk() {
  return useOptimisticUserMutation(
    (v: { action: BulkAction; ids: number[]; days?: number; group_id?: number }) => api<{ affected?: number }>('/users/bulk', { method: 'POST', body: v }),
    (v) => v.ids,
    (u, v) => {
      switch (v.action) {
        case 'delete':
          return null;
        case 'disable':
          return { ...u, status: 'disabled' };
        case 'enable':
          return { ...u, status: 'active' };
        case 'reset_traffic':
          return { ...u, used_traffic: 0, up: 0, down: 0 };
        case 'extend': {
          const base = u.expire_at ? Math.max(new Date(u.expire_at).getTime(), Date.now()) : null;
          return base ? { ...u, expire_at: new Date(base + (v.days ?? 0) * 86_400_000).toISOString() } : u;
        }
        case 'set_group':
          return { ...u, group_id: v.group_id || null };
      }
    },
  );
}

/* ---------- inbounds ---------- */
export function useInbounds() {
  return useQuery({ queryKey: qk.inbounds, queryFn: () => api<Inbound[]>('/inbounds') });
}
export function useSaveInbound() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id?: number; body: InboundInput }) =>
      v.id ? api<Inbound>(`/inbounds/${v.id}`, { method: 'PATCH', body: v.body }) : api<Inbound>('/inbounds', { method: 'POST', body: v.body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.inbounds }),
  });
}
export function useToggleInbound() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: number; enabled: boolean }) => api<Inbound>(`/inbounds/${v.id}`, { method: 'PATCH', body: { enabled: v.enabled } }),
    onMutate: async (v) => {
      await qc.cancelQueries({ queryKey: qk.inbounds });
      const prev = qc.getQueryData<Inbound[]>(qk.inbounds);
      qc.setQueryData<Inbound[]>(qk.inbounds, (old) => old?.map((i) => (i.id === v.id ? { ...i, enabled: v.enabled } : i)));
      return { prev };
    },
    onError: (_e, _v, ctx) => qc.setQueryData(qk.inbounds, ctx?.prev),
    onSettled: () => qc.invalidateQueries({ queryKey: qk.inbounds }),
  });
}
export function useDeleteInbound() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api(`/inbounds/${id}`, { method: 'DELETE' }),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: qk.inbounds });
      const prev = qc.getQueryData<Inbound[]>(qk.inbounds);
      qc.setQueryData<Inbound[]>(qk.inbounds, (old) => old?.filter((i) => i.id !== id));
      return { prev };
    },
    onError: (_e, _v, ctx) => qc.setQueryData(qk.inbounds, ctx?.prev),
    onSettled: () => qc.invalidateQueries({ queryKey: qk.inbounds }),
  });
}
export const genRealityKeys = () => api<RealityKeys>('/inbounds/reality-keys', { method: 'POST' });
export const genWgKeys = () => api<WgKeys>('/inbounds/wireguard-keys', { method: 'POST' });

/* ---------- certs ---------- */
export function useCerts() {
  return useQuery({ queryKey: qk.certs, queryFn: () => api<Cert[]>('/certs') });
}
export function useAcme() {
  const qc = useQueryClient();
  return useMutation({
    // Issuance can take up to ~4 minutes (HTTP-01 validation); no client timeout.
    mutationFn: (b: AcmeRequest) => api<Cert>('/certs/acme', { method: 'POST', body: { http_port: 80, ...b } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.certs });
      qc.invalidateQueries({ queryKey: qk.settings });
    },
  });
}
/** Detected public addresses of the server (external checkers + interfaces). */
export function usePublicIP(enabled: boolean) {
  return useQuery({ queryKey: ['certs', 'public-ip'], queryFn: () => api<PublicIP>('/certs/public-ip'), enabled, staleTime: 10 * 60_000, retry: 0 });
}
function useCertMutation<V>(fn: (v: V) => Promise<unknown>) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: fn, onSettled: () => qc.invalidateQueries({ queryKey: qk.certs }) });
}
export const useSetCertAutoRenew = () =>
  useCertMutation((v: { id: number; auto_renew: boolean }) => api<Cert>(`/certs/${v.id}`, { method: 'PATCH', body: { auto_renew: v.auto_renew } }));
export const useRenewCert = () => useCertMutation((id: number) => api<Cert>(`/certs/${id}/renew`, { method: 'POST' }));
export const useRevokeCert = () => useCertMutation((id: number) => api<{ ok: boolean }>(`/certs/${id}/revoke`, { method: 'POST' }));
export function useSelfSignedCert() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: { domain: string }) => api<Cert>('/certs/self-signed', { method: 'POST', body: b }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.certs }),
  });
}
export function useUploadCert() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: { domain: string; cert_pem: string; key_pem: string }) => api<Cert>('/certs/upload', { method: 'POST', body: b }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.certs }),
  });
}
export function useDeleteCert() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api(`/certs/${id}`, { method: 'DELETE' }),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: qk.certs });
      const prev = qc.getQueryData<Cert[]>(qk.certs);
      qc.setQueryData<Cert[]>(qk.certs, (old) => old?.filter((c) => c.id !== id));
      return { prev };
    },
    onError: (_e, _v, ctx) => qc.setQueryData(qk.certs, ctx?.prev),
    onSettled: () => qc.invalidateQueries({ queryKey: qk.certs }),
  });
}

/* ---------- settings ---------- */
export function useSettings() {
  return useQuery({ queryKey: qk.settings, queryFn: () => api<Settings>('/settings') });
}
/** The panel's time zone (settings), used for absolute timestamps. */
export function usePanelTz(): string | undefined {
  const fromSettings = useQuery({ queryKey: qk.settings, queryFn: () => api<Settings>('/settings'), staleTime: 5 * 60_000, select: (st) => st.timezone || undefined }).data;
  const synced = useUi((x) => x.tz);
  return synced ?? fromSettings;
}
/**
 * Keeps the UI store's time zone equal to the panel setting, so every
 * formatter (dates, charts, logs) uses the panel's clock. Mounted once by
 * the signed-in shell.
 */
export function useSyncPanelTz() {
  const tz = useQuery({ queryKey: qk.settings, queryFn: () => api<Settings>('/settings'), staleTime: 5 * 60_000, select: (st) => st.timezone || 'UTC' }).data;
  const setTz = useUi((x) => x.setTz);
  useEffect(() => {
    if (tz) setTz(tz);
  }, [tz, setTz]);
}
export function useSaveSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: Partial<Settings>) => api<Settings>('/settings', { method: 'PATCH', body: b }),
    onSuccess: (s) => qc.setQueryData(qk.settings, s),
  });
}

/* ---------- admins ---------- */
export function useAdmins(enabled: boolean) {
  return useQuery({ queryKey: qk.admins, queryFn: () => api<Admin[]>('/admins'), enabled });
}
export function useCreateAdmin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: { username: string; password: string; is_sudo: boolean } & AdminPatch) => api<Admin>('/admins', { method: 'POST', body: b }),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.admins }),
  });
}
/** PATCH /admins/{id}: permissions, quotas and sudo access (sudo only). */
export function useUpdateAdmin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: number; patch: AdminPatch }) => api<Admin>(`/admins/${id}`, { method: 'PATCH', body: patch }),
    onSuccess: (a) => {
      qc.setQueryData<Admin[]>(qk.admins, (old) => old?.map((x) => (x.id === a.id ? a : x)));
      qc.invalidateQueries({ queryKey: qk.admins });
    },
  });
}
export function useDeleteAdmin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api(`/admins/${id}`, { method: 'DELETE' }),
    onMutate: async (id) => {
      const prev = qc.getQueryData<Admin[]>(qk.admins);
      qc.setQueryData<Admin[]>(qk.admins, (old) => old?.filter((a) => a.id !== id));
      return { prev };
    },
    onError: (_e, _v, ctx) => qc.setQueryData(qk.admins, ctx?.prev),
    onSettled: () => qc.invalidateQueries({ queryKey: qk.admins }),
  });
}

/* ---------- audit ---------- */
export const AUDIT_PER_PAGE = 20;
export function useAudit(page: number) {
  return useQuery({
    queryKey: qk.audit(page),
    queryFn: () => api<Paged<AuditEntry>>('/audit', { query: { page, per_page: AUDIT_PER_PAGE } }),
    placeholderData: keepPreviousData,
  });
}
