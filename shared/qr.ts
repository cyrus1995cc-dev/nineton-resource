export type QrFormat = 'gongbeiyun_url' | 'native_url' | 'asset_code' | 'unknown';

export type ParsedQr = {
  raw: string;
  assetCode: string | null;
  format: QrFormat;
};

const assetCodePattern = /^[A-Z]{2,5}-\d{3,6}$/;

export function parseQr(input: string): ParsedQr {
  const raw = String(input ?? '').trim();
  if (!raw) return { raw, assetCode: null, format: 'unknown' };

  if (assetCodePattern.test(raw)) {
    return { raw, assetCode: raw, format: 'asset_code' };
  }

  try {
    const url = new URL(raw);
    const segments = url.pathname.split('/').filter(Boolean);
    const lastSegment = decodeURIComponent(segments.at(-1) ?? '');
    const assetCode = assetCodePattern.test(lastSegment) ? lastSegment : null;
    if (url.hostname === 'd.gongbeiyun.com' && segments[0] === 'a') {
      return { raw, assetCode, format: 'gongbeiyun_url' };
    }
    if (assetCode) return { raw, assetCode, format: 'native_url' };
  } catch {
    // A QR value can be arbitrary text. Keep it as an unknown string.
  }

  return { raw, assetCode: null, format: 'unknown' };
}
