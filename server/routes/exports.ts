import { Router, type Request, type Response } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { requireAuth, requireRole } from '../middleware/auth';
import { boundedText, integer } from '../errors';
import { excelMime, exportAssets, exportFlows, exportInventory } from '../exports';

export function createExportRouter(db: DatabaseSync) {
  const router = Router(); router.use(requireAuth(db));
  const filters = (req: Request) => Object.fromEntries(['scope', 'search', 'status', 'category', 'location'].map((key) => [key, boundedText(req.query[key] ?? '', '筛选条件', 200)]));
  const send = (res: Response, file: { buffer: Buffer; filename: string }) => {
    res.set({ 'Content-Type': excelMime, 'Content-Disposition': `attachment; filename="export.xlsx"; filename*=UTF-8''${encodeURIComponent(file.filename)}`, 'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff', 'Access-Control-Expose-Headers': 'Content-Disposition' });
    res.send(file.buffer);
  };
  router.get('/assets', (req, res) => send(res, exportAssets(db, req.user!, filters(req))));
  router.get('/flows', (req, res) => send(res, exportFlows(db, req.user!, filters(req))));
  router.get('/inventory/:taskId', requireRole('admin', 'asset_manager', 'viewer'), (req, res) => send(res, exportInventory(db, req.user!, integer(req.params.taskId, '盘点任务编号'))));
  return router;
}
