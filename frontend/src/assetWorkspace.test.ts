import { describe, expect, it } from 'vitest';
import { allowedAssetFlows, assetQuery, blankAssetFilters, normalizeColumns, pageNumbers } from './assetWorkspace';
import type { Asset, AuthUser } from './types';

const asset = (status: string, owner: number | null = null, assignee = '') => ({ status, assignee_employee_id: owner, assignee }) as Asset;
const admin: AuthUser = { id: 1, username: 'admin', display_name: '管理', role: 'admin' };
const employee: AuthUser = { ...admin, role: 'employee', employee_id: 8 };

describe('asset workspace permissions and bulk operations', () => {
  it('only offers actions supported by every selected asset', () => {
    expect(allowedAssetFlows([asset('空闲'), asset('在用')], admin)).toEqual(['调拨', '维修', '报废', '报失']);
    expect(allowedAssetFlows([asset('空闲'), asset('报废')], admin)).toEqual([]);
    expect(allowedAssetFlows([], admin)).toEqual([]);
    expect(allowedAssetFlows([asset('空闲')], { ...admin, role: 'viewer' })).toEqual([]);
  });
  it('requires a linked employee identity and exact ownership', () => {
    expect(allowedAssetFlows([asset('空闲')], employee)).toEqual([]);
    expect(allowedAssetFlows([asset('在用', 8, '本人')], employee)).toEqual(['归还', '交接', '维修', '报失']);
    expect(allowedAssetFlows([asset('在用', 9, '同名')], employee)).toEqual([]);
    expect(allowedAssetFlows([asset('在用')], { ...employee, employee_id: null })).toEqual([]);
    expect(allowedAssetFlows([asset('空闲')], { ...employee, employee_id: undefined })).toEqual([]);
    expect(allowedAssetFlows([asset('空闲', 8, '本人')], employee)).toEqual(['维修', '报失']);
    expect(allowedAssetFlows([asset('空闲', null, '未绑定的旧人员')], employee)).toEqual([]);
    expect(allowedAssetFlows([asset('在用', 8), asset('在用', 9)], employee)).toEqual([]);
    expect(allowedAssetFlows([asset('在用', 8)], { ...employee, role: 'approver' })).toEqual(['归还', '交接', '维修', '报失']);
  });
});

describe('asset workspace display and query preferences', () => {
  it('keeps the fixed asset code and discards obsolete or duplicate column keys', () => {
    expect(normalizeColumns(['name', 'name', 'unknown', null, 'status'])).toEqual(['asset_code', 'name', 'status']);
    expect(normalizeColumns([])).toEqual(['asset_code']);
    expect(normalizeColumns(null)).toContain('company');
  });
  it('encodes the same filters and ordering for an export', () => {
    const q = assetQuery({ ...blankAssetFilters(), search: 'A&B+空格', searchField: 'company', company: '集团', department: '研发' }, 'mine', 'asset_code', 'asc');
    expect(new URLSearchParams(q.toString()).get('search')).toBe('A&B+空格');
    expect(Object.fromEntries(q)).toEqual({ scope: 'mine', sort: 'asset_code', direction: 'asc', search: 'A&B+空格', searchField: 'company', company: '集团', department: '研发' });
    expect(assetQuery(blankAssetFilters(), '').has('searchField')).toBe(false);
  });
  it('keeps nearby pages and the first and last pages available', () => {
    expect(pageNumbers(1, 1)).toEqual([1]);
    expect(pageNumbers(1, 36)).toEqual([1, 2, '…', 36]);
    expect(pageNumbers(18, 36)).toEqual([1, '…', 17, 18, 19, '…', 36]);
    expect(pageNumbers(36, 36)).toEqual([1, '…', 35, 36]);
  });
});
