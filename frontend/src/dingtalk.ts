export type DingTalkApp = { corpId: string; clientId: string };
type AuthCodeAPI = (app: DingTalkApp) => Promise<{ code: string }>;

export function isDingTalkClient(userAgent = navigator.userAgent): boolean {
  return /DingTalk/i.test(userAgent);
}

async function loadAuthCodeAPI(): Promise<AuthCodeAPI> {
  // Bundle the official SDK locally and load only the client platforms and API in use.
  await import('dingtalk-jsapi/entry/union');
  const { default: requestAuthCode } = await import('dingtalk-jsapi/api/union/requestAuthCode');
  return requestAuthCode;
}

export async function requestDingTalkAuthCode(app: DingTalkApp, options: { userAgent?: string; load?: () => Promise<AuthCodeAPI>; timeoutMs?: number } = {}): Promise<string> {
  if (!isDingTalkClient(options.userAgent)) throw new Error('请从钉钉工作台打开“九吨资产”，再使用钉钉登录。');
  if (!app.corpId || !app.clientId) throw new Error('钉钉应用尚未配置完整，请联系管理员。');
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    const result = await Promise.race([
      (options.load ?? loadAuthCodeAPI)().then(api => api(app)),
      new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error('钉钉授权超时，请重新打开应用后重试。')), options.timeoutMs ?? 15000); }),
    ]);
    if (!result?.code || typeof result.code !== 'string') throw new Error('钉钉未返回有效授权码，请重新打开应用后重试。');
    return result.code;
  } catch (error) {
    if (error instanceof Error) throw error;
    throw new Error('钉钉授权未完成，请确认应用使用范围，并将钉钉更新到最新版本后重试。');
  } finally { if (timer) clearTimeout(timer); }
}
