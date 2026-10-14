import { Router, type Request, type Response } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { requireAuth, requireRole } from '../middleware/auth';
import { actOnFlow, commentOnFlow, createFlow, FlowError, flowOptions, getFlow, listFlows } from '../repositories/flows';

export function createFlowRouter(db: DatabaseSync) {
  const router = Router();
  router.use(requireAuth(db));
  const handle = (fn: (req: Request, res: Response) => unknown) => (req: Request, res: Response) => {
    try { return fn(req, res); }
    catch (error) {
      if (error instanceof FlowError) return res.status(error.status).json({ message: error.message });
      console.error('Flow operation failed', error);
      return res.status(500).json({ message: '单据保存失败，本次操作已回滚，请稍后重试' });
    }
  };
  const id = (req: Request) => {
    const value = Number(req.params.id); if (!Number.isSafeInteger(value) || value < 1) throw new FlowError(400, '无效单据编号'); return value;
  };
  router.get('/options', (_req, res) => res.json({ approvers: flowOptions(db), notification: { mode: 'queued', message: '通知已接入队列，钉钉发送待配置联调' } }));
  router.get('/', handle((req, res) => res.json(listFlows(db, req.user!, { scope: String(req.query.scope ?? ''), status: String(req.query.status ?? ''), search: String(req.query.search ?? ''), page: Number(req.query.page) }))));
  router.get('/:id', handle((req, res) => res.json(getFlow(db, id(req)))));
  router.post('/', requireRole('admin', 'asset_manager'), handle((req, res) => { const result = createFlow(db, req.user!, req.body); return res.status('replayed' in result ? 200 : 201).json(result); }));
  for (const action of ['approve', 'reject', 'withdraw'] as const) router.post(`/:id/${action}`, requireRole('admin', 'asset_manager'), handle((req, res) => res.json(actOnFlow(db, id(req), req.user!, action, req.body?.comment))));
  router.post('/:id/comments', requireRole('admin', 'asset_manager'), handle((req, res) => res.status(201).json(commentOnFlow(db, id(req), req.user!, req.body?.content))));
  return router;
}
