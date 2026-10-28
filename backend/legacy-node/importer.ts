import * as XLSX from 'xlsx';
import type { DatabaseSync } from 'node:sqlite';
import { assetOwnershipFields, fieldProblem } from '../../frontend/shared/assets';

export type ImportRow = {
  asset_code: string;
  category_level1: string;
  created_at: string;
  category_path: string;
  name: string;
  status: string;
  location: string;
  company: string;
  model: string;
  cpu: string;
  discrete_gpu: string;
  memory: string;
  gpu_model: string;
  serial_number: string;
  remark: string;
  photo_url: string;
  assignee: string;
  department: string;
  received_at: string;
  amount_taxed: number | null;
  amount_untaxed: number | null;
  tax_amount: number | null;
};

export type ImportPreview = {
  newRows: ImportRow[];
  updatedRows: ImportRow[];
  unchangedRows: ImportRow[];
  invalidRows: Array<{ row: ImportRow; reason: string }>;
};

export type ImportResult = {
  totalRows: number;
  newRows: number;
  updatedRows: number;
  unchangedRows: number;
  invalidRows: number;
};

const fieldMap: Record<string, keyof ImportRow> = {
  资产编码: 'asset_code',
  '资产分类(一级)': 'category_level1',
  创建时间: 'created_at',
  资产分类: 'category_path',
  资产名称: 'name',
  资产状态: 'status',
  所在位置: 'location',
  '所属/承租公司': 'company',
  型号: 'model',
  CPU: 'cpu',
  是否有独立显卡: 'discrete_gpu',
  内存: 'memory',
  显卡型号: 'gpu_model',
  设备序列号: 'serial_number',
  备注: 'remark',
  资产照片: 'photo_url',
  使用人: 'assignee',
  使用部门: 'department',
  领用日期: 'received_at',
  '购置/租赁金额（含税）': 'amount_taxed',
  '购置/租赁金额（不含税）': 'amount_untaxed',
  税额: 'tax_amount',
};

const textFields = new Set<keyof ImportRow>([
  'asset_code', 'category_level1', 'created_at', 'category_path', 'name', 'status', 'location',
  'company', 'model', 'cpu', 'discrete_gpu', 'memory', 'gpu_model', 'serial_number', 'remark',
  'photo_url', 'assignee', 'department', 'received_at',
]);

function text(value: unknown): string {
  if (value instanceof Date) return value.toISOString();
  return value === null || value === undefined ? '' : String(value).trim();
}

function amount(value: unknown): number | null {
  if (value === null || value === undefined || String(value).trim() === '') return null;
  const parsed = Number(String(value).replace(/[,￥¥\s]/g, ''));
  if (!Number.isFinite(parsed) || parsed < 0) throw new Error(`金额格式无效：${value}`);
  return parsed;
}

export function parseWorkbook(buffer: Buffer): ImportRow[] {
  const workbook = XLSX.read(buffer, { type: 'buffer', cellDates: true });
  const sheet = workbook.Sheets[workbook.SheetNames[0]];
  const rawRows = XLSX.utils.sheet_to_json<Record<string, unknown>>(sheet, { defval: '' });
  return rawRows.map((source, index) => {
    const row = {} as ImportRow;
    for (const [header, field] of Object.entries(fieldMap)) {
      try { row[field] = (textFields.has(field) ? text(source[header]) : amount(source[header])) as never; }
      catch { throw new Error(`Excel 第 ${index + 2} 行「${header}」金额格式无效，请填写非负数字或留空`); }
    }
    return row;
  });
}

const comparableFields: Array<keyof ImportRow> = Object.keys(fieldMap).map((key) => fieldMap[key]);

function hasValue(value: unknown): boolean {
  return value !== null && value !== undefined && value !== '';
}

function isSame(existing: Record<string, unknown>, incoming: ImportRow): boolean {
  return comparableFields.every((field) => {
    if (!hasValue(incoming[field])) return true;
    return String(existing[field] ?? '') === String(incoming[field] ?? '');
  });
}

export function previewImport(db: DatabaseSync, rows: ImportRow[]): ImportPreview {
  const preview: ImportPreview = { newRows: [], updatedRows: [], unchangedRows: [], invalidRows: [] };
  const seen = new Set<string>();
  for (const row of rows) {
    if (!row.asset_code) {
      preview.invalidRows.push({ row, reason: '资产编码为空' });
      continue;
    }
    if (seen.has(row.asset_code)) {
      preview.invalidRows.push({ row, reason: `资产编码重复：${row.asset_code}` });
      continue;
    }
    seen.add(row.asset_code);
    const existing = db.prepare('SELECT * FROM assets WHERE asset_code = ?').get(row.asset_code) as Record<string, unknown> | undefined;
    const problem = comparableFields.map((field) => {
      if (!hasValue(row[field]) && existing) return null;
      if (!hasValue(row[field]) && !['asset_code', 'name'].includes(field)) return null;
      return fieldProblem(field, row[field]);
    }).find(Boolean);
    if (problem) { preview.invalidRows.push({ row, reason: problem }); continue; }
    if (existing && assetOwnershipFields.some((field) => hasValue(row[field]) && String(row[field]) !== String(existing[field] ?? ''))) {
      preview.invalidRows.push({ row, reason: '已有资产的状态、使用人、部门、位置或领用日期发生变化，请通过流转审批调整；本行未导入' }); continue;
    }
    if (!existing) preview.newRows.push(row);
    else if (isSame(existing, row)) preview.unchangedRows.push(row);
    else preview.updatedRows.push(row);
  }
  return preview;
}

