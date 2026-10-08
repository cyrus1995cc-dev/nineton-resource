import type { NextFunction, Request, Response } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { authTokenFromRequest, resolveSession, type AuthUser, type UserRole } from '../auth';

declare global {
  namespace Express { interface Request { user?: AuthUser } }
}

export function requireAuth(db: DatabaseSync) {
  return (req: Request, res: Response, next: NextFunction) => {
    const user = resolveSession(db, authTokenFromRequest(req.header('authorization')));
    if (!user) return res.status(401).json({ message: '请先登录' });
    req.user = user;
    return next();
  };
}

export function requireRole(...roles: UserRole[]) {
  return (req: Request, res: Response, next: NextFunction) => {
    if (req.user && !roles.includes(req.user.role)) return res.status(403).json({ message: '没有权限执行此操作' });
    return next();
  };
}
