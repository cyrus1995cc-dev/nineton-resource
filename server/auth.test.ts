import { describe, expect, it } from 'vitest';
import request from 'supertest';
import { openDatabase } from './db';
import { createApp } from './index';

describe('authentication API', () => {
  it('requires a session before reading protected asset data', async () => {
    const db = openDatabase(':memory:');
    const response = await request(createApp({ db, auth: true })).get('/api/stats');
    expect(response.status).toBe(401);
  });

  it('logs in the local administrator and returns the current user', async () => {
    const db = openDatabase(':memory:');
    const app = createApp({ db, auth: true });
    const login = await request(app).post('/api/auth/login').send({ username: 'admin', password: 'admin123' });
    expect(login.status).toBe(200);
    expect(login.body.token).toEqual(expect.any(String));
    expect(login.body.user).toMatchObject({ username: 'admin', role: 'admin' });

    const me = await request(app).get('/api/auth/me').set('Authorization', `Bearer ${login.body.token}`);
    expect(me.status).toBe(200);
    expect(me.body.user.username).toBe('admin');
  });

  it('rejects an invalid password', async () => {
    const db = openDatabase(':memory:');
    const response = await request(createApp({ db, auth: true })).post('/api/auth/login').send({ username: 'admin', password: 'wrong' });
    expect(response.status).toBe(401);
  });

  it('lets an administrator create a read-only user whose writes are denied', async () => {
    const db = openDatabase(':memory:');
    const app = createApp({ db, auth: true });
    const adminLogin = await request(app).post('/api/auth/login').send({ username: 'admin', password: 'admin123' });
    const created = await request(app).post('/api/auth/users').set('Authorization', `Bearer ${adminLogin.body.token}`).send({
      username: 'viewer', password: 'viewer123', display_name: '只读用户', role: 'viewer',
    });
    expect(created.status).toBe(201);

    const viewerLogin = await request(app).post('/api/auth/login').send({ username: 'viewer', password: 'viewer123' });
    expect(viewerLogin.status).toBe(200);
    const read = await request(app).get('/api/stats').set('Authorization', `Bearer ${viewerLogin.body.token}`);
    expect(read.status).toBe(200);
    const write = await request(app).patch('/api/assets/CSJ-0431').set('Authorization', `Bearer ${viewerLogin.body.token}`).send({ name: '不应保存' });
    expect(write.status).toBe(403);
  });
});
