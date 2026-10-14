import { Router } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { requireRole } from '../middleware/auth';
import { completeInventoryTask, createInventoryTask, getInventoryTask, listInventoryTasks, recordInventoryScan } from '../repositories/inventory';

export function createInventoryRouter(db: DatabaseSync) {
  const router = Router();
  router.param('taskId', (_req, res, next, value: string) => {
    if (!/^\d+$/.test(value) || !Number.isSafeInteger(Number(value)) || Number(value) < 1) return res.status(400).json({ message: '盘点任务编号无效' });
    return next();
  });
  router.get('/tasks', (_req, res) => res.json({ tasks: listInventoryTasks(db) }));
  router.get('/tasks/:taskId', (req, res) => {
    const result = getInventoryTask(db, Number(req.params.taskId));
    return result ? res.json(result) : res.status(404).json({ message: '盘点任务不存在' });
  });
  router.post('/tasks', requireRole('admin', 'asset_manager'), (req, res) => {
    const result = createInventoryTask(db, { ...req.body, operatorName: req.user?.display_name ?? req.body?.operatorName });
    if (result.status === 'invalid' || result.status === 'empty') return res.status(400).json({ message: result.message });
    return res.status(201).json(result);
  });
  router.post('/tasks/:taskId/records', requireRole('admin', 'asset_manager'), (req, res) => {
    const result = recordInventoryScan(db, Number(req.params.taskId), { ...req.body, operatorName: req.user?.display_name ?? req.body?.operatorName });
    if (result.status === 'not_found') return res.status(404).json({ message: '盘点任务不存在' });
    if (result.status === 'closed') return res.status(409).json({ message: '盘点任务已结束' });
    if (result.status === 'invalid') return res.status(400).json({ message: result.message });
    if (result.status === 'out_of_scope') return res.status(400).json({ message: '资产不在本次盘点范围内' });
    return res.json(result);
  });
  router.post('/tasks/:taskId/complete', requireRole('admin', 'asset_manager'), (req, res) => {
    const result = completeInventoryTask(db, Number(req.params.taskId), req.user?.display_name ?? String(req.body?.operatorName ?? ''));
    return result.status === 'not_found' ? res.status(404).json({ message: '盘点任务不存在' }) : res.json(result);
  });
  return router;
}
