import { AppError } from './errors';

export class DingTalkClient {
  private token = ''; private expiresAt = 0;
  constructor(private fetcher: typeof fetch = fetch) {}
  async json(url: string, body: unknown) {
    let response: Response;
    try { response = await this.fetcher(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal: AbortSignal.timeout(15000) }); }
    catch { throw new AppError(502, '连接钉钉超时或网络不可用，请稍后重试'); }
    const data = await response.json().catch(() => null) as Record<string, any> | null;
    if (!response.ok || !data || (data.errcode !== undefined && Number(data.errcode) !== 0)) {
      const code = String(data?.errcode ?? data?.code ?? response.status).replace(/[^a-zA-Z0-9._-]/g, '').slice(0, 80);
      throw new AppError(502, `钉钉请求失败（${code}），请核对应用权限、通讯录范围和配置`);
    }
    return data;
  }
  async accessToken() {
    if (this.token && this.expiresAt > Date.now()) return this.token;
    if (!process.env.DINGTALK_CLIENT_ID || !process.env.DINGTALK_CLIENT_SECRET || !process.env.DINGTALK_CORP_ID) throw new AppError(503, '请先配置钉钉企业应用参数');
    const data = await this.json('https://api.dingtalk.com/v1.0/oauth2/accessToken', { appKey: process.env.DINGTALK_CLIENT_ID, appSecret: process.env.DINGTALK_CLIENT_SECRET });
    if (typeof data.accessToken !== 'string' || !data.accessToken) throw new AppError(502, '钉钉未返回有效企业应用令牌');
    this.token = data.accessToken; this.expiresAt = Date.now() + Math.max(0, Number(data.expireIn ?? 7200) - 120) * 1000; return this.token;
  }
  async call(path: string, body: unknown) {
    const token = await this.accessToken();
    try { return await this.json(`https://oapi.dingtalk.com${path}?access_token=${encodeURIComponent(token)}`, body); }
    catch (error) { this.token = ''; this.expiresAt = 0; throw error; }
  }
}
