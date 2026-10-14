import { afterEach, describe, expect, it, vi } from 'vitest';
import request from 'supertest';
import { openDatabase } from './db';
import { createApp } from './index';
import * as XLSX from 'xlsx';

describe('authentication API', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.unstubAllEnvs(); });
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

  it('previews and confirms an authenticated Excel upload', async () => {
    const db = openDatabase(':memory:'); const app = createApp({ db, auth: true });
    const login = await request(app).post('/api/auth/login').send({ username: 'admin', password: 'admin123' });
    const workbook = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(workbook, XLSX.utils.json_to_sheet([{ 资产编码: 'IMP-001', 资产名称: '导入验收设备', 资产状态: '空闲' }]), '资产');
    const buffer = XLSX.write(workbook, { type: 'buffer', bookType: 'xlsx' });
    const preview = await request(app).post('/api/imports/preview').set('Authorization', `Bearer ${login.body.token}`).attach('file', buffer, 'qa.xlsx');
    expect(preview.status).toBe(200);
    expect(preview.body.preview.newRows).toBe(1);
    expect(db.prepare('SELECT * FROM assets').all()).toHaveLength(0);
    const confirmed = await request(app).post('/api/imports/confirm').set('Authorization', `Bearer ${login.body.token}`).send(preview.body);
    expect(confirmed.status).toBe(200);
    expect((await request(app).get('/api/assets/IMP-001').set('Authorization', `Bearer ${login.body.token}`)).body.name).toBe('导入验收设备');
    db.close();
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

  it('exposes DingTalk configuration without exposing secrets', async () => {
    const db = openDatabase(':memory:');
    const response = await request(createApp({ db, auth: true })).get('/api/auth/dingtalk/config');
    expect(response.status).toBe(200);
    expect(response.body).toEqual({ enabled: false, corpId: '' });
    expect(response.body).not.toHaveProperty('clientSecret');
  });

  it('exchanges a DingTalk auth code and creates a viewer session', async () => {
    const db = openDatabase(':memory:');
    const app = createApp({ db, auth: true });
    vi.stubEnv('DINGTALK_CLIENT_ID', 'client-id');
    vi.stubEnv('DINGTALK_CLIENT_SECRET', 'client-secret');
    vi.stubEnv('DINGTALK_CORP_ID', 'corp-id');
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ accessToken: 'user-access-token' }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ unionId: 'ding-union-1', nick: '钉钉用户' }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    const login = await request(app).post('/api/auth/dingtalk/login').send({ code: 'auth-code' });
    expect(login.status).toBe(200);
    expect(login.body.user).toMatchObject({ display_name: '钉钉用户', role: 'viewer' });
    expect(fetchMock).toHaveBeenCalledTimes(2);

    const me = await request(app).get('/api/auth/me').set('Authorization', `Bearer ${login.body.token}`);
    expect(me.status).toBe(200);
    expect(me.body.user.username).toMatch(/^ding_/);
  });
});
