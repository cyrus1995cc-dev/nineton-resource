import { randomUUID } from 'node:crypto';
import type { DatabaseSync } from 'node:sqlite';
import { parseQr } from '../../../frontend/shared/qr';
import { availableFlowTypes, type FlowType } from '../../../frontend/shared/flows';
import { assetEditFields, fieldProblem } from '../../../frontend/shared/assets';
import { AppError, integer } from '../errors';
import type { AuthUser } from '../auth';
import { assetScope } from '../access';

export type AssetEventInput = {
  type: string;
  operatorName?: string;
  toAssignee?: string;
  toDepartment?: string;
  toLocation?: string;
  note?: string;
  requestId?: string;
  flowId?: number;
  toEmployeeId?: number | null;
};

export type AssetUpdateInput = {
  category_level1?: string;
  category_path?: string;
  name?: string;
  location?: string;
  company?: string;
  model?: string;
  cpu?: string;
  discrete_gpu?: string;
  memory?: string;
  gpu_model?: string;
  serial_number?: string;
  remark?: string;
  photo_url?: string;
  assignee?: string;
  department?: string;
  received_at?: string;
  amount_taxed?: number;
  amount_untaxed?: number;
  tax_amount?: number;
  expectedVersion?: number;
  operatorName?: string;
  note?: string;
};

const editableFields = [
  'id', 'asset_code', 'qr_raw', 'qr_format', 'category_level1', 'category_path', 'created_at', 'name', 'status',
  'location', 'company', 'model', 'cpu', 'discrete_gpu', 'memory', 'gpu_model', 'serial_number', 'remark',
  'photo_url', 'assignee', 'department', 'received_at', 'amount_taxed', 'amount_untaxed', 'tax_amount',
  'source_updated_at', 'created_at_system', 'updated_at_system', 'version', 'assignee_employee_id',
];

const assetUpdateFields = [
  'category_level1', 'category_path', 'name', 'location', 'company', 'model', 'cpu', 'discrete_gpu',
  'memory', 'gpu_model', 'serial_number', 'remark', 'photo_url', 'assignee', 'department', 'received_at',
  'amount_taxed', 'amount_untaxed', 'tax_amount',
] as const;

function mapAsset(row: Record<string, unknown> | undefined) {
  if (!row) return null;
  return Object.fromEntries(editableFields.map((field) => [field, row[field] ?? (field === 'assignee_employee_id' ? null : '')]));
}

export type AssetFilters = { search?: string; status?: string; category?: string; location?: string; scope?: string };
export function assetQuery(params: AssetFilters, actor?: AuthUser) {
  const where: string[] = [];
  const values: (string | number)[] = [];
  const scope = params.scope === 'mine' ? { clause: 'assignee_employee_id = ?', values: [actor?.employee_id ?? -1] } : assetScope(actor, params.scope === 'available');
  where.push(scope.clause); values.push(...scope.values);
  if (params.search) {
    where.push('(asset_code LIKE ? OR name LIKE ? OR model LIKE ? OR serial_number LIKE ? OR assignee LIKE ? OR department LIKE ?)');
    const query = `%${params.search}%`;
    values.push(query, query, query, query, query, query);
  }
  if (params.status) { where.push('status = ?'); values.push(params.status); }
  if (params.category) { where.push('category_path = ?'); values.push(params.category); }
  if (params.location) { where.push('location = ?'); values.push(params.location); }
  const clause = where.length ? `WHERE ${where.join(' AND ')}` : '';
  return { clause, values };
}

export function listAssets(db: DatabaseSync, params: AssetFilters & { page?: number; pageSize?: number }, actor?: AuthUser) {
  const { clause, values } = assetQuery(params, actor);
  const page = integer(params.page, '页码', 1, 1000000);
  const pageSize = integer(params.pageSize, '每页条数', 20, 100);
  const total = Number((db.prepare(`SELECT COUNT(*) AS count FROM assets ${clause}`).get(...values) as { count: number }).count);
  const rows = db.prepare(`SELECT * FROM assets ${clause} ORDER BY updated_at_system DESC, id DESC LIMIT ? OFFSET ?`)
    .all(...values, pageSize, (page - 1) * pageSize) as Array<Record<string, unknown>>;
  return { items: rows.map(mapAsset), total, page, pageSize };
}

export function getAsset(db: DatabaseSync, assetCode: string): (Record<string, any> & { events: Array<Record<string, any>> }) | null {
  const row = db.prepare('SELECT * FROM assets WHERE asset_code = ?').get(assetCode) as Record<string, unknown> | undefined;
  if (!row) return null;
  const events = (db.prepare(`SELECT e.*, f.document_no FROM asset_events e LEFT JOIN asset_flows f ON f.id = e.flow_id
    WHERE e.asset_code = ? ORDER BY e.id DESC`).all(assetCode) as Array<Record<string, unknown>>)
    .map((event) => ({ ...event, changes: JSON.parse(String(event.changes_json ?? '[]')) }));
  return { ...mapAsset(row), events };
}

