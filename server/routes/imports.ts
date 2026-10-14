import { randomUUID } from 'node:crypto';
import { Router } from 'express';
import multer from 'multer';
import type { DatabaseSync } from 'node:sqlite';
import { applyImport, parseWorkbook, previewImport, type ImportRow } from '../importer';

const upload = multer({ storage: multer.memoryStorage(), limits: { fileSize: 20 * 1024 * 1024 } });

export function createImportRouter(db: DatabaseSync) {
  const router = Router();

  router.post('/preview', upload.single('file'), (req, res) => {
    if (!req.file) return res.status(400).json({ message: '请上传 Excel 文件' });
    try {
      const rows = parseWorkbook(req.file.buffer);
      const preview = previewImport(db, rows);
      return res.json({ batchId: randomUUID(), fileName: req.file.originalname, rows,
        preview: { newRows: preview.newRows.length, updatedRows: preview.updatedRows.length, unchangedRows: preview.unchangedRows.length, invalidRows: preview.invalidRows.length },
        errors: preview.invalidRows.map(({ row, reason }) => ({ row: rows.indexOf(row) + 2, assetCode: row.asset_code, reason })),
      });
    } catch (error) {
      return res.status(400).json({ message: error instanceof Error ? error.message : 'Excel 解析失败' });
    }
  });

  router.post('/confirm', (req, res) => {
    const rows = Array.isArray(req.body?.rows) ? req.body.rows as ImportRow[] : [];
    if (!rows.length) return res.status(400).json({ message: '没有可导入的数据' });
    const result = applyImport(db, rows, String(req.body?.batchId ?? randomUUID()), String(req.body?.fileName ?? ''));
    return res.json(result);
  });

  return router;
}
