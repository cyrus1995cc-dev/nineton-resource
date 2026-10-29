export type DingTalkApp = { corpId: string; clientId: string };
type AuthCodeAPI = (app: DingTalkApp) => Promise<{ code: string }>;

export function isDingTalkClient(userAgent = navigator.userAgent): boolean {
  return /DingTalk/i.test(userAgent);
}

export function isMobileDingTalkClient(userAgent = navigator.userAgent): boolean {
  return isDingTalkClient(userAgent) && /Android|iPhone|iPad|HarmonyOS/i.test(userAgent);
}

type ScanAPI = (options: { type: 'qrCode' }) => Promise<{ text?: unknown }>;
async function loadScanAPI(): Promise<ScanAPI> {
  await import('dingtalk-jsapi/entry/union');
  const { default: scan } = await import('dingtalk-jsapi/api/biz/util/scan');
  return scan;
}

// Return the QR text to our lookup. Never navigate to a vendor URL from a label.
export async function scanWithDingTalk(options: { userAgent?: string; load?: () => Promise<ScanAPI>; timeoutMs?: number } = {}): Promise<string | null> {
  if (!isMobileDingTalkClient(options.userAgent)) throw new Error('请在手机钉钉中打开应用后扫码。');
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    const result = await Promise.race([
      (options.load ?? loadScanAPI)().then(scan => scan({ type: 'qrCode' })),
      new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error('钉钉扫码响应超时，请关闭扫码窗口后重试，或使用下方摄像头扫码。')), options.timeoutMs ?? 60000); }),
    ]);
    if (typeof result?.text !== 'string' || !result.text.trim()) throw new Error('未识别到二维码内容，请重试。');
    return result.text.trim();
  } catch (error) {
    const detail = error as { message?: string; errorMessage?: string; errMsg?: string } | null;
    if (/cancel|取消/i.test(String(detail?.message || detail?.errorMessage || detail?.errMsg || ''))) return null;
    if (error instanceof Error && /钉钉扫码响应超时|未识别到二维码/.test(error.message)) throw error;
    throw new Error('钉钉扫码暂不可用，请使用下方摄像头扫码，或输入资产编码。');
  } finally { if (timer) clearTimeout(timer); }
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