export function saveAssetQr(db: DatabaseSync, assetCode: string, raw: string) {
  if (typeof raw !== 'string' || raw.length > 4000) return { status: 'invalid' as const };
  const parsed = parseQr(raw);
  if (!parsed.assetCode || parsed.assetCode !== assetCode) return { status: 'invalid' as const };
  const existing = db.prepare('SELECT qr_raw, qr_format FROM assets WHERE asset_code = ?').get(assetCode) as { qr_raw: string | null; qr_format: string | null } | undefined;
  if (!existing) return { status: 'not_found' as const };
  // URLs printed on old labels are immutable. Different observations are aliases.
  if (existing.qr_raw) {
    if (existing.qr_raw !== parsed.raw) db.prepare('INSERT OR IGNORE INTO asset_qr_aliases (asset_id, raw) SELECT id, ? FROM assets WHERE asset_code = ?').run(parsed.raw, assetCode);
    return { status: 'saved' as const, qr_raw: existing.qr_raw, qr_format: existing.qr_format };
  }
  const result = db.prepare('UPDATE assets SET qr_raw = ?, qr_format = ?, updated_at_system = CURRENT_TIMESTAMP WHERE asset_code = ?')
    .run(parsed.raw, parsed.format, assetCode);
  if (!result.changes) return { status: 'not_found' as const };
  return { status: 'saved' as const, qr_raw: parsed.raw, qr_format: parsed.format };
}