const dbFields: Array<keyof ImportRow> = [
  'category_level1', 'created_at', 'category_path', 'name', 'status', 'location', 'company', 'model',
  'cpu', 'discrete_gpu', 'memory', 'gpu_model', 'serial_number', 'remark', 'photo_url', 'assignee',
  'department', 'received_at', 'amount_taxed', 'amount_untaxed', 'tax_amount',
];

export function applyImport(db: DatabaseSync, rows: ImportRow[], batchId: string, fileName = '', actor = { id: 0, display_name: '命令行导入' }): ImportResult {
  const replay = db.prepare('SELECT * FROM import_batches WHERE checksum = ?').get(batchId) as Record<string, unknown> | undefined;
  if (replay) return { totalRows: Number(replay.total_rows), newRows: Number(replay.new_rows), updatedRows: Number(replay.updated_rows), unchangedRows: Number(replay.unchanged_rows), invalidRows: Number(replay.invalid_rows) };
  const preview = previewImport(db, rows);
  const result: ImportResult = {
    totalRows: rows.length,
    newRows: preview.newRows.length,
    updatedRows: preview.updatedRows.length,
    unchangedRows: preview.unchangedRows.length,
    invalidRows: preview.invalidRows.length,
  };
  const now = new Date().toISOString();
  db.exec('SAVEPOINT import_apply');
  try {
    for (const row of [...preview.newRows, ...preview.updatedRows]) {
      const existing = db.prepare('SELECT * FROM assets WHERE asset_code = ?').get(row.asset_code) as Record<string, any> | undefined;
      if (!existing) {
        const columns = ['asset_code', ...dbFields, 'source_updated_at'];
        const values = [row.asset_code, ...dbFields.map((field) => field === 'status' ? row[field] || '空闲' : row[field] ?? 0), now];
        const placeholders = columns.map(() => '?').join(', ');
        db.prepare(`INSERT INTO assets (${columns.join(', ')}) VALUES (${placeholders})`).run(...(values as any[]));
      } else {
        const updates: string[] = [];
        const values: unknown[] = [];
        for (const field of dbFields) {
          if (hasValue(row[field])) {
            updates.push(`${field} = ?`);
            values.push(row[field]);
          }
        }
        updates.push('source_updated_at = ?', 'updated_at_system = CURRENT_TIMESTAMP');
        values.push(now, row.asset_code);
        db.prepare(`UPDATE assets SET ${updates.join(', ')} WHERE asset_code = ?`).run(...(values as any[]));
      }
      const stored = db.prepare('SELECT * FROM assets WHERE asset_code = ?').get(row.asset_code) as Record<string, any>;
      const changes = dbFields.filter((field) => String(existing?.[field] ?? '') !== String(stored[field] ?? ''))
        .map((field) => ({ field, before: existing?.[field] ?? '', after: stored[field] ?? '' }));
      db.prepare(`INSERT INTO asset_events (asset_id, asset_code, type, operator_name, from_status, to_status, from_assignee, to_assignee, from_department, to_department, from_location, to_location, note, changes_json)
        VALUES (?, ?, '导入', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`).run(stored.id, row.asset_code, actor.display_name, existing?.status ?? '', stored.status, existing?.assignee ?? '', stored.assignee ?? '', existing?.department ?? '', stored.department ?? '', existing?.location ?? '', stored.location ?? '', `导入批次 ${batchId} · ${fileName}`, JSON.stringify(changes));
    }
    db.prepare(`INSERT INTO import_batches (file_name, checksum, total_rows, new_rows, updated_rows, unchanged_rows, invalid_rows, errors_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
      .run(fileName, batchId, result.totalRows, result.newRows, result.updatedRows, result.unchangedRows, result.invalidRows, JSON.stringify(preview.invalidRows.map((item) => item.reason)));
    db.prepare('INSERT INTO audit_logs (actor_id, actor_name, action, target, details_json) VALUES (?, ?, ?, ?, ?)').run(actor.id || null, actor.display_name, 'import', batchId, JSON.stringify(result));
    db.exec('RELEASE import_apply');
    return result;
  } catch (error) {
    db.exec('ROLLBACK TO import_apply; RELEASE import_apply');
    throw error;
  }
}
