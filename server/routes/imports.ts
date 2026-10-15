import { randomUUID } from 'node:crypto';
import { Router } from 'express';
import multer from 'multer';
import type { DatabaseSync } from 'node:sqlite';
import { applyImport, parseWorkbook, previewImport, type ImportRow } from '../importer';
import { AppError } from '../errors';

const upload = multer({ storage: multer.memoryStorage(), limits: { fileSize: 20 * 1024 * 1024 } });
export function createImportRouter(db: DatabaseSync) {
  const router = Router();
  router.post('/preview', upload.single('file'), (req, res) => {
    if (!req.file) throw new AppError(400, '请上传 Excel 文件');
    let rows: ImportRow[];
    try { rows = parseWorkbook(req.file.buffer); }
    catch (error) { throw new AppError(400, error instanceof Error ? error.message : 'Excel 解析失败'); }
    if (!rows.length || rows.length > 20000) throw new AppError(400, '每次请导入 1 至 20000 行数据');
    const preview = previewImport(db, rows); const batchId = randomUUID(); const expiresAt = Date.now() + 30 * 60 * 1000;
    const versions = Object.fromEntries(rows.map((row) => [row.asset_code, db.prepare('SELECT version FROM assets WHERE asset_code = ?').get(row.asset_code)?.version ?? null]));
    db.prepare('INSERT INTO import_previews (id, actor_id, file_name, rows_json, versions_json, expires_at) VALUES (?, ?, ?, ?, ?, ?)')
      .run(batchId, req.user?.id ?? 0, req.file.originalname, JSON.stringify(rows), JSON.stringify(versions), expiresAt);
    res.json({ batchId, fileName: req.file.originalname, expiresAt,
      preview: { newRows: preview.newRows.length, updatedRows: preview.updatedRows.length, unchangedRows: preview.unchangedRows.length, invalidRows: preview.invalidRows.length },
      errors: preview.invalidRows.map(({ row, reason }) => ({ row: rows.indexOf(row) + 2, assetCode: row.asset_code, reason })),
      changes: preview.updatedRows.map((row) => {
        const current = db.prepare('SELECT * FROM assets WHERE asset_code = ?').get(row.asset_code)!;
        return { assetCode: row.asset_code, fields: Object.entries(row).filter(([field, value]) => value !== '' && value !== null && String(current[field] ?? '') !== String(value)).map(([field, after]) => ({ field, before: current[field] ?? '', after })) };
      }),
    });
  });
  router.post('/confirm', (req, res) => {
    const preview = db.prepare('SELECT * FROM import_previews WHERE id = ?').get(String(req.body?.batchId ?? '')) as Record<string, any> | undefined;
    if (!preview || preview.actor_id !== (req.user?.id ?? 0)) throw new AppError(404, '导入预览不存在或不属于当前账号，请重新上传');
    if (preview.result_json) return res.json(JSON.parse(preview.result_json));
    if (preview.expires_at < Date.now()) throw new AppError(409, '导入预览已过期，请重新上传');
    db.exec('BEGIN IMMEDIATE');
    try {
      const versions = JSON.parse(preview.versions_json) as Record<string, number | null>;
      for (const [code, version] of Object.entries(versions)) {
        const current = db.prepare('SELECT version FROM assets WHERE asset_code = ?').get(code)?.version ?? null;
        if (current !== version) throw new AppError(409, `${code} 在预览后已变化，请重新上传核对差异`);
      }
      const result = applyImport(db, JSON.parse(preview.rows_json), preview.id, preview.file_name, req.user ?? { id: 0, display_name: '测试导入' });
      db.prepare('UPDATE import_previews SET result_json = ? WHERE id = ?').run(JSON.stringify(result), preview.id);
      db.exec('COMMIT'); return res.json(result);
    } catch (error) { db.exec('ROLLBACK'); throw error; }
  });
  return router;
}
