import { randomUUID } from 'node:crypto';
import type { DatabaseSync } from 'node:sqlite';
import { parseQr } from '../../shared/qr';

export type AssetEventInput = {
  type: string;
  operatorName?: string;
  toAssignee?: string;
  toDepartment?: string;
  toLocation?: string;
  note?: string;
  requestId?: string;
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
  operatorName?: string;
  note?: string;
};

const editableFields = [
  'id', 'asset_code', 'qr_raw', 'qr_format', 'category_level1', 'category_path', 'created_at', 'name', 'status',
  'location', 'company', 'model', 'cpu', 'discrete_gpu', 'memory', 'gpu_model', 'serial_number', 'remark',
  'photo_url', 'assignee', 'department', 'received_at', 'amount_taxed', 'amount_untaxed', 'tax_amount',
  'source_updated_at', 'created_at_system', 'updated_at_system',
];

const assetUpdateFields = [
  'category_level1', 'category_path', 'name', 'location', 'company', 'model', 'cpu', 'discrete_gpu',
  'memory', 'gpu_model', 'serial_number', 'remark', 'photo_url', 'assignee', 'department', 'received_at',
  'amount_taxed', 'amount_untaxed', 'tax_amount',
] as const;

function mapAsset(row: Record<string, unknown> | undefined) {
  if (!row) return null;
  return Object.fromEntries(editableFields.map((field) => [field, row[field] ?? '']));
}

export function listAssets(db: DatabaseSync, params: { search?: string; status?: string; category?: string; location?: string; page?: number; pageSize?: number }) {
  const where: string[] = [];
  const values: string[] = [];
  if (params.search) {
    where.push('(asset_code LIKE ? OR name LIKE ? OR model LIKE ? OR serial_number LIKE ?)');
    const query = `%${params.search}%`;
    values.push(query, query, query, query);
  }
  if (params.status) { where.push('status = ?'); values.push(params.status); }
  if (params.category) { where.push('category_path = ?'); values.push(params.category); }
  if (params.location) { where.push('location = ?'); values.push(params.location); }
  const clause = where.length ? `WHERE ${where.join(' AND ')}` : '';
  const page = Math.max(1, Number(params.page ?? 1));
  const pageSize = Math.min(100, Math.max(1, Number(params.pageSize ?? 20)));
  const total = Number((db.prepare(`SELECT COUNT(*) AS count FROM assets ${clause}`).get(...values) as { count: number }).count);
  const rows = db.prepare(`SELECT * FROM assets ${clause} ORDER BY updated_at_system DESC, id DESC LIMIT ? OFFSET ?`)
    .all(...values, pageSize, (page - 1) * pageSize) as Array<Record<string, unknown>>;
  return { items: rows.map(mapAsset), total, page, pageSize };
}

export function getAsset(db: DatabaseSync, assetCode: string) {
  const row = db.prepare('SELECT * FROM assets WHERE asset_code = ?').get(assetCode) as Record<string, unknown> | undefined;
  if (!row) return null;
  const events = db.prepare('SELECT * FROM asset_events WHERE asset_code = ? ORDER BY id DESC').all(assetCode);
  return { ...mapAsset(row), events };
}

export function saveAssetQr(db: DatabaseSync, assetCode: string, raw: string) {
  const parsed = parseQr(raw);
  if (!parsed.assetCode || parsed.assetCode !== assetCode) return { status: 'invalid' as const };
  const result = db.prepare('UPDATE assets SET qr_raw = ?, qr_format = ?, updated_at_system = CURRENT_TIMESTAMP WHERE asset_code = ?')
    .run(parsed.raw, parsed.format, assetCode);
  if (!result.changes) return { status: 'not_found' as const };
  return { status: 'saved' as const, qr_raw: parsed.raw, qr_format: parsed.format };
}

