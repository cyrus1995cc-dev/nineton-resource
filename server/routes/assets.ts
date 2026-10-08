import { Router } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { createAssetEvent, getAsset, listAssets } from '../repositories/assets';

export function createAssetRouter(db: DatabaseSync) {
  const router = Router();
  router.get('/', (req, res) => res.json(listAssets(db, {
    search: String(req.query.search ?? ''), status: String(req.query.status ?? ''), category: String(req.query.category ?? ''),
    location: String(req.query.location ?? ''), page: Number(req.query.page ?? 1), pageSize: Number(req.query.pageSize ?? 20),
  })));
  router.get('/:assetCode', (req, res) => {
    const asset = getAsset(db, req.params.assetCode);
    return asset ? res.json(asset) : res.status(404).json({ message: '资产不存在' });
  });
  return router;
}
