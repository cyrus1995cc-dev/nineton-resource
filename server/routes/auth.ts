import { Router } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { authenticate, authTokenFromRequest, createUser, listUsers, resolveSession, revokeSession } from '../auth';
import { requireAuth, requireRole } from '../middleware/auth';

export function createAuthRouter(db: DatabaseSync) {
  const router = Router();
  router.post('/login', (req, res) => {
    const result = authenticate(db, String(req.body?.username ?? '').trim(), String(req.body?.password ?? ''));
    if (!result) return res.status(401).json({ message: '用户名或密码错误' });
    return res.json(result);
  });
  router.get('/me', (req, res) => {
    const user = resolveSession(db, authTokenFromRequest(req.header('authorization')));
    return user ? res.json({ user }) : res.status(401).json({ message: '登录已失效，请重新登录' });
  });
  router.post('/logout', (req, res) => {
    revokeSession(db, authTokenFromRequest(req.header('authorization')));
    return res.status(204).send();
  });
  router.get('/users', requireAuth(db), requireRole('admin'), (_req, res) => res.json({ users: listUsers(db) }));
  router.post('/users', requireAuth(db), requireRole('admin'), (req, res) => {
    const result = createUser(db, req.body ?? {});
    if (result.status === 'invalid') return res.status(400).json({ message: '请填写有效的用户信息' });
    if (result.status === 'duplicate') return res.status(409).json({ message: '用户名已存在' });
    return res.status(201).json(result);
  });
  return router;
}