export function updateAsset(db: DatabaseSync, assetCode: string, input: AssetUpdateInput) {
  const asset = db.prepare('SELECT * FROM assets WHERE asset_code = ?').get(assetCode) as Record<string, unknown> | undefined;
  if (!asset) return { status: 'not_found' as const };
  if (input.expectedVersion !== undefined && integer(input.expectedVersion, '资料版本') !== Number(asset.version)) throw new AppError(409, '资料已被其他人更新，请重新打开资产核对后保存');
  for (const field of assetEditFields) {
    if (input[field] === undefined) continue;
    const problem = fieldProblem(field, input[field]); if (problem) throw new AppError(400, problem);
  }

  const updates: string[] = [];
  const values: unknown[] = [];
  for (const field of assetUpdateFields) {
    if (input[field] === undefined || String(input[field]) === String(asset[field] ?? '')) continue;
    updates.push(`${field} = ?`);
    values.push(input[field]);
  }
  if (!updates.length) return { status: 'empty' as const };

  const nextAssignee = input.assignee ?? String(asset.assignee ?? '');
  const nextDepartment = input.department ?? String(asset.department ?? '');
  const nextLocation = input.location ?? String(asset.location ?? '');
  const changes = assetUpdateFields.filter((field) => input[field] !== undefined && String(input[field]) !== String(asset[field] ?? ''))
    .map((field) => ({ field, before: asset[field] ?? '', after: input[field] }));
  db.exec('BEGIN');
  try {
    values.push(assetCode, asset.version);
    const saved = db.prepare(`UPDATE assets SET ${updates.join(', ')}, updated_at_system = CURRENT_TIMESTAMP WHERE asset_code = ? AND version = ?`).run(...(values as any[]));
    if (!saved.changes) throw new AppError(409, '资料已变化，请重新打开资产核对后保存');
    const event = db.prepare(`INSERT INTO asset_events
      (asset_id, asset_code, type, operator_name, from_status, to_status, from_assignee, to_assignee,
       from_department, to_department, from_location, to_location, note, changes_json)
      VALUES (?, ?, '编辑', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
      .run(...([asset.id as number, assetCode, input.operatorName ?? '', asset.status as string, asset.status as string,
        asset.assignee ?? '', nextAssignee, asset.department ?? '', nextDepartment, asset.location ?? '', nextLocation,
        input.note ?? '', JSON.stringify(changes)] as any[]));
    db.exec('COMMIT');
    return { status: 'updated' as const, asset: getAsset(db, assetCode), event: db.prepare('SELECT * FROM asset_events WHERE id = ?').get(event.lastInsertRowid) };
  } catch (error) {
    db.exec('ROLLBACK');
    throw error;
  }
}

function eventTransition(asset: Record<string, unknown>, input: AssetEventInput) {
  const currentStatus = String(asset.status ?? '空闲');
  const type = input.type;
  const allowed: Record<string, { status: string; clearAssignee?: boolean }> = {
    领用: { status: '在用' },
    归还: { status: '空闲', clearAssignee: true },
    借出: { status: '借出' },
    借出归还: { status: '空闲', clearAssignee: true },
    维修: { status: '维修' },
    报废: { status: '报废' },
    报失: { status: '丢失' },
    交接: { status: currentStatus },
  };
  if (!availableFlowTypes(currentStatus).includes(type as FlowType)) throw new Error(`资产当前状态为${currentStatus}，不能${type}`);
  if (type === '调拨') return { status: currentStatus, clearAssignee: false };
  const transition = allowed[type];
  if (!transition) throw new Error(`不支持的资产操作：${type}`);
  return transition;
}

export function createAssetEvent(db: DatabaseSync, assetCode: string, input: AssetEventInput) {
  const asset = db.prepare('SELECT * FROM assets WHERE asset_code = ?').get(assetCode) as Record<string, unknown> | undefined;
  if (!asset) return { status: 'not_found' as const };
  const requestId = input.requestId || randomUUID();
  const replay = db.prepare('SELECT * FROM asset_events WHERE request_id = ?').get(requestId) as Record<string, unknown> | undefined;
  if (replay) {
    if (replay.asset_code !== assetCode || replay.type !== input.type) throw new Error('重复请求与原资产操作不一致');
    return { status: 'replayed' as const, event: replay, asset: getAsset(db, assetCode) };
  }
  const transition = eventTransition(asset, input);
  const nextStatus = transition.status;
  const nextAssignee = transition.clearAssignee ? '' : input.toAssignee ?? String(asset.assignee ?? '');
  const nextDepartment = transition.clearAssignee ? '' : input.toDepartment ?? String(asset.department ?? '');
  const nextLocation = input.toLocation ?? String(asset.location ?? '');

  const changes = [{ field: 'status', before: asset.status, after: nextStatus },
    { field: 'assignee', before: asset.assignee ?? '', after: nextAssignee },
    { field: 'department', before: asset.department ?? '', after: nextDepartment },
    { field: 'location', before: asset.location ?? '', after: nextLocation }].filter((c) => c.before !== c.after);
  db.exec('SAVEPOINT asset_event');
  try {
    const result = db.prepare(`INSERT INTO asset_events
      (asset_id, asset_code, type, operator_name, from_status, to_status, from_assignee, to_assignee,
       from_department, to_department, from_location, to_location, note, request_id, flow_id, changes_json)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
      .run(...([asset.id as number, assetCode, input.type, input.operatorName ?? '', asset.status as string, nextStatus, asset.assignee ?? '', nextAssignee,
        asset.department ?? '', nextDepartment, asset.location ?? '', nextLocation, input.note ?? '', requestId, input.flowId ?? null, JSON.stringify(changes)] as any[]));
    const employeeId = transition.clearAssignee ? null : input.toEmployeeId === undefined ? asset.assignee_employee_id ?? null : input.toEmployeeId;
    db.prepare(`UPDATE assets SET status = ?, assignee = ?, department = ?, location = ?, assignee_employee_id = ?, updated_at_system = CURRENT_TIMESTAMP WHERE asset_code = ?`)
      .run(nextStatus, nextAssignee, nextDepartment, nextLocation, employeeId as number | null, assetCode);
    db.exec('RELEASE asset_event');
    return { status: 'created' as const, event: db.prepare('SELECT * FROM asset_events WHERE id = ?').get(result.lastInsertRowid), asset: getAsset(db, assetCode) };
  } catch (error) {
    db.exec('ROLLBACK TO asset_event; RELEASE asset_event');
    throw error;
  }
}

export function getStats(db: DatabaseSync, actor?: AuthUser) {
  const scope = assetScope(actor);
  const query = (sql: string) => db.prepare(sql.replace('FROM assets', `FROM assets WHERE ${scope.clause}`));
  const total = Number((query('SELECT COUNT(*) AS count FROM assets').get(...scope.values) as { count: number }).count);
  const statusRows = query('SELECT status, COUNT(*) AS count FROM assets GROUP BY status ORDER BY status').all(...scope.values) as Array<{ status: string; count: number }>;
  const categoryRows = query('SELECT COALESCE(category_path, \'未分类\') AS name, COUNT(*) AS count FROM assets GROUP BY category_path ORDER BY count DESC').all(...scope.values);
  const locationRows = query('SELECT COALESCE(location, \'未设置\') AS name, COUNT(*) AS count FROM assets GROUP BY location ORDER BY count DESC').all(...scope.values);
  const companyRows = query('SELECT COALESCE(company, \'未设置\') AS name, COUNT(*) AS count FROM assets GROUP BY company ORDER BY count DESC').all(...scope.values);
  const amounts = query('SELECT COALESCE(SUM(amount_taxed), 0) AS taxed, COALESCE(SUM(amount_untaxed), 0) AS untaxed, COALESCE(SUM(tax_amount), 0) AS tax FROM assets').get(...scope.values) as { taxed: number; untaxed: number; tax: number };
  return {
    total,
    status: Object.fromEntries(statusRows.map((row) => [row.status, Number(row.count)])),
    categories: categoryRows,
    locations: locationRows,
    companies: companyRows,
    amounts: { taxed: Number(amounts.taxed), untaxed: Number(amounts.untaxed), tax: Number(amounts.tax) },
  };
}
