import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, createApiClient } from './apiClient';

describe('authenticated API transport', () => {
  afterEach(() => vi.restoreAllMocks());
  it('sends authenticated multipart without overriding the browser boundary', async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ rows: [] }));
    const request = createApiClient({ getToken: () => 'session', onSessionExpired: vi.fn(), fetch: fetchMock });
    const body = new FormData(); body.append('file', new Blob(['excel']), 'assets.xlsx');
    await request('/api/imports/preview', { method: 'POST', body, headers: { 'X-Request': 'import' } });
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    const headers = new Headers(init.headers);
    expect(headers.get('Authorization')).toBe('Bearer session');
    expect(headers.get('X-Request')).toBe('import');
    expect(headers.has('Content-Type')).toBe(false);
    expect(init.body).toBe(body);
  });
  it('expires a protected session but preserves it for network failures and bad passwords', async () => {
    const expired = vi.fn();
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ message: '请先登录' }, { status: 401 }));
    const request = createApiClient({ getToken: () => 'old', onSessionExpired: expired, fetch: fetchMock });
    await expect(request('/api/assets')).rejects.toMatchObject({ status: 401 });
    expect(expired).toHaveBeenCalledTimes(1);
    expired.mockClear();
    await expect(request('/api/auth/login')).rejects.toBeInstanceOf(ApiError);
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));
    await expect(request('/api/auth/me')).rejects.toMatchObject({ status: 0 });
    expect(expired).not.toHaveBeenCalled();
  });
  it('reports invalid response content and accepts no-content responses', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(new Response('<html/>')).mockResolvedValueOnce(new Response(null, { status: 204 }));
    const request = createApiClient({ getToken: () => '', onSessionExpired: vi.fn(), fetch: fetchMock });
    await expect(request('/api/assets')).rejects.toThrow('无法识别');
    await expect(request('/api/auth/logout')).resolves.toBeUndefined();
  });
  it('ignores an old request after the user switches accounts', async () => {
    let token = 'old'; const expired = vi.fn();
    const request = createApiClient({ getToken: () => token, onSessionExpired: expired, fetch: async () => { token = 'new'; return Response.json({ message: '已过期' }, { status: 401 }); } });
    await expect(request('/api/assets')).rejects.toThrow('登录状态已变更');
    expect(expired).not.toHaveBeenCalled();
  });
});