export function updateAsset(db: DatabaseSync, assetCode: string, input: AssetUpdateInput) {
  const asset = db.prepare('SELECT * FROM assets WHERE asset_code = ?').get(assetCode) as Record<string, unknown> | undefined;
  if (!asset) return { status: 'not_found' as const };

  const updates: string[] = [];
  const values: unknown[] = [];
  for (const field of assetUpdateFields) {
    if (input[field] === undefined) continue;
    updates.push(`${field} = ?`);
    values.push(input[field]);
  }
  if (!updates.length) return { status: 'empty' as const };

  const nextAssignee = input.assignee ?? String(asset.assignee ?? '');
  const nextDepartment = input.department ?? String(asset.department ?? '');
  const nextLocation = input.location ?? String(asset.location ?? '');
  db.exec('BEGIN');
  try {
    values.push(assetCode);
    db.prepare(`UPDATE assets SET ${updates.join(', ')}, updated_at_system = CURRENT_TIMESTAMP WHERE asset_code = ?`).run(...(values as any[]));
    const event = db.prepare(`INSERT INTO asset_events
      (asset_id, asset_code, type, operator_name, from_status, to_status, from_assignee, to_assignee,
       from_department, to_department, from_location, to_location, note)
      VALUES (?, ?, '编辑', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
      .run(...([asset.id as number, assetCode, input.operatorName ?? '', asset.status as string, asset.status as string,
        asset.assignee ?? '', nextAssignee, asset.department ?? '', nextDepartment, asset.location ?? '', nextLocation,
        input.note ?? ''] as any[]));
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
  };
  if (type === '领用' && currentStatus !== '空闲') throw new Error(`资产当前状态为${currentStatus}，不能领用`);
  if ((type === '归还' || type === '借出归还') && !['在用', '借出'].includes(currentStatus)) throw new Error(`资产当前状态为${currentStatus}，不能归还`);
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
  if (replay) return { status: 'replayed' as const, event: replay, asset: getAsset(db, assetCode) };
  const transition = eventTransition(asset, input);
  const nextStatus = transition.status;
  const nextAssignee = transition.clearAssignee ? '' : input.toAssignee ?? String(asset.assignee ?? '');
  const nextDepartment = transition.clearAssignee ? '' : input.toDepartment ?? String(asset.department ?? '');
  const nextLocation = input.toLocation ?? String(asset.location ?? '');

  db.exec('BEGIN');
  try {
    const result = db.prepare(`INSERT INTO asset_events
      (asset_id, asset_code, type, operator_name, from_status, to_status, from_assignee, to_assignee,
       from_department, to_department, from_location, to_location, note, request_id)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
      .run(...([asset.id as number, assetCode, input.type, input.operatorName ?? '', asset.status as string, nextStatus, asset.assignee ?? '', nextAssignee,
        asset.department ?? '', nextDepartment, asset.location ?? '', nextLocation, input.note ?? '', requestId] as any[]));
    db.prepare(`UPDATE assets SET status = ?, assignee = ?, department = ?, location = ?, updated_at_system = CURRENT_TIMESTAMP WHERE asset_code = ?`)
      .run(nextStatus, nextAssignee, nextDepartment, nextLocation, assetCode);
    db.exec('COMMIT');
    return { status: 'created' as const, event: db.prepare('SELECT * FROM asset_events WHERE id = ?').get(result.lastInsertRowid), asset: getAsset(db, assetCode) };
  } catch (error) {
    db.exec('ROLLBACK');
    throw error;
  }
}

export function getStats(db: DatabaseSync) {
  const total = Number((db.prepare('SELECT COUNT(*) AS count FROM assets').get() as { count: number }).count);
  const statusRows = db.prepare('SELECT status, COUNT(*) AS count FROM assets GROUP BY status ORDER BY status').all() as Array<{ status: string; count: number }>;
  const categoryRows = db.prepare('SELECT COALESCE(category_path, \'未分类\') AS name, COUNT(*) AS count FROM assets GROUP BY category_path ORDER BY count DESC').all();
  const locationRows = db.prepare('SELECT COALESCE(location, \'未设置\') AS name, COUNT(*) AS count FROM assets GROUP BY location ORDER BY count DESC').all();
  const companyRows = db.prepare('SELECT COALESCE(company, \'未设置\') AS name, COUNT(*) AS count FROM assets GROUP BY company ORDER BY count DESC').all();
  const amounts = db.prepare('SELECT COALESCE(SUM(amount_taxed), 0) AS taxed, COALESCE(SUM(amount_untaxed), 0) AS untaxed, COALESCE(SUM(tax_amount), 0) AS tax FROM assets').get() as { taxed: number; untaxed: number; tax: number };
  return {
    total,
    status: Object.fromEntries(statusRows.map((row) => [row.status, Number(row.count)])),
    categories: categoryRows,
    locations: locationRows,
    companies: companyRows,
    amounts: { taxed: Number(amounts.taxed), untaxed: Number(amounts.untaxed), tax: Number(amounts.tax) },
  };
}
