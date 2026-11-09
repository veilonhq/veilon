import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './hooks';
import type { CoreInfo } from './types';

export interface XrayRelease {
  tag: string;
  prerelease: boolean;
  published_at: string;
}
export interface XrayReleaseList {
  releases: XrayRelease[];
  fetched_at: string;
  stale: boolean;
  rate_limited_until?: string;
  error?: string;
}
export interface XrayJob {
  kind: 'install' | 'geo';
  version?: string;
  running: boolean;
  error?: string;
  rolled_back?: boolean;
  started_at: string;
  finished_at?: string;
}
export interface XrayGeoFile {
  name: string;
  size: number;
  updated_at: string | null;
}
export interface XrayInfo {
  status: CoreInfo;
  job: XrayJob | null;
  geo: XrayGeoFile[] | null;
  /** engines an admin switched off */
  stopped_cores?: string[];
}

const xk = {
  info: ['xray-info'] as const,
  releases: ['xray-releases'] as const,
};

/** Core state plus the running/last install or geo job; polls while a job runs. */
export function useXrayInfo(busy: boolean) {
  return useQuery({
    queryKey: xk.info,
    queryFn: () => api<XrayInfo>('/core/xray'),
    refetchInterval: (q) => (busy || q.state.data?.job?.running ? 2000 : false),
  });
}

/** Published releases; the server caches GitHub for an hour. */
export function useXrayReleases() {
  return useQuery({
    queryKey: xk.releases,
    queryFn: () => api<XrayReleaseList>('/core/xray/releases'),
    staleTime: 10 * 60_000,
    retry: false,
  });
}

function useCoreMutation<T>(fn: (v: T) => Promise<unknown>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSettled: () => {
      qc.invalidateQueries({ queryKey: qk.stats });
      qc.invalidateQueries({ queryKey: xk.info });
    },
  });
}

/** Which cores a power switch touches: only Xray, or every core (xray, sing-box, mtg, amneziawg). */
export type CoreScope = 'xray' | 'all';
/** Stop defaults to Xray only; start with no scope brings back every core that was switched off. */
export const useCorePower = () =>
  useCoreMutation(({ on, scope }: { on: boolean; scope?: CoreScope }) =>
    api(on ? '/core/start' : '/core/stop', { method: 'POST', body: scope ? { scope } : undefined }),
  );
export const useInstallXray = () => useCoreMutation((version: string) => api('/core/xray/install', { method: 'POST', body: { version } }));
export const useUpdateGeo = () => useCoreMutation((_: void) => api('/core/xray/geo', { method: 'POST' }));
