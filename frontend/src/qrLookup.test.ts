import { describe, expect, it } from 'vitest';
import { getAssetCodeFromScan } from './qrLookup';

describe('mobile QR lookup boundary', () => {
  it('turns the existing vendor URL into a local asset code without navigation', () => {
    expect(getAssetCodeFromScan('https://d.gongbeiyun.com/a/58b7d/57771/CSJ-0431')).toBe('CSJ-0431');
  });

  it('returns null for content that is not an asset identifier', () => {
    expect(getAssetCodeFromScan('https://example.com/help')).toBeNull();
  });

  it('accepts the monitor label code and vendor URLs with a query or trailing slash', () => {
    expect(getAssetCodeFromScan('XSQ-0318')).toBe('XSQ-0318');
    expect(getAssetCodeFromScan('https://d.gongbeiyun.com/a/company/asset/XSQ-0318/?from=dingtalk#detail')).toBe('XSQ-0318');
  });

  it('does not guess an asset for an opaque vendor identifier or a malformed URL', () => {
    expect(getAssetCodeFromScan('https://d.gongbeiyun.com/a/57771')).toBeNull();
    expect(getAssetCodeFromScan('https://d.gongbeiyun.com/a/%E0%A4%A')).toBeNull();
  });
});
