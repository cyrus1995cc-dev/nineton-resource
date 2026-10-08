import { describe, expect, it } from 'vitest';
import { parseQr } from './qr';

describe('parseQr', () => {
  it('extracts the asset code from the existing Gongbeiyun URL', () => {
    const result = parseQr(' https://d.gongbeiyun.com/a/58b7d/57771/CSJ-0431 ');
    expect(result).toEqual({
      raw: 'https://d.gongbeiyun.com/a/58b7d/57771/CSJ-0431',
      assetCode: 'CSJ-0431',
      format: 'gongbeiyun_url',
    });
  });

  it('accepts a plain asset code without treating it as a URL', () => {
    expect(parseQr('CSJ-0431')).toMatchObject({ assetCode: 'CSJ-0431', format: 'asset_code' });
  });

  it('recognizes a native asset URL', () => {
    expect(parseQr('https://assets.example.com/assets/CSJ-0431')).toMatchObject({
      assetCode: 'CSJ-0431',
      format: 'native_url',
    });
  });

  it('keeps unknown QR content as raw text', () => {
    expect(parseQr('a random note')).toEqual({ raw: 'a random note', assetCode: null, format: 'unknown' });
  });
});
