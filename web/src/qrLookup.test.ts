import { describe, expect, it } from 'vitest';
import { getAssetCodeFromScan } from './qrLookup';

describe('mobile QR lookup boundary', () => {
  it('turns the existing vendor URL into a local asset code without navigation', () => {
    expect(getAssetCodeFromScan('https://d.gongbeiyun.com/a/58b7d/57771/CSJ-0431')).toBe('CSJ-0431');
  });

  it('returns null for content that is not an asset identifier', () => {
    expect(getAssetCodeFromScan('https://example.com/help')).toBeNull();
  });
});
