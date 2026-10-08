import express from 'express';
import cors from 'cors';

export function createApp() {
  const app = express();
  app.use(cors());
  app.use(express.json({ limit: '2mb' }));

  app.get('/api/health', (_req, res) => {
    res.json({ ok: true });
  });

  return app;
}

if (process.env.NODE_ENV !== 'test') {
  const port = Number(process.env.PORT ?? 3000);
  createApp().listen(port, '0.0.0.0', () => {
    console.log(`Asset API listening on http://localhost:${port}`);
  });
}
