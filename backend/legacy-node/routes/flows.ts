import { Router, type Request, type Response } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { requireAuth, requireRole } from '../middleware/auth';
import { actOnFlow, commentOnFlow, createFlow, FlowError, flowOptions, getFlow, listFlows, canViewFlow } from '../repositories/flows';
import { listEmployees } from '../repositories/directory';
import { AppError, integer } from '../errors';

export function createFlowRouter(db: DatabaseSync) {
  const router = Router();
  router.use(requireAuth(db));
  const handle = (fn: (req: Request, res: Response) => unknown) => (req: Request, res: Response) => {
    try { return fn(req, res); }
    catch (error) {
      if (error instanceof FlowError) return res.status(error.status).json({ message: error.message });
      if (error instanceof AppError) return res.status(error.status).json({ message: error.message });
      console.error('Flow operation failed', error);
      return res.status(500).json({ message: '单据保存失败，本次操作已回滚，请稍后重试' });
    }
  };
  const id = (req: Request) => {
    const value = Number(req.params.id); if (!Number.isSafeInteger(value) || value < 1) throw new FlowError(400, '无效单据编号'); return value;
  };
  router.get('/options', (_req, res) => res.json({ approvers: flowOptions(db), employees: listEmployees(db), notification: { mode: 'queued', message: '通知已接入队列，钉钉发送待配置联调' } }));
  router.get('/', handle((req, res) => res.json(listFlows(db, req.user!, { scope: String(req.query.scope ?? ''), status: String(req.query.status ?? ''), search: String(req.query.search ?? ''), page: integer(req.query.page, '页码', 1, 1000000) }))));
  router.get('/:id', handle((req, res) => { if (!canViewFlow(db, id(req), req.user!)) throw new FlowError(404, '单据不存在或无权查看'); return res.json(getFlow(db, id(req))); }));
  router.post('/', requireRole('admin', 'asset_manager', 'employee', 'approver'), handle((req, res) => { const result = createFlow(db, req.user!, req.body); return res.status('replayed' in result ? 200 : 201).json(result); }));
  for (const action of ['approve', 'reject', 'withdraw'] as const) router.post(`/:id/${action}`, requireRole(...(action === 'withdraw' ? ['admin', 'asset_manager', 'employee', 'approver'] as const : ['admin', 'asset_manager', 'approver'] as const)), handle((req, res) => res.json(actOnFlow(db, id(req), req.user!, action, req.body?.comment))));
  router.post('/:id/comments', requireRole('admin', 'asset_manager', 'employee', 'approver'), handle((req, res) => { if (!canViewFlow(db, id(req), req.user!)) throw new FlowError(404, '单据不存在或无权查看'); return res.status(201).json(commentOnFlow(db, id(req), req.user!, req.body?.content)); }));
  return router;
}
