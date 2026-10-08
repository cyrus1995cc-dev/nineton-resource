import express from 'express';
import cors from 'cors';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import type { DatabaseSync } from 'node:sqlite';
import { openDatabase } from './db';
import { createImportRouter } from './routes/imports';

export function createApp(options: { db?: DatabaseSync } = {}) {
  const app = express();
  const db = options.db ?? openDatabase();
  app.use(cors());
  app.use(express.json({ limit: '2mb' }));

  app.get('/api/health', (_req, res) => {
    res.json({ ok: true });
  });

  app.use('/api/imports', createImportRouter(db));

  const distPath = join(process.cwd(), 'dist');
  if (existsSync(distPath)) app.use(express.static(distPath));

  return app;
}

if (process.env.NODE_ENV !== 'test') {
  const port = Number(process.env.PORT ?? 3000);
  createApp().listen(port, '0.0.0.0', () => {
    console.log(`Asset API listening on http://localhost:${port}`);
  });
}
