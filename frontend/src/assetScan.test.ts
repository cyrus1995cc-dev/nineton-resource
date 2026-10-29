import { describe, expect, it } from 'vitest';
import { normalizeScanActions, visibleScanActions } from './assetScan';

describe('scan action preferences', () => {
  it('keeps the familiar default shortcuts and limits the bar to three plus More', () => {
    expect(normalizeScanActions(null)).toEqual(['维修', '交接', '报失']);
    expect(normalizeScanActions(['归还', '归还', 'invented', '交接', '维修', '调拨'])).toEqual(['归还', '交接', '维修']);
    expect(normalizeScanActions([])).toEqual([]);
  });
  it('never restores an action excluded by the current asset or account permissions', () => {
    expect(visibleScanActions(['调拨', '交接', '维修'], ['领用'])).toEqual([]);
    expect(visibleScanActions(undefined, ['归还', '维修'])).toEqual(['维修']);
    expect(visibleScanActions(['归还'], [])).toEqual([]);
  });
});
