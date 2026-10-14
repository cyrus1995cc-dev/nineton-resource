import type { Asset, AssetListResponse, AssetUpdateInput, AuthUser, ImportResponse, InventoryTask, InventoryTaskDetail, ManagedUser, Stats } from './types';
import { createApiClient } from './apiClient';

const authToken = () => typeof localStorage === 'undefined' ? '' : localStorage.getItem('asset_token') ?? '';
const apiBaseUrl = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '');

export const SESSION_EXPIRED_EVENT = 'asset-session-expired';
const request = createApiClient({ baseUrl: apiBaseUrl, getToken: authToken, onSessionExpired: () => {
  localStorage.removeItem('asset_token');
  window.dispatchEvent(new Event(SESSION_EXPIRED_EVENT));
} });

export const api = {
  login: async (username: string, password: string) => {
    const result = await request<{ token: string; user: AuthUser }>('/api/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) });
    localStorage.setItem('asset_token', result.token);
    return result.user;
  },
  dingtalkConfig: () => request<{ enabled: boolean; corpId: string }>('/api/auth/dingtalk/config'),
  loginDingTalk: async (code: string) => {
    const result = await request<{ token: string; user: AuthUser }>('/api/auth/dingtalk/login', { method: 'POST', body: JSON.stringify({ code }) });
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
    return request<ImportResponse>('/api/imports/preview', { method: 'POST', body });
  },
  confirmImport: (input: Pick<ImportResponse, 'batchId' | 'fileName' | 'rows'>) => request<{ totalRows: number; newRows: number; updatedRows: number; unchangedRows: number; invalidRows: number }>('/api/imports/confirm', {
    method: 'POST', body: JSON.stringify(input),
  }),
  listInventoryTasks: () => request<{ tasks: InventoryTask[] }>('/api/inventory/tasks'),
  getInventoryTask: (taskId: number) => request<InventoryTaskDetail>(`/api/inventory/tasks/${taskId}`),
  createInventoryTask: (input: { name: string; location?: string; department?: string; operatorName?: string }) => request<InventoryTaskDetail>('/api/inventory/tasks', {
    method: 'POST', body: JSON.stringify(input),
  }),
  recordInventoryScan: (taskId: number, input: { assetCode: string; actualLocation?: string; note?: string; operatorName?: string }) => request<InventoryTaskDetail & { record: InventoryTaskDetail['records'][number]; replayed?: boolean }>(`/api/inventory/tasks/${taskId}/records`, {
    method: 'POST', body: JSON.stringify(input),
  }),
  completeInventoryTask: (taskId: number, operatorName?: string) => request<InventoryTaskDetail>(`/api/inventory/tasks/${taskId}/complete`, {
    method: 'POST', body: JSON.stringify({ operatorName }),
  }),
};
