import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './hooks';
import type { Obj, XrayTemplate } from '@/pages/xray/model';

export interface TemplateInbound {
  id: number;
  tag: string;
  remark: string;
  protocol: string;
}
export interface TemplateResponse {
  template: XrayTemplate;
  /** false while the panel still uses the built-in defaults */
  custom: boolean;
  inbounds: TemplateInbound[];
  reserved: string[];
}
export interface TemplateResult {
  ok: boolean;
  tested: boolean;
  applied: boolean;
  rolled_back: boolean;
  error?: string;
  warnings: string[];
}

const key = ['xray-template'] as const;

export function useXrayTemplate() {
  return useQuery({ queryKey: key, queryFn: () => api<TemplateResponse>('/xray/template'), staleTime: 30_000 });
}

export function useTestTemplate() {
  return useMutation({ mutationFn: (template: XrayTemplate) => api<TemplateResult>('/xray/template/test', { method: 'POST', body: { template } }) });
}

export function useApplyTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (template: XrayTemplate) => api<{ result: TemplateResult; template: XrayTemplate }>('/xray/template', { method: 'PUT', body: { template } }),
    onSuccess: (r) => {
      if (r.result.applied) qc.setQueryData<TemplateResponse>(key, (old) => (old ? { ...old, template: r.template, custom: true } : old));
      qc.invalidateQueries({ queryKey: qk.stats });
    },
  });
}

export function useParseLink() {
  return useMutation({ mutationFn: (b: { link: string; tag?: string }) => api<{ outbound: Obj }>('/xray/outbounds/parse', { method: 'POST', body: b }) });
}
