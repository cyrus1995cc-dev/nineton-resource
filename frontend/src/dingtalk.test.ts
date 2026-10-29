import { afterEach, describe, expect, it, vi } from 'vitest';
import { isDingTalkClient, requestDingTalkAuthCode } from './dingtalk';

const app = { corpId: 'ding-company', clientId: 'ding-application' };
const client = { userAgent: 'Mozilla/5.0 DingTalk/7.6.0' };
afterEach(() => vi.useRealTimers());

describe('DingTalk enterprise login boundary', () => {
  it('only loads the native SDK in a DingTalk client', async () => {
    const load = vi.fn();
    expect(isDingTalkClient(client.userAgent)).toBe(true);
    expect(isDingTalkClient('Mozilla Chrome')).toBe(false);
    await expect(requestDingTalkAuthCode(app, { userAgent: 'Mozilla Chrome', load })).rejects.toThrow('工作台');
    expect(load).not.toHaveBeenCalled();
  });
  it('supplies both enterprise and application identity to the official API', async () => {
    const invoke = vi.fn(async () => ({ code: 'single-use-code' }));
    expect(await requestDingTalkAuthCode(app, { ...client, load: async () => invoke })).toBe('single-use-code');
    expect(invoke).toHaveBeenCalledWith(app);
  });
  it('rejects incomplete application configuration and empty authorization results', async () => {
    await expect(requestDingTalkAuthCode({ ...app, clientId: '' }, client)).rejects.toThrow('配置完整');
    await expect(requestDingTalkAuthCode(app, { ...client, load: async () => async () => ({ code: '' }) })).rejects.toThrow('有效授权码');
  });
  it('releases a hanging native authorization attempt with an actionable timeout', async () => {
    vi.useFakeTimers();
    const pending = requestDingTalkAuthCode(app, { ...client, timeoutMs: 100, load: () => new Promise(() => undefined) });
    const rejected = expect(pending).rejects.toThrow('授权超时');
    await vi.advanceTimersByTimeAsync(100);
    await rejected;
    expect(vi.getTimerCount()).toBe(0);
  });
  it('handles client cancellation without accepting an identity', async () => {
    await expect(requestDingTalkAuthCode(app, { ...client, load: async () => async () => { throw { errCode: -1 }; } })).rejects.toThrow('授权未完成');
  });
});
