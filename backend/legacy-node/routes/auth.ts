import { Router } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { authenticate, authenticateDingTalk, authTokenFromRequest, createUser, getDingTalkConfig, listUsers, resolveSession, revokeSession, updateUser } from '../auth';
import { AppError, integer } from '../errors';
import { requireAuth, requireRole } from '../middleware/auth';

export function createAuthRouter(db: DatabaseSync) {
  const router = Router();
  router.post('/login', (req, res) => {
    const result = authenticate(db, String(req.body?.username ?? '').trim(), String(req.body?.password ?? ''));
    if (!result) return res.status(401).json({ message: '用户名或密码错误' });
    return res.json(result);
  });
  router.get('/dingtalk/config', (_req, res) => res.json(getDingTalkConfig()));
  router.post('/dingtalk/login', async (req, res) => {
    try {
      const result = await authenticateDingTalk(db, String(req.body?.code ?? ''));
      if (result.status === 'disabled') return res.status(503).json({ message: '钉钉登录尚未配置' });
      if (result.status === 'invalid_code') return res.status(400).json({ message: '缺少钉钉授权码' });
      if (result.status === 'disabled_user') return res.status(403).json({ message: '钉钉账号已停用' });
      return res.json(result);
    } catch (error) {
      if (error instanceof AppError) return res.status(error.status).json({ message: error.message });
      return res.status(502).json({ message: error instanceof Error ? error.message : '钉钉登录失败' });
    }
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
  router.patch('/users/:id', requireAuth(db), requireRole('admin'), (req, res) => res.json({ user: updateUser(db, req.user!, integer(req.params.id, '账号编号'), req.body ?? {}) }));
  return router;
}
