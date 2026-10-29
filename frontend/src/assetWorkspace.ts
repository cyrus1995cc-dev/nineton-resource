import type { Asset, AuthUser } from './types';
import { availableFlowTypes, flowTypes, type FlowType } from '../shared/flows';

export type AssetFilters = { search: string; searchField: string; status: string; location: string; company: string; department: string; category: string };
export type AssetOptions = { total: number; statuses: Array<{ name: string; count: number }>; locations: Array<{ name: string; count: number }>; companies: Array<{ name: string; count: number }>; departments: Array<{ name: string; count: number }>; categories: Array<{ name: string; count: number }> };
export type AssetFlowDraft = { assets: Asset[]; type: FlowType; nonce: number };
export const blankAssetFilters = (): AssetFilters => ({ search: '', searchField: 'all', status: '', location: '', company: '', department: '', category: '' });
export const assetColumns = [
  { key: 'asset_code', label: '资产编码', width: 148, fixed: true, sortable: true, default: true },
  { key: 'name', label: '资产名称', width: 168, sortable: true, default: true },
  { key: 'status', label: '资产状态', width: 100, sortable: true, default: true },
  { key: 'category_path', label: '资产分类', width: 180, sortable: true, default: true },
  { key: 'company', label: '所属公司', width: 200, sortable: true, default: true },
  { key: 'location', label: '所在位置', width: 112, sortable: true, default: true },
  { key: 'assignee', label: '使用人', width: 112, sortable: true, default: true },
  { key: 'department', label: '使用部门', width: 180, sortable: true, default: true },
  { key: 'model', label: '型号', width: 164, sortable: true, default: true },
  { key: 'serial_number', label: '设备序列号', width: 180, default: false },
  { key: 'cpu', label: 'CPU', width: 140, default: false },
  { key: 'memory', label: '内存', width: 100, default: false },
  { key: 'gpu_model', label: '显卡型号', width: 160, default: false },
  { key: 'discrete_gpu', label: '独立显卡', width: 110, default: false },
  { key: 'created_at', label: '创建时间', width: 170, sortable: true, default: false },
  { key: 'received_at', label: '领用日期', width: 140, sortable: true, default: false },
  { key: 'remark', label: '备注', width: 240, default: false },
] as const;
export type AssetColumn = typeof assetColumns[number]['key'];
export function assetDepartmentLabel(asset: Pick<Asset, 'department' | 'company'>): string {
  const levels = (asset.department || '').split(/[/／]/).map(level => level.trim()).filter(Boolean);
  while (levels.length && (levels[0] === asset.company?.trim() || /(?:公司|集团)$/.test(levels[0]!))) levels.shift();
  return levels[0] || '';
}
export function normalizeColumns(raw: unknown): AssetColumn[] {
  const allowed = new Set<string>(assetColumns.map(c => c.key));
  if (!Array.isArray(raw)) return assetColumns.filter(c => c.default).map(c => c.key);
  return [...new Set<AssetColumn>(['asset_code', ...raw.filter((k): k is AssetColumn => typeof k === 'string' && allowed.has(k))])];
}
export function assetQuery(filters: AssetFilters, scope: string, sort = 'updated_at_system', direction = 'desc') {
  const query = new URLSearchParams({ scope, sort, direction });
  for (const [key, value] of Object.entries(filters)) if (value && (key !== 'searchField' || filters.search)) query.set(key, value);
  return query;
}
export function allowedAssetFlows(assets: Asset[], user: AuthUser): FlowType[] {
  if (!assets.length || user.role === 'viewer') return [];
  const limited = ['employee', 'approver'].includes(user.role);
  if (limited && !user.employee_id) return [];
  return flowTypes.filter(type => (!limited || ['归还', '交接', '维修', '报失'].includes(type)) && assets.every(a => {
    if (!availableFlowTypes(a.status).includes(type)) return false;
    if (!limited) return true;
    return a.assignee_employee_id === user.employee_id;
  }));
}
export function pageNumbers(page: number, totalPages: number): Array<number | '…'> {
  const pages = [...new Set([1, totalPages, page - 1, page, page + 1])].filter(n => n >= 1 && n <= totalPages).sort((a, b) => a - b);
  const result: Array<number | '…'> = [];
  for (const n of pages) { if (typeof result.at(-1) === 'number' && n - (result.at(-1) as number) > 1) result.push('…'); result.push(n); }
  return result;
}
