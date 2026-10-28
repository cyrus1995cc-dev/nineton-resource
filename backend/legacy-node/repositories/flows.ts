import { createHash } from 'node:crypto';
import type { DatabaseSync } from 'node:sqlite';
import type { AuthUser } from '../auth';
import { availableFlowTypes, flowTypes, type Flow, type FlowDetail, type FlowInput, type FlowItem, type FlowStep } from '../../../frontend/shared/flows';
import { createAssetEvent } from './assets';
import { restrictedUser } from '../access';
import { integer } from '../errors';

export class FlowError extends Error {
  constructor(public status: number, message: string) { super(message); }
}
function fail(message: string, status = 409): never { throw new FlowError(status, message); }
function text(value: unknown, max = 200) {
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string' || value.length > max) fail(`请填写有效文本（最多 ${max} 字）`, 400);
  return value.trim();
}
function transaction<T>(db: DatabaseSync, action: () => T): T {
  db.exec('BEGIN IMMEDIATE');
  try { const result = action(); db.exec('COMMIT'); return result; }
  catch (error) { db.exec('ROLLBACK'); throw error; }
}
function log(db: DatabaseSync, id: number, action: string, actor: AuthUser, note = '') {
  db.prepare('INSERT INTO asset_flow_logs (flow_id, action, actor_id, actor_name, note) VALUES (?, ?, ?, ?, ?)')
    .run(id, action, actor.id, actor.display_name, note);
}
function notify(db: DatabaseSync, flow: Flow, kind: string, recipientId: number, name: string, step = 0) {
  const identity = db.prepare('SELECT e.id AS employeeId, e.corp_id AS corpId, e.dingtalk_user_id AS userId FROM users u LEFT JOIN employees e ON e.id = u.employee_id WHERE u.id = ?').get(recipientId);
  db.prepare(`INSERT INTO notification_outbox (flow_id, kind, recipient_id, recipient_name, dedupe_key, payload_json)
    VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(dedupe_key) DO NOTHING`).run(flow.id, kind, recipientId, name, `${flow.id}:${kind}:${step}:${recipientId}`,
    JSON.stringify({ flowId: flow.id, documentNo: flow.document_no, type: flow.type, applicant: flow.applicant_name, path: `/?flow=${flow.id}`, recipient: { localUserId: recipientId, ...identity } }));
}
function cancelNotifications(db: DatabaseSync, id: number) {
  db.prepare("UPDATE notification_outbox SET status = 'cancelled' WHERE flow_id = ? AND kind = 'approval_pending' AND status IN ('pending', 'failed')").run(id);
}
export function flowOptions(db: DatabaseSync) {
  return db.prepare("SELECT u.id, u.username, u.display_name, u.role FROM users u LEFT JOIN employees e ON e.id = u.employee_id WHERE u.active = 1 AND (e.id IS NULL OR (e.active = 1 AND e.missing_since IS NULL)) AND u.role IN ('admin', 'asset_manager', 'approver') ORDER BY u.id").all();
}
export function canViewFlow(db: DatabaseSync, id: number, actor: AuthUser) {
  if (!restrictedUser(actor)) return true;
  return Boolean(db.prepare('SELECT f.id FROM asset_flows f WHERE f.id = ? AND (f.applicant_id = ? OR EXISTS (SELECT 1 FROM asset_flow_steps s WHERE s.flow_id = f.id AND s.approver_id = ?) OR f.to_employee_id = ?)').get(id, actor.id, actor.id, actor.employee_id ?? -1));
}
export function getFlow(db: DatabaseSync, id: number): FlowDetail {
  const flow = db.prepare(`SELECT f.*, (SELECT COUNT(*) FROM asset_flow_items WHERE flow_id = f.id) AS asset_count FROM asset_flows f WHERE id = ?`).get(id) as Flow | undefined;
  if (!flow) fail('单据不存在', 404);
  return {
    flow,
    items: db.prepare('SELECT * FROM asset_flow_items WHERE flow_id = ? ORDER BY id').all(id) as FlowItem[],
    steps: db.prepare('SELECT * FROM asset_flow_steps WHERE flow_id = ? ORDER BY sequence').all(id) as FlowStep[],
    logs: db.prepare('SELECT id, action, actor_name, note, created_at FROM asset_flow_logs WHERE flow_id = ? ORDER BY id').all(id) as FlowDetail['logs'],
    comments: db.prepare('SELECT * FROM asset_flow_comments WHERE flow_id = ? ORDER BY id').all(id) as FlowDetail['comments'],
    notifications: db.prepare('SELECT id, kind, recipient_name, status, created_at FROM notification_outbox WHERE flow_id = ? ORDER BY id').all(id) as FlowDetail['notifications'],
  };
}
export type FlowFilters = { scope?: string; status?: string; search?: string };
export function flowQuery(actor: AuthUser, params: FlowFilters) {
  const where: string[] = []; const values: (string | number)[] = [];
  if (restrictedUser(actor)) { where.push('(f.applicant_id = ? OR EXISTS (SELECT 1 FROM asset_flow_steps s WHERE s.flow_id = f.id AND s.approver_id = ?) OR f.to_employee_id = ?)'); values.push(actor.id, actor.id, actor.employee_id ?? -1); }
  if (params.scope === 'todo') {
    where.push("f.status = 'pending' AND EXISTS (SELECT 1 FROM asset_flow_steps s WHERE s.flow_id = f.id AND s.approver_id = ? AND s.status = 'pending')"); values.push(actor.id);
  } else if (params.scope === 'mine') { where.push('f.applicant_id = ?'); values.push(actor.id); }
  if (params.status) { where.push('f.status = ?'); values.push(params.status); }
  if (params.search) {
    where.push('(f.document_no LIKE ? OR f.applicant_name LIKE ? OR EXISTS (SELECT 1 FROM asset_flow_items i WHERE i.flow_id = f.id AND (i.asset_code LIKE ? OR i.asset_name LIKE ?)))');
    values.push(...Array(4).fill(`%${params.search.slice(0, 200)}%`));
  }
  const clause = where.length ? `WHERE ${where.join(' AND ')}` : '';
  return { clause, values };
}
export function listFlows(db: DatabaseSync, actor: AuthUser, params: FlowFilters & { page?: number }) {
  const { clause, values } = flowQuery(actor, params);
  const page = integer(params.page, '页码', 1, 1000000); const pageSize = 20;
  const total = Number((db.prepare(`SELECT COUNT(*) AS n FROM asset_flows f ${clause}`).get(...values) as { n: number }).n);
  const items = db.prepare(`SELECT f.*, (SELECT COUNT(*) FROM asset_flow_items WHERE flow_id = f.id) AS asset_count
    FROM asset_flows f ${clause} ORDER BY id DESC LIMIT ? OFFSET ?`).all(...values, pageSize, (page - 1) * pageSize) as Flow[];
  return { items, total, page, pageSize };
}
export function createFlow(db: DatabaseSync, actor: AuthUser, raw: FlowInput) {
  if (restrictedUser(actor) && (!actor.employee_id || !db.prepare('SELECT id FROM employees WHERE id = ? AND active = 1 AND missing_since IS NULL').get(actor.employee_id))) fail('请管理员先关联有效员工身份', 403);
  if (restrictedUser(actor) && !['领用', '归还', '交接', '维修', '报失'].includes(raw?.type)) fail('员工不能发起此类管理操作', 403);
  if (!raw || !flowTypes.includes(raw.type)) fail('请选择有效的流转类型', 400);
  if (!Array.isArray(raw.assetCodes) || !raw.assetCodes.length || raw.assetCodes.length > 100) fail('每张单据需要 1 至 100 件资产', 400);
  const codes = raw.assetCodes.map((value) => text(value, 100));
  if (codes.some((c) => !c) || new Set(codes).size !== codes.length) fail('资产编码为空或重复', 400);
  if (!Array.isArray(raw.approverIds) || !raw.approverIds.length || raw.approverIds.length > 10 || raw.approverIds.some((id) => !Number.isSafeInteger(id))) fail('请指定 1 至 10 位审批人', 400);
  if (new Set(raw.approverIds).size !== raw.approverIds.length) fail('审批人不能重复', 400);
  if (raw.approverIds.includes(actor.id)) fail('申请人不能审批自己的申请，请选择其他审批账号', 400);
  const input = { type: raw.type, assetCodes: codes, applicantDepartment: text(raw.applicantDepartment), approverIds: raw.approverIds,
    toAssignee: text(raw.toAssignee), toDepartment: text(raw.toDepartment), toLocation: text(raw.toLocation), note: text(raw.note, 2000), requestId: text(raw.requestId, 100) };
  if (!['领用', '借出', '交接'].includes(input.type)) input.toAssignee = '';
  if (!['领用', '借出', '交接', '调拨'].includes(input.type)) input.toDepartment = '';
  if (input.type === '调拨' && !input.toLocation && !input.toDepartment) fail('调拨需要填写目标位置或部门', 400);
  if (['维修', '报失', '报废'].includes(input.type) && !input.note) fail('请填写申请原因或故障说明', 400);
  let employeeId: number | null = null;
  if (raw.toEmployeeId != null && ['领用', '借出', '交接'].includes(input.type)) {
    if (!Number.isSafeInteger(raw.toEmployeeId)) fail('接收员工编号无效', 400);
    const e = db.prepare('SELECT * FROM employees WHERE id = ? AND active = 1 AND missing_since IS NULL').get(raw.toEmployeeId) as Record<string, any> | undefined;
    if (!e) fail('接收员工不存在或已停用', 400);
    employeeId = Number(e.id); input.toAssignee = e.name; input.toDepartment = e.department ?? '';
  }
  if (restrictedUser(actor)) {
    input.applicantDepartment = actor.department ?? '';
    if (input.type === '领用' && employeeId !== actor.employee_id) fail('员工领用的接收人必须为本人', 403);
  }
  if (['领用', '借出', '交接'].includes(input.type) && !employeeId && db.prepare('SELECT id FROM employees WHERE active = 1 LIMIT 1').get()) fail('请从通讯录选择接收员工', 400);
  if (!input.applicantDepartment || !input.requestId) fail('请填写申请部门和请求标识', 400);
  if (['领用', '借出', '交接'].includes(input.type) && (!input.toAssignee || !input.toDepartment)) fail('请填写目标使用人和使用部门', 400);
  const hash = createHash('sha256').update(JSON.stringify({ ...input, toEmployeeId: employeeId, applicantId: actor.id })).digest('hex');
  return transaction(db, () => {
    const replay = db.prepare('SELECT id, request_hash FROM asset_flows WHERE request_id = ?').get(input.requestId) as { id: number; request_hash: string } | undefined;
    if (replay) {
      if (replay.request_hash !== hash) fail('请求标识已用于另一份申请，请重新提交');
      return { ...getFlow(db, replay.id), replayed: true };
    }
    const approvers = input.approverIds.map((id) => {
      const row = flowOptions(db).find((user: any) => user.id === id) as { id: number; display_name: string } | undefined;
      if (!row) fail('审批账号不存在、已停用或没有审批权限', 400); return row;
    });
    const assets = codes.map((code) => {
      const asset = db.prepare('SELECT * FROM assets WHERE asset_code = ?').get(code) as Record<string, any> | undefined;
      if (!asset) fail(`资产 ${code} 不存在`, 404);
      if (restrictedUser(actor) && (input.type === '领用' ? asset.status !== '空闲' || asset.assignee_employee_id != null || Boolean(asset.assignee) : asset.assignee_employee_id !== actor.employee_id)) fail(`无权为 ${code} 发起该申请`, 403);
      if (!availableFlowTypes(asset.status).includes(input.type)) fail(`${code} 当前为${asset.status}，不能${input.type}`);
      const pending = db.prepare("SELECT f.document_no FROM asset_flow_items i JOIN asset_flows f ON f.id = i.flow_id WHERE i.asset_id = ? AND f.status = 'pending'").get(asset.id) as { document_no: string } | undefined;
      if (pending) fail(`${code} 已在审批单 ${pending.document_no} 中，请先处理该单据`);
      return asset;
    });
    // Generate the public number from the actual inserted ID, under the same write transaction.
    const inserted = db.prepare(`INSERT INTO asset_flows (document_no, type, applicant_id, applicant_name, applicant_department, to_assignee, to_department, to_location, note, request_id, request_hash)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`).run(`pending:${input.requestId}`, input.type, actor.id, actor.display_name, input.applicantDepartment, input.toAssignee, input.toDepartment, input.toLocation, input.note, input.requestId, hash);
    const id = Number(inserted.lastInsertRowid);
    db.prepare('UPDATE asset_flows SET to_employee_id = ? WHERE id = ?').run(employeeId, id);
    const prefix: Record<string, string> = { 领用: 'ZCLY', 归还: 'ZCTH', 借出: 'ZCJC', 借出归还: 'ZCJH', 调拨: 'ZCDB', 交接: 'ZCJJ', 维修: 'ZCWX', 报废: 'ZCBF', 报失: 'ZCBS' };
    const day = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date()).replaceAll('-', '');
    db.prepare('UPDATE asset_flows SET document_no = ? WHERE id = ?').run(`${prefix[input.type]}${day}${String(id).padStart(5, '0')}`, id);
    for (const asset of assets) db.prepare(`INSERT INTO asset_flow_items (flow_id, asset_id, asset_code, asset_name, category_path, model, expected_status, expected_location, expected_assignee, expected_department)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`).run(id, asset.id, asset.asset_code, asset.name ?? '', asset.category_path ?? '', asset.model ?? '', asset.status, asset.location ?? '', asset.assignee ?? '', asset.department ?? '');
    for (const asset of assets) db.prepare('UPDATE asset_flow_items SET expected_employee_id = ? WHERE flow_id = ? AND asset_id = ?').run(asset.assignee_employee_id ?? null, id, asset.id);
    approvers.forEach((a, index) => db.prepare('INSERT INTO asset_flow_steps (flow_id, sequence, approver_id, approver_name, status) VALUES (?, ?, ?, ?, ?)').run(id, index + 1, a.id, a.display_name, index ? 'waiting' : 'pending'));
    log(db, id, 'submitted', actor, input.note);
    const detail = getFlow(db, id); notify(db, detail.flow, 'approval_pending', approvers[0].id, approvers[0].display_name, 1);
    return getFlow(db, id);
  });
}
export function actOnFlow(db: DatabaseSync, id: number, actor: AuthUser, action: 'approve' | 'reject' | 'withdraw', rawComment: unknown) {
  const comment = text(rawComment, 2000);
  return transaction(db, () => {
    const detail = getFlow(db, id); const { flow, steps, items } = detail;
    if (action === 'withdraw') {
      if (flow.applicant_id !== actor.id) fail('只有申请人可以撤回单据', 403);
      if (flow.status === 'withdrawn') return detail;
    } else {
      if (!steps.some((s) => s.approver_id === actor.id)) fail('你不是该单据的指定审批人', 403);
      const own = steps.find((s) => s.approver_id === actor.id)!;
      if (action === 'approve' && own.status === 'approved') return detail;
      if (action === 'reject' && own.status === 'rejected') return detail;
    }
    if (flow.status !== 'pending') fail('该单据已结束，不能继续处理');
    cancelNotifications(db, id);
    if (action === 'withdraw') {
      db.prepare("UPDATE asset_flows SET status = 'withdrawn', completed_at = CURRENT_TIMESTAMP WHERE id = ?").run(id);
      db.prepare("UPDATE asset_flow_steps SET status = 'cancelled' WHERE flow_id = ? AND status IN ('pending', 'waiting')").run(id);
      log(db, id, 'withdrawn', actor, comment); notify(db, flow, 'withdrawn', flow.applicant_id, flow.applicant_name);
      return getFlow(db, id);
    }
    const step = steps.find((s) => s.sequence === flow.current_step && s.status === 'pending');
    if (!step || step.approver_id !== actor.id) fail('请等待前一位审批人处理，只有当前指定审批人可以审批', 403);
    if (action === 'reject' && !comment) fail('请填写驳回原因', 400);
    db.prepare('UPDATE asset_flow_steps SET status = ?, comment = ?, acted_at = CURRENT_TIMESTAMP WHERE id = ?').run(action === 'approve' ? 'approved' : 'rejected', comment, step.id);
    log(db, id, action === 'approve' ? 'approved' : 'rejected', actor, comment);
    if (action === 'reject') {
      db.prepare("UPDATE asset_flows SET status = 'rejected', completed_at = CURRENT_TIMESTAMP WHERE id = ?").run(id);
      db.prepare("UPDATE asset_flow_steps SET status = 'cancelled' WHERE flow_id = ? AND status = 'waiting'").run(id);
      notify(db, flow, 'rejected', flow.applicant_id, flow.applicant_name);
    } else {
      const next = steps.find((s) => s.sequence === step.sequence + 1);
      if (next) {
        db.prepare("UPDATE asset_flow_steps SET status = 'pending' WHERE id = ?").run(next.id);
        db.prepare('UPDATE asset_flows SET current_step = ? WHERE id = ?').run(next.sequence, id);
        notify(db, flow, 'approval_pending', next.approver_id, next.approver_name, next.sequence);
      } else {
        for (const item of items) {
          const asset = db.prepare('SELECT * FROM assets WHERE id = ?').get(item.asset_id) as Record<string, unknown> | undefined;
          if (!asset || ['status', 'location', 'assignee', 'department'].some((f) => String(asset[f] ?? '') !== String(item[`expected_${f}` as keyof FlowItem]))) fail(`${item.asset_code} 台账已变化，请撤回后重新申请，避免覆盖最新信息`);
          if ((asset.assignee_employee_id ?? null) !== ((item as any).expected_employee_id ?? null)) fail(`${item.asset_code} 人员关联已变化，请撤回重填`);
          const toEmployeeId = (flow as any).to_employee_id as number | null;
          if (toEmployeeId && !db.prepare('SELECT id FROM employees WHERE id = ? AND active = 1 AND missing_since IS NULL').get(toEmployeeId)) fail('接收员工已停用或不在授权范围，请撤回重填');
          createAssetEvent(db, item.asset_code, { type: flow.type, operatorName: actor.display_name, toAssignee: flow.to_assignee || undefined,
            toDepartment: flow.to_department || undefined, toEmployeeId: toEmployeeId || undefined, toLocation: flow.to_location || undefined, note: flow.note, requestId: `flow:${id}:${item.asset_id}`, flowId: id });
        }
        db.prepare("UPDATE asset_flows SET status = 'completed', completed_at = CURRENT_TIMESTAMP WHERE id = ?").run(id);
        log(db, id, 'completed', actor, '全部审批通过，资产台账已更新'); notify(db, flow, 'completed', flow.applicant_id, flow.applicant_name);
      }
    }
    return getFlow(db, id);
  });
}
export function commentOnFlow(db: DatabaseSync, id: number, actor: AuthUser, raw: unknown) {
  const content = text(raw, 2000); if (!content) fail('评论不能为空', 400); getFlow(db, id);
  db.prepare('INSERT INTO asset_flow_comments (flow_id, user_id, user_name, content) VALUES (?, ?, ?, ?)').run(id, actor.id, actor.display_name, content);
  return getFlow(db, id);
}
