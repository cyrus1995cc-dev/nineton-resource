import { Router } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { createAssetEvent } from '../repositories/assets';
import { requireRole } from '../middleware/auth';

export function createEventRouter(db: DatabaseSync) {
  const router = Router();
  router.post('/:assetCode/events', requireRole('admin', 'asset_manager'), (req, res) => {
    try {
      const result = createAssetEvent(db, String(req.params.assetCode), req.body ?? {});
      if (result.status === 'not_found') return res.status(404).json({ message: '资产不存在' });
      if (result.status === 'replayed') return res.status(200).json({ ...result, replayed: true });
      return res.status(201).json(result);
    } catch (error) {
      return res.status(409).json({ message: error instanceof Error ? error.message : '资产操作失败' });
    }
  });
  return router;
}
