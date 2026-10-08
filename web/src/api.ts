import type { Asset, AssetListResponse, ImportResponse, Stats } from './types';

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) }, ...init });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.message ?? `请求失败（${response.status}）`);
  return payload as T;
}

export const api = {
  getStats: () => request<Stats>('/api/stats'),
  listAssets: (params: { search?: string; status?: string; category?: string; location?: string; page?: number; pageSize?: number }) => {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) if (value) query.set(key, String(value));
    return request<AssetListResponse>(`/api/assets?${query}`);
  },
  getAsset: (assetCode: string) => request<Asset>(`/api/assets/${encodeURIComponent(assetCode)}`),
  createEvent: (assetCode: string, input: Record<string, unknown>) => request<{ asset: Asset; event: unknown; replayed?: boolean }>(`/api/assets/${encodeURIComponent(assetCode)}/events`, {
    method: 'POST', body: JSON.stringify(input),
  }),
  previewImport: async (file: File) => {
    const body = new FormData();
    body.append('file', file);
    const response = await fetch('/api/imports/preview', { method: 'POST', body });
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.message ?? '导入预览失败');
    return payload as ImportResponse;
  },
  confirmImport: (input: Pick<ImportResponse, 'batchId' | 'fileName' | 'rows'>) => request<{ totalRows: number; newRows: number; updatedRows: number; unchangedRows: number; invalidRows: number }>('/api/imports/confirm', {
    method: 'POST', body: JSON.stringify(input),
  }),
};
