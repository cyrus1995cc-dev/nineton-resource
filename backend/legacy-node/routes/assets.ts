import { Router } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { getAsset, listAssets, saveAssetQr, updateAsset, type AssetUpdateInput } from '../repositories/assets';
import { requireRole } from '../middleware/auth';
import { AppError, integer } from '../errors';
import { assetScope, restrictedUser } from '../access';
import { assetOwnershipFields } from '../../../frontend/shared/assets';

export function createAssetRouter(db: DatabaseSync) {
  const router = Router();
  router.get('/', (req, res) => res.json(listAssets(db, {
    search: String(req.query.search ?? ''), status: String(req.query.status ?? ''), category: String(req.query.category ?? ''),
    location: String(req.query.location ?? ''), scope: String(req.query.scope ?? ''), page: integer(req.query.page, '页码', 1, 1000000), pageSize: integer(req.query.pageSize, '每页条数', 20, 100),
  }, req.user)));
  router.patch('/:assetCode', requireRole('admin', 'asset_manager'), (req, res) => {
    if (req.user) {
      const asset = getAsset(db, String(req.params.assetCode)) as Record<string, unknown> | null;
      if (asset && assetOwnershipFields.some((field) => req.body?.[field] !== undefined && String(req.body[field]) !== String(asset[field] ?? ''))) {
        return res.status(409).json({ message: '使用人、部门和位置的变更请提交交接或调拨审批申请' });
      }
      if (asset && req.body?.expectedVersion === undefined) throw new AppError(400, '缺少资料版本，请重新打开资产后保存');
    }
    const result = updateAsset(db, String(req.params.assetCode), { ...req.body, operatorName: req.user?.display_name ?? req.body?.operatorName } as AssetUpdateInput);
    if (result.status === 'not_found') return res.status(404).json({ message: '资产不存在' });
    if (result.status === 'empty') return res.status(400).json({ message: '没有需要保存的资产资料' });
    return res.json(result);
  });
  router.get('/:assetCode', (req, res) => {
    if (restrictedUser(req.user)) {
      const scope = assetScope(req.user); const available = assetScope(req.user, true);
      if (!db.prepare(`SELECT id FROM assets WHERE asset_code = ? AND (${scope.clause} OR ${available.clause})`).get(req.params.assetCode, ...scope.values)) throw new AppError(404, '资产不存在或无权查看');
    }
    const asset = getAsset(db, req.params.assetCode);
    if (asset && restrictedUser(req.user) && asset.status === '空闲' && asset.assignee_employee_id !== req.user?.employee_id) asset.events = [];
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
