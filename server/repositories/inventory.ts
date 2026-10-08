import type { DatabaseSync } from 'node:sqlite';

export type InventoryTaskInput = {
  name?: string;
  location?: string;
  department?: string;
  operatorName?: string;
};

export type InventoryRecordInput = {
  assetCode?: string;
  result?: 'found';
  actualLocation?: string;
  note?: string;
  operatorName?: string;
};

function taskRow(row: Record<string, unknown> | undefined) {
  if (!row) return null;
  return {
    ...row,
    id: Number(row.id),
    total_count: Number(row.total_count ?? 0),
    checked_count: Number(row.checked_count ?? 0),
    difference_count: Number(row.difference_count ?? 0),
  };
}

function recordRow(row: Record<string, unknown>) {
  return {
    ...row,
    id: Number(row.id),
    task_id: Number(row.task_id),
    asset_id: Number(row.asset_id),
  };
}

function recordsForTask(db: DatabaseSync, taskId: number) {
  return (db.prepare('SELECT * FROM inventory_records WHERE task_id = ? ORDER BY id').all(taskId) as Array<Record<string, unknown>>).map(recordRow);
}

function refreshTaskCounts(db: DatabaseSync, taskId: number) {
  db.prepare(`UPDATE inventory_tasks SET
    checked_count = (SELECT COUNT(*) FROM inventory_records WHERE task_id = ? AND result <> 'pending'),
    difference_count = (SELECT COUNT(*) FROM inventory_records WHERE task_id = ? AND result IN ('different', 'missing'))
    WHERE id = ?`).run(taskId, taskId, taskId);
}

export function listInventoryTasks(db: DatabaseSync) {
  return (db.prepare('SELECT * FROM inventory_tasks ORDER BY id DESC').all() as Array<Record<string, unknown>>).map(taskRow);
}

export function getInventoryTask(db: DatabaseSync, taskId: number) {
  const task = taskRow(db.prepare('SELECT * FROM inventory_tasks WHERE id = ?').get(taskId) as Record<string, unknown> | undefined);
  return task ? { task, records: recordsForTask(db, taskId) } : null;
}

export function createInventoryTask(db: DatabaseSync, input: InventoryTaskInput) {
  const name = String(input.name ?? '').trim();
  if (!name) return { status: 'invalid' as const, message: '盘点任务名称不能为空' };
  const location = String(input.location ?? '').trim();
  const department = String(input.department ?? '').trim();
  const filters: string[] = [];
  const values: string[] = [];
  if (location) { filters.push('location = ?'); values.push(location); }
  if (department) { filters.push('department = ?'); values.push(department); }
  const where = filters.length ? `WHERE ${filters.join(' AND ')}` : '';
  const assets = db.prepare(`SELECT id, asset_code, name, status, location, assignee, department FROM assets ${where} ORDER BY asset_code`).all(...values) as Array<Record<string, unknown>>;
  if (!assets.length) return { status: 'empty' as const, message: '没有符合条件的资产' };

  db.exec('BEGIN');
  try {
    const created = db.prepare(`INSERT INTO inventory_tasks (name, location_filter, department_filter, operator_name, total_count)
      VALUES (?, ?, ?, ?, ?)`).run(name, location, department, input.operatorName ?? '', assets.length);
    const taskId = Number(created.lastInsertRowid);
    const insertRecord = db.prepare(`INSERT INTO inventory_records
      (task_id, asset_id, asset_code, asset_name, expected_status, expected_location, expected_assignee, expected_department)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?)`);
    for (const asset of assets) insertRecord.run(...([taskId, asset.id, asset.asset_code, asset.name ?? '', asset.status ?? '', asset.location ?? '', asset.assignee ?? '', asset.department ?? ''] as any[]));
    db.exec('COMMIT');
    return { status: 'created' as const, ...getInventoryTask(db, taskId)! };
  } catch (error) {
    db.exec('ROLLBACK');
    throw error;
  }
}

export function recordInventoryScan(db: DatabaseSync, taskId: number, input: InventoryRecordInput) {
  const task = db.prepare('SELECT * FROM inventory_tasks WHERE id = ?').get(taskId) as Record<string, unknown> | undefined;
  if (!task) return { status: 'not_found' as const };
  if (task.status === 'completed') return { status: 'closed' as const };
  const assetCode = String(input.assetCode ?? '').trim();
  if (!assetCode) return { status: 'invalid' as const, message: '资产编码不能为空' };
  const record = db.prepare('SELECT * FROM inventory_records WHERE task_id = ? AND asset_code = ?').get(taskId, assetCode) as Record<string, unknown> | undefined;
  if (!record) return { status: 'out_of_scope' as const };
  const actualLocation = String(input.actualLocation ?? record.expected_location ?? '').trim();
  const result = actualLocation && actualLocation !== String(record.expected_location ?? '') ? 'different' : 'found';
  db.prepare(`UPDATE inventory_records SET result = ?, actual_location = ?, note = ?, operator_name = ?, checked_at = CURRENT_TIMESTAMP WHERE id = ?`)
    .run(...([result, actualLocation, input.note ?? '', input.operatorName ?? '', record.id] as any[]));
  refreshTaskCounts(db, taskId);
  return { status: 'recorded' as const, record: recordRow(db.prepare('SELECT * FROM inventory_records WHERE id = ?').get(record.id as number) as Record<string, unknown>), ...getInventoryTask(db, taskId)! };
}

export function completeInventoryTask(db: DatabaseSync, taskId: number, operatorName = '') {
  const task = db.prepare('SELECT * FROM inventory_tasks WHERE id = ?').get(taskId) as Record<string, unknown> | undefined;
  if (!task) return { status: 'not_found' as const };
  if (task.status !== 'completed') {
    db.prepare(`UPDATE inventory_records SET result = 'missing', operator_name = ?, checked_at = CURRENT_TIMESTAMP WHERE task_id = ? AND result = 'pending'`).run(operatorName, taskId);
    db.prepare(`UPDATE inventory_tasks SET status = 'completed', completed_at = CURRENT_TIMESTAMP WHERE id = ?`).run(taskId);
    refreshTaskCounts(db, taskId);
  }
  return { status: 'completed' as const, ...getInventoryTask(db, taskId)! };
}
