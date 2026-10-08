import { Router } from 'express';
import type { DatabaseSync } from 'node:sqlite';
import { getStats } from '../repositories/assets';

export function createStatsRouter(db: DatabaseSync) {
  const router = Router();
  router.get('/', (_req, res) => res.json(getStats(db)));
  return router;
}
