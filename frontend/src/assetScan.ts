import type { FlowType } from '../shared/flows';

export const defaultScanActions: FlowType[] = ['维修', '交接', '报失'];
export const scanActionLabels: Record<FlowType, string> = {
  领用: '资产领用', 归还: '资产退还', 借出: '资产借出', 借出归还: '借出归还',
  调拨: '资产调拨', 交接: '交接他人', 维修: '故障报修', 报废: '资产报废', 报失: '资产报失',
};
export const scanActionIcons: Record<FlowType, { name: string; tone: string }> = {
  领用: { name: 'box', tone: 'blue' }, 归还: { name: 'return', tone: 'coral' },
  借出: { name: 'upload', tone: 'orange' }, 借出归还: { name: 'return', tone: 'coral' },
  调拨: { name: 'flow', tone: 'purple' }, 交接: { name: 'handover', tone: 'orange' },
  维修: { name: 'repair', tone: 'purple' }, 报废: { name: 'dispose', tone: 'blue' },
  报失: { name: 'search', tone: 'blue' },
};

export function normalizeScanActions(value: unknown): FlowType[] {
  if (!Array.isArray(value)) return [...defaultScanActions];
  return [...new Set(value.filter((type): type is FlowType => typeof type === 'string' && Object.hasOwn(scanActionLabels, type)))].slice(0, 3);
}

export function visibleScanActions(preferences: unknown, allowed: FlowType[]): FlowType[] {
  return normalizeScanActions(preferences).filter(type => allowed.includes(type));
}
