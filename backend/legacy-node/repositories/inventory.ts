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
    checked_count = (SELECT COUNT(*) FROM inventory_records WHERE task_id = ? AND result IN ('found', 'different')),
    difference_count = (SELECT COUNT(*) FROM inventory_records WHERE task_id = ? AND result IN ('different', 'missing'))
    WHERE id = ?`).run(taskId, taskId, taskId);
}

export function listInventoryTasks(db: DatabaseSync) {
  return (db.prepare('SELECT * FROM inventory_tasks ORDER BY id DESC').all() as Array<Record<string, unknown>>).map(taskRow);
}

function auditInventory(db: DatabaseSync, task: Record<string, unknown>, record: Record<string, unknown>, result: string, actualLocation: string, note: string, operatorName: string) {
  // Inventory observes the asset; a location discrepancy is resolved separately by editing or transfer.
  const label = result === 'missing' ? '未盘到' : result === 'different' ? '位置差异' : '正常';
  const description = `盘点任务 #${task.id}「${task.name}」：${label}；应在 ${record.expected_location || '未设置'}${result === 'missing' ? '' : `；实盘位置 ${actualLocation || '未设置'}`}${note ? `；备注：${note}` : ''}`;
  db.prepare(`INSERT INTO asset_events
    (asset_id, asset_code, type, operator_name, from_status, to_status, from_assignee, to_assignee,
     from_department, to_department, from_location, to_location, note)
    SELECT id, asset_code, '盘点', ?, status, status, assignee, assignee, department, department, location, location, ?
    FROM assets WHERE id = ?`).run(operatorName, description, Number(record.asset_id));
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
  const result = actualLocation !== String(record.expected_location ?? '') ? 'different' : 'found';
  const note = String(input.note ?? '').trim();
  const operatorName = String(input.operatorName ?? '').trim();
  if (record.result === result && record.actual_location === actualLocation && (record.note ?? '') === note) {
    return { status: 'recorded' as const, replayed: true, record: recordRow(record), ...getInventoryTask(db, taskId)! };
  }
  db.exec('BEGIN');
  try {
    db.prepare(`UPDATE inventory_records SET result = ?, actual_location = ?, note = ?, operator_name = ?, checked_at = CURRENT_TIMESTAMP WHERE id = ?`)
      .run(result, actualLocation, note, operatorName, Number(record.id));
    auditInventory(db, task, record, result, actualLocation, note, operatorName);
    refreshTaskCounts(db, taskId);
    db.exec('COMMIT');
    return { status: 'recorded' as const, replayed: false, record: recordRow(db.prepare('SELECT * FROM inventory_records WHERE id = ?').get(Number(record.id)) as Record<string, unknown>), ...getInventoryTask(db, taskId)! };
  } catch (error) { db.exec('ROLLBACK'); throw error; }
}

export function completeInventoryTask(db: DatabaseSync, taskId: number, operatorName = '') {
  const task = db.prepare('SELECT * FROM inventory_tasks WHERE id = ?').get(taskId) as Record<string, unknown> | undefined;
  if (!task) return { status: 'not_found' as const };
  if (task.status !== 'completed') {
    db.exec('BEGIN');
    try {
      const pending = db.prepare("SELECT * FROM inventory_records WHERE task_id = ? AND result = 'pending'").all(taskId) as Array<Record<string, unknown>>;
      for (const record of pending) auditInventory(db, task, record, 'missing', '', '', operatorName);
      db.prepare(`UPDATE inventory_records SET result = 'missing', operator_name = ?, checked_at = NULL WHERE task_id = ? AND result = 'pending'`).run(operatorName, taskId);
      db.prepare(`UPDATE inventory_tasks SET status = 'completed', completed_at = CURRENT_TIMESTAMP WHERE id = ?`).run(taskId);
      refreshTaskCounts(db, taskId);
      db.exec('COMMIT');
    } catch (error) { db.exec('ROLLBACK'); throw error; }
  }
  return { status: 'completed' as const, ...getInventoryTask(db, taskId)! };
}
