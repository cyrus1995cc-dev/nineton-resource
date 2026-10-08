import type { Asset, AssetListResponse, AssetUpdateInput, AuthUser, ImportResponse, ManagedUser, Stats } from './types';

const authToken = () => typeof localStorage === 'undefined' ? '' : localStorage.getItem('asset_token') ?? '';

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const token = authToken();
  const response = await fetch(path, { headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(init?.headers ?? {}) }, ...init });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.message ?? `请求失败（${response.status}）`);
  return payload as T;
}

export const api = {
  login: async (username: string, password: string) => {
    const result = await request<{ token: string; user: AuthUser }>('/api/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) });
    localStorage.setItem('asset_token', result.token);
    return result.user;
  },
  me: async () => (await request<{ user: AuthUser }>('/api/auth/me')).user,
  listUsers: () => request<{ users: ManagedUser[] }>('/api/auth/users'),
  createUser: (input: { username: string; password: string; display_name: string; role: ManagedUser['role'] }) => request<{ user: ManagedUser }>('/api/auth/users', {
    method: 'POST', body: JSON.stringify(input),
  }),
  logout: async () => { await request<void>('/api/auth/logout', { method: 'POST' }).catch(() => undefined); localStorage.removeItem('asset_token'); },
  getStats: () => request<Stats>('/api/stats'),
  listAssets: (params: { search?: string; status?: string; category?: string; location?: string; page?: number; pageSize?: number }) => {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) if (value) query.set(key, String(value));
    return request<AssetListResponse>(`/api/assets?${query}`);
  },
  getAsset: (assetCode: string) => request<Asset>(`/api/assets/${encodeURIComponent(assetCode)}`),
  updateAsset: (assetCode: string, input: AssetUpdateInput) => request<{ asset: Asset; event: unknown }>(`/api/assets/${encodeURIComponent(assetCode)}`, {
    method: 'PATCH', body: JSON.stringify(input),
  }),
  saveQr: (assetCode: string, raw: string) => request<{ qr_raw: string; qr_format: string }>(`/api/assets/${encodeURIComponent(assetCode)}/qr`, {
    method: 'POST', body: JSON.stringify({ raw }),
  }),
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
