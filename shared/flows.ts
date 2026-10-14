export const flowTypes = ['领用', '归还', '借出', '借出归还', '调拨', '交接', '维修', '报废', '报失'] as const;
export type FlowType = typeof flowTypes[number];
export type FlowStatus = 'pending' | 'completed' | 'rejected' | 'withdrawn';
export const flowStatusLabels: Record<FlowStatus, string> = { pending: '审批中', completed: '已完结', rejected: '已驳回', withdrawn: '已撤回' };
export const flowTitles: Record<FlowType, string> = { 领用: '资产领用', 归还: '资产归还', 借出: '资产借出', 借出归还: '借出归还', 调拨: '资产调拨', 交接: '资产交接', 维修: '故障报修', 报废: '资产报废', 报失: '资产报失' };
export type Flow = {
  id: number; document_no: string; type: FlowType; status: FlowStatus;
  applicant_id: number; applicant_name: string; applicant_department: string;
  to_assignee: string; to_department: string; to_location: string; note: string;
  current_step: number; created_at: string; completed_at: string | null; asset_count: number;
};
export type FlowItem = { id: number; asset_id: number; asset_code: string; asset_name: string; category_path: string; model: string; expected_status: string; expected_location: string; expected_assignee: string; expected_department: string };
export type FlowStep = { id: number; sequence: number; approver_id: number; approver_name: string; status: 'waiting' | 'pending' | 'approved' | 'rejected' | 'cancelled'; comment: string; acted_at: string | null };
export type FlowLog = { id: number; action: 'submitted' | 'approved' | 'rejected' | 'withdrawn' | 'completed'; actor_name: string; note: string; created_at: string };
export type FlowComment = { id: number; user_id: number; user_name: string; content: string; created_at: string };
export type FlowNotification = { id: number; kind: 'approval_pending' | 'completed' | 'rejected' | 'withdrawn'; recipient_name: string; status: 'pending' | 'sent' | 'failed' | 'cancelled'; created_at: string };
export type FlowDetail = { flow: Flow; items: FlowItem[]; steps: FlowStep[]; logs: FlowLog[]; comments: FlowComment[]; notifications: FlowNotification[] };
export type FlowInput = { type: FlowType; assetCodes: string[]; applicantDepartment: string; approverIds: number[]; toAssignee?: string; toDepartment?: string; toLocation?: string; note?: string; requestId: string };
export function availableFlowTypes(status: string): FlowType[] {
  if (status === '空闲') return ['领用', '借出', '调拨', '维修', '报废', '报失'];
  if (status === '在用') return ['归还', '交接', '调拨', '维修', '报废', '报失'];
  if (status === '借出') return ['借出归还', '交接', '调拨', '维修', '报废', '报失'];
  if (status === '维修') return ['调拨', '报废', '报失'];
  return [];
}
