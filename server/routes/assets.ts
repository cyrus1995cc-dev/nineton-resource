import { Router } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { getAsset, listAssets, saveAssetQr, updateAsset, type AssetUpdateInput } from '../repositories/assets';
import { requireRole } from '../middleware/auth';

export function createAssetRouter(db: DatabaseSync) {
  const router = Router();
  router.get('/', (req, res) => res.json(listAssets(db, {
    search: String(req.query.search ?? ''), status: String(req.query.status ?? ''), category: String(req.query.category ?? ''),
    location: String(req.query.location ?? ''), page: Number(req.query.page ?? 1), pageSize: Number(req.query.pageSize ?? 20),
  })));
  router.patch('/:assetCode', requireRole('admin', 'asset_manager'), (req, res) => {
    const result = updateAsset(db, String(req.params.assetCode), { ...req.body, operatorName: req.user?.display_name ?? req.body?.operatorName } as AssetUpdateInput);
    if (result.status === 'not_found') return res.status(404).json({ message: '资产不存在' });
    if (result.status === 'empty') return res.status(400).json({ message: '没有需要保存的资产资料' });
    return res.json(result);
  });
  router.get('/:assetCode', (req, res) => {
    const asset = getAsset(db, req.params.assetCode);
    return asset ? res.json(asset) : res.status(404).json({ message: '资产不存在' });
  });
  router.post('/:assetCode/qr', requireRole('admin', 'asset_manager'), (req, res) => {
    const result = saveAssetQr(db, String(req.params.assetCode), String(req.body?.raw ?? ''));
    if (result.status === 'not_found') return res.status(404).json({ message: '资产不存在' });
    if (result.status === 'invalid') return res.status(400).json({ message: '二维码内容与资产编码不匹配' });
    return res.json(result);
  });
  return router;
}
