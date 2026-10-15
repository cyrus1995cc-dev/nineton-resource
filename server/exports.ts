import * as XLSX from 'xlsx';
import type { DatabaseSync } from 'node:sqlite';
import type { AuthUser } from './auth';
import { assetQuery, type AssetFilters } from './repositories/assets';
import { flowQuery, type FlowFilters } from './repositories/flows';
import { getInventoryTask } from './repositories/inventory';
import { AppError } from './errors';
import { flowStatusLabels, type FlowStatus } from '../shared/flows';

const maxRows = 50000;
type Row = Record<string, any>;
type Column = { field: string; title: string; width?: number; money?: boolean };
export const excelMime = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';
export function beijingTime(value: unknown = new Date()) {
  if (!value) return '';
  const raw = String(value);
  const date = value instanceof Date ? value : new Date(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(raw) ? `${raw.replace(' ', 'T')}Z` : raw);
  if (Number.isNaN(date.getTime())) return '';
  const parts = Object.fromEntries(new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).formatToParts(date).map((p) => [p.type, p.value]));
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`;
}
function checkSize(count: number) {
  if (count > maxRows) throw new AppError(400, `单次最多导出 ${maxRows} 行，请缩小筛选范围后重试`);
}
function append(book: XLSX.WorkBook, name: string, rows: Row[], columns: Column[]) {
  checkSize(rows.length);
  // AOA string values stay literal cells, including names/remarks that begin with '='.
  const sheet = XLSX.utils.aoa_to_sheet([columns.map((c) => c.title), ...rows.map((r) => columns.map((c) => r[c.field] ?? ''))]);
  sheet['!cols'] = columns.map((c) => ({ wch: c.width ?? 20 }));
  sheet['!autofilter'] = { ref: sheet['!ref']! };
  sheet['!rows'] = [{ hpt: 24 }];
  columns.forEach((c, index) => {
    if (c.money) for (let row = 1; row <= rows.length; row++) {
      const cell = sheet[XLSX.utils.encode_cell({ r: row, c: index })];
      if (cell?.t === 'n') cell.z = '#,##0.00';
    }
  });
  XLSX.utils.book_append_sheet(book, sheet, name);
}
const col = (field: string, title: string, width?: number): Column => ({ field, title, width });
function information(book: XLSX.WorkBook, actor: AuthUser, time: string, count: number, filters: Row, note: string) {
  append(book, '导出说明', [
    { key: '导出人', value: actor.display_name }, { key: '导出时间（北京时间）', value: time },
    { key: '主表记录数', value: count }, ...Object.entries(filters).map(([key, value]) => ({ key, value: value || '全部' })),
    { key: '说明', value: note },
  ], [col('key', '项目', 26), col('value', '内容', 100)]);
}
function finish(db: DatabaseSync, book: XLSX.WorkBook, actor: AuthUser, kind: string, time: string, counts: Row, filters: Row) {
  const buffer = XLSX.write(book, { type: 'buffer', bookType: 'xlsx', compression: true }) as Buffer;
  db.prepare('INSERT INTO audit_logs (actor_id, actor_name, action, target, details_json) VALUES (?, ?, ?, ?, ?)')
    .run(actor.id, actor.display_name, 'export', kind, JSON.stringify({ counts, filters, exportedAt: time }));
  return { buffer, filename: `${kind}-${time.replace(/[-:]/g, '').replace(' ', '-')}.xlsx` };
}
export function exportAssets(db: DatabaseSync, actor: AuthUser, filters: AssetFilters) {
  const query = assetQuery(filters, actor); const time = beijingTime();
  const rows = db.prepare(`SELECT * FROM assets ${query.clause} ORDER BY updated_at_system DESC, id DESC LIMIT ?`).all(...query.values, maxRows + 1) as Row[];
  checkSize(rows.length); const book = XLSX.utils.book_new();
  append(book, '资产清单', rows, [
    col('asset_code', '资产编码'), col('category_level1', '资产分类(一级)'), col('created_at', '创建时间', 24), col('category_path', '资产分类', 34),
    col('name', '资产名称', 32), col('status', '资产状态', 12), col('location', '所在位置'), col('company', '所属/承租公司', 36),
    col('model', '型号', 28), col('cpu', 'CPU'), col('discrete_gpu', '是否有独立显卡'), col('memory', '内存'), col('gpu_model', '显卡型号'),
    col('serial_number', '设备序列号', 26), col('remark', '备注', 48), col('photo_url', '资产照片', 40), col('assignee', '使用人'), col('department', '使用部门'),
    col('received_at', '领用日期', 24), { ...col('amount_taxed', '购置/租赁金额（含税）', 26), money: true },
    { ...col('amount_untaxed', '购置/租赁金额（不含税）', 26), money: true }, { ...col('tax_amount', '税额'), money: true }, col('qr_raw', '二维码原文', 60),
  ]);
  information(book, actor, time, rows.length, { 搜索: filters.search, 状态: filters.status, 分类: filters.category, 位置: filters.location, 查看范围: filters.scope === 'mine' ? '我的资产' : filters.scope === 'available' ? '可领用资产' : '当前账号可见资产' }, '按筛选条件导出全部可见记录，不受页面分页限制；二维码原文作为文本保留。已有资产归属与状态变更仍须审批。');
  return finish(db, book, actor, '资产清单', time, { assets: rows.length }, filters);
}
const stepLabels: Row = { waiting: '等待前序', pending: '待审批', approved: '已同意', rejected: '已驳回', cancelled: '已取消' };
const actionLabels: Row = { submitted: '提交申请', approved: '审批通过', rejected: '驳回申请', withdrawn: '撤回申请', completed: '流程完结' };
export function exportFlows(db: DatabaseSync, actor: AuthUser, filters: FlowFilters) {
  const { clause, values } = flowQuery(actor, filters); const time = beijingTime();
  const from = `FROM asset_flows f ${clause}`; const book = XLSX.utils.book_new();
  const rows = db.prepare(`SELECT f.*, (SELECT COUNT(*) FROM asset_flow_items WHERE flow_id = f.id) AS asset_count,
    (SELECT approver_name FROM asset_flow_steps WHERE flow_id = f.id AND status = 'pending') AS approver ${from} ORDER BY f.id DESC LIMIT ?`).all(...values, maxRows + 1) as Row[];
  checkSize(rows.length);
  const related = (table: string) => {
    const records = db.prepare(`SELECT r.*, f.document_no FROM ${table} r JOIN asset_flows f ON f.id = r.flow_id ${clause} ORDER BY f.id DESC, r.id LIMIT ?`).all(...values, maxRows + 1) as Row[];
    checkSize(records.length); return records;
  };
  const items = related('asset_flow_items'); const steps = related('asset_flow_steps'); const logs = related('asset_flow_logs');
  append(book, '流转单据', rows.map((r) => ({ ...r, status_label: flowStatusLabels[r.status as FlowStatus], submitted: beijingTime(r.created_at), ended: beijingTime(r.completed_at) })), [
    col('document_no', '单据编号', 26), col('type', '流转类型', 14), col('status_label', '审批状态', 14), col('applicant_name', '申请人'), col('applicant_department', '申请部门'),
    col('asset_count', '资产数量', 12), col('approver', '当前审批人'), col('to_assignee', '目标使用人'), col('to_department', '目标部门'), col('to_location', '目标位置'),
    col('note', '申请说明', 48), col('submitted', '申请时间（北京时间）', 26), col('ended', '结束时间（北京时间）', 26),
  ]);
  append(book, '流转资产明细', items, [col('document_no', '单据编号', 26), col('asset_code', '资产编码'), col('asset_name', '资产名称', 32), col('category_path', '资产分类', 32), col('model', '型号'), col('expected_status', '申请时状态'), col('expected_assignee', '申请时使用人'), col('expected_department', '申请时部门'), col('expected_location', '申请时位置')]);
  append(book, '审批步骤', steps.map((r) => ({ ...r, status_label: stepLabels[r.status], acted: beijingTime(r.acted_at) })), [col('document_no', '单据编号', 26), col('sequence', '审批顺序', 12), col('approver_name', '指定审批人'), col('status_label', '处理状态'), col('comment', '审批意见', 48), col('acted', '处理时间（北京时间）', 26)]);
  append(book, '处理记录', logs.map((r) => ({ ...r, action_label: actionLabels[r.action] ?? r.action, acted: beijingTime(r.created_at) })), [col('document_no', '单据编号', 26), col('action_label', '操作'), col('actor_name', '处理人'), col('note', '处理说明', 48), col('acted', '操作时间（北京时间）', 26)]);
  information(book, actor, time, rows.length, { 搜索: filters.search, 审批状态: flowStatusLabels[filters.status as FlowStatus] || '全部', 查看范围: filters.scope === 'todo' ? '我的待办' : filters.scope === 'mine' ? '我发起的' : '当前账号可见单据' }, '包含筛选后的全部可见单据及其资产快照、审批步骤、处理记录；资产信息为提交时快照。');
  return finish(db, book, actor, '流转记录', time, { flows: rows.length, items: items.length, steps: steps.length, logs: logs.length }, filters);
}
export function exportInventory(db: DatabaseSync, actor: AuthUser, id: number) {
  const detail = getInventoryTask(db, id); if (!detail) throw new AppError(404, '盘点任务不存在');
  const task = detail.task as Row; const records = detail.records as Row[]; checkSize(records.length);
  const time = beijingTime(); const book = XLSX.utils.book_new();
  const labels: Row = { pending: '待盘', found: '正常', different: '位置差异', missing: '未盘到' };
  const count = (result: string) => records.filter((r) => r.result === result).length;
  append(book, '任务概况', [{ name: task.name, status: task.status === 'completed' ? '已结束' : '进行中', location: task.location_filter || '全部位置', department: task.department_filter || '全部部门', operator: task.operator_name, total: task.total_count, checked: task.checked_count, pending: count('pending'), normal: count('found'), different: count('different'), missing: count('missing'), differences: task.difference_count, created: beijingTime(task.created_at), completed: beijingTime(task.completed_at) }], [
    col('name', '任务名称', 32), col('status', '任务状态'), col('location', '位置范围'), col('department', '部门范围'), col('operator', '创建人'), col('total', '应盘数量'), col('checked', '实际已盘'), col('pending', '待盘数量'), col('normal', '正常数量'), col('different', '位置差异'), col('missing', '未盘到'), col('differences', '差异总数'), col('created', '创建时间（北京时间）', 26), col('completed', '结束时间（北京时间）', 26),
  ]);
  const columns = [col('asset_code', '资产编码'), col('asset_name', '资产名称', 32), col('expected_status', '任务创建时状态'), col('expected_assignee', '任务创建时使用人'), col('expected_department', '任务创建时部门'), col('expected_location', '任务创建时位置'), col('result_label', '盘点结果'), col('actual_location', '实盘位置'), col('operator_name', '登记/结束人'), col('checked', '实盘时间（北京时间）', 26), col('note', '盘点备注', 48)];
  const data: Row[] = records.map((r) => ({ ...r, result_label: labels[r.result], checked: beijingTime(r.checked_at) }));
  append(book, '全部明细', data, columns);
  append(book, '差异清单', data.filter((r) => ['different', 'missing'].includes(r.result)), columns);
  information(book, actor, time, records.length, { 盘点任务编号: id, 任务名称: task.name }, '导出该任务全部记录，不受页面明细筛选影响。待盘不等于未盘到；结束时未扫描的资产记为未盘到，未盘到不代表台账已报失。位置差异不自动修改台账，请核实后提交调拨或交接审批。');
  return finish(db, book, actor, `盘点结果-${id}`, time, { records: records.length, differences: task.difference_count }, { taskId: id });
}
