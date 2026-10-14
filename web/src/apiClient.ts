export class ApiError extends Error {
  constructor(message: string, public status: number) { super(message); }
}

export function createApiClient(options: {
  baseUrl?: string;
  getToken: () => string;
  onSessionExpired: () => void;
  fetch?: typeof fetch;
}) {
  return async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const token = options.getToken();
    const headers = new Headers(init.headers);
    if (token) headers.set('Authorization', `Bearer ${token}`);
    if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
      headers.set('Content-Type', 'application/json');
    }
    let response: Response;
    try { response = await (options.fetch ?? fetch)(`${options.baseUrl ?? ''}${path}`, { ...init, headers }); }
    catch { throw new ApiError('无法连接服务器，请检查网络和服务是否已启动', 0); }
    const publicPath = ['/api/auth/login', '/api/auth/dingtalk/login', '/api/auth/dingtalk/config'].includes(path);
    if (token && !publicPath && token !== options.getToken()) throw new ApiError('登录状态已变更，请重新操作', 0);
    if (response.status === 401 && token && !publicPath) options.onSessionExpired();
    if (response.status === 204) return undefined as T;
    const payload = await response.json().catch(() => null);
    if (!response.ok) throw new ApiError(payload?.message ?? `请求失败（${response.status}）`, response.status);
    if (payload === null) throw new ApiError('服务器返回了无法识别的内容，请稍后重试', response.status);
    return payload as T;
  };
}
