import { parseQr } from '../shared/qr';

export function getAssetCodeFromScan(raw: string): string | null {
  return parseQr(raw).assetCode;
}
