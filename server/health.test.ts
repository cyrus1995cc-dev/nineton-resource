import { describe, expect, it } from 'vitest';
import request from 'supertest';
import { createApp } from './index';

describe('health endpoint', () => {
  it('reports the service as ready', async () => {
    const response = await request(createApp()).get('/api/health');
    expect(response.status).toBe(200);
    expect(response.body).toEqual({ ok: true });
  });
});
