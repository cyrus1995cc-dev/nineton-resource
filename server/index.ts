import express, { type ErrorRequestHandler } from 'express';
import cors from 'cors';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import type { DatabaseSync } from 'node:sqlite';
import { openDatabase } from './db';
import { createImportRouter } from './routes/imports';
import { createAssetRouter } from './routes/assets';
import { createEventRouter } from './routes/events';
import { createStatsRouter } from './routes/stats';
import { createInventoryRouter } from './routes/inventory';
import { createAuthRouter } from './routes/auth';
import { createFlowRouter } from './routes/flows';
import { ensureDefaultAdmin } from './auth';
import { requireAuth, requireRole } from './middleware/auth';
import { AppError } from './errors';
import { createDirectoryRouter } from './routes/directory';
import { startDirectoryScheduler } from './repositories/directory';

export function createApp(options: { db?: DatabaseSync; auth?: boolean } = {}) {
  const app = express();
  const db = options.db ?? openDatabase();
  const authenticationEnabled = options.auth ?? !options.db;
  ensureDefaultAdmin(db);
  app.use(cors());
  app.use(express.json({ limit: '2mb' }));

  app.get('/api/health', (_req, res) => {
    res.json({ ok: true });
  });

  app.use('/api/auth', createAuthRouter(db));
  if (authenticationEnabled) app.use('/api', requireAuth(db));
  app.use('/api/imports', requireRole('admin', 'asset_manager'), createImportRouter(db));
  app.use('/api/assets', createAssetRouter(db));
  app.use('/api/assets', createEventRouter(db));
  app.use('/api/stats', createStatsRouter(db));
  app.use('/api/inventory', requireRole('admin', 'asset_manager', 'viewer'), createInventoryRouter(db));
  app.use('/api/flows', createFlowRouter(db));
  app.use('/api/directory', createDirectoryRouter(db));
  if (!options.db) startDirectoryScheduler(db);

  const distPath = join(process.cwd(), 'dist');
  if (existsSync(distPath)) app.use(express.static(distPath));
  const errors: ErrorRequestHandler = (error, _req, res, _next) => {
    if (error instanceof AppError) return void res.status(error.status).json({ message: error.message });
    if (error instanceof SyntaxError || error.code === 'LIMIT_FILE_SIZE') return void res.status(400).json({ message: '请求格式错误或文件超过 20MB' });
    console.error('API request failed', error); res.status(500).json({ message: '操作失败，请稍后重试' });
  };
  app.use(errors);

  return app;
}

if (process.env.NODE_ENV !== 'test') {
  const port = Number(process.env.PORT ?? 3000);
  createApp().listen(port, '0.0.0.0', () => {
    console.log(`Asset API listening on http://localhost:${port}`);
  });
}
