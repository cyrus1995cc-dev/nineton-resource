import type { AuthUser } from './auth';
export function restrictedUser(actor?: AuthUser) { return actor?.role === 'employee' || actor?.role === 'approver'; }
export function assetScope(actor?: AuthUser, available = false, alias = ''): { clause: string; values: number[] } {
  if (!restrictedUser(actor)) return { clause: '1 = 1', values: [] };
  const a = `${alias || 'assets'}.`;
  if (available) return { clause: `${a}status = '空闲' AND ${a}assignee_employee_id IS NULL AND COALESCE(${a}assignee, '') = ''`, values: [] };
  return { clause: `(${a}assignee_employee_id = ? OR EXISTS (SELECT 1 FROM asset_flow_items si JOIN asset_flow_steps ss ON ss.flow_id = si.flow_id JOIN asset_flows sf ON sf.id = si.flow_id WHERE si.asset_id = ${a}id AND ss.approver_id = ? AND sf.status = 'pending'))`, values: [actor?.employee_id ?? -1, actor!.id] };
}
