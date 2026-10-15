import { afterEach, describe, expect, it, vi } from 'vitest';
import request from 'supertest';
import { openDatabase } from './db';
import { createApp } from './index';
import { createUser } from './auth';
import { DingTalkClient } from './dingtalk';
import { syncDirectory } from './repositories/directory';

function config() { vi.stubEnv('DINGTALK_CLIENT_ID', 'app'); vi.stubEnv('DINGTALK_CLIENT_SECRET', 'secret'); vi.stubEnv('DINGTALK_CORP_ID', 'corp'); }
function directoryFetch(people = [{ userid: 'u1', unionid: 'union1', name: '张三', job_number: '001' }], fail = false) {
  return vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
    const path = String(url); const body = JSON.parse(String(init?.body ?? '{}'));
    if (path.includes('/oauth2/accessToken')) return Response.json({ accessToken: 'app-token', expireIn: 7200 });
    if (fail && path.includes('/user/list')) return Response.json({ errcode: 50004, errmsg: 'token secret must not leak' });
    if (path.includes('/department/get')) return Response.json({ errcode: 0, result: { name: body.dept_id === 1 ? '公司' : '研发部', parent_id: body.dept_id === 1 ? null : 1 } });
    if (path.includes('/department/listsub')) return Response.json({ errcode: 0, result: body.dept_id === 1 ? [{ dept_id: 2, name: '研发部' }] : [] });
    return Response.json({ errcode: 0, result: { list: body.dept_id === 2 ? people : [], has_more: false } });
  });
}
async function setup() {
  const db = openDatabase(':memory:'); const app = createApp({ db, auth: true });
  const admin = (await request(app).post('/api/auth/login').send({ username: 'admin', password: 'admin123' })).body;
  return { db, app, auth: `Bearer ${admin.token}`, admin: admin.user };
}
describe('directory and employee access', () => {
  afterEach(() => { vi.unstubAllEnvs(); vi.unstubAllGlobals(); });
  it('syncs departments and employees, preserves role changes, caches app tokens and keeps failed sync atomic', async () => {
    config(); const { db, admin } = await setup(); const fetcher = directoryFetch(); const client = new DingTalkClient(fetcher);
    expect(await syncDirectory(db, admin, client)).toEqual({ employees: 1, departments: 2 });
    expect(db.prepare('SELECT name, corp_id, union_id, dingtalk_user_id, department FROM employees').get()).toMatchObject({ name: '张三', corp_id: 'corp', union_id: 'union1', dingtalk_user_id: 'u1', department: '研发部' });
    db.prepare("UPDATE users SET role = 'approver' WHERE employee_id IS NOT NULL").run();
    await syncDirectory(db, admin, client);
    expect(fetcher.mock.calls.filter(([url]) => String(url).includes('/oauth2/accessToken'))).toHaveLength(1);
    expect(db.prepare('SELECT role FROM users WHERE employee_id IS NOT NULL').get()?.role).toBe('approver');
    await expect(syncDirectory(db, admin, new DingTalkClient(directoryFetch([], true)))).rejects.toThrow('50004');
    expect(db.prepare('SELECT * FROM employees').all()).toHaveLength(1);
    expect(db.prepare('SELECT missing_since FROM employees').get()?.missing_since).toBeNull();
    expect(db.prepare('SELECT message FROM directory_sync_runs ORDER BY id DESC LIMIT 1').get()?.message).not.toContain('secret'); db.close();
  });
  it('marks disappeared people for review without declaring departure or reactivating disabled people', async () => {
    config(); const { db, admin } = await setup();
    await syncDirectory(db, admin, new DingTalkClient(directoryFetch()));
    await syncDirectory(db, admin, new DingTalkClient(directoryFetch([])));
    expect(db.prepare('SELECT active, missing_since FROM employees').get()).toMatchObject({ active: 1, missing_since: expect.any(String) });
    db.prepare('UPDATE employees SET active = 0').run();
    await syncDirectory(db, admin, new DingTalkClient(directoryFetch()));
    expect(db.prepare('SELECT active, missing_since FROM employees').get()).toMatchObject({ active: 0, missing_since: null }); db.close();
  });
  it('walks paginated department users, deduplicates memberships and aborts repeated cursors', async () => {
    config(); const { db, admin } = await setup();
    const base = directoryFetch(); let repeat = false;
    const fetcher = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
      if (!String(url).includes('/user/list')) return base(url, init);
      const body = JSON.parse(String(init?.body));
      if (body.dept_id === 1) return Response.json({ errcode: 0, result: { list: [], has_more: false } });
      return Response.json({ errcode: 0, result: { list: [{ userid: body.cursor ? 'u2' : 'u1', unionid: body.cursor ? 'union2' : 'union1', name: body.cursor ? '李四' : '张三' }], has_more: repeat || body.cursor === 0, next_cursor: 100 } });
    });
    expect(await syncDirectory(db, admin, new DingTalkClient(fetcher))).toEqual({ employees: 2, departments: 2 });
    expect(db.prepare('SELECT * FROM employees').all()).toHaveLength(2);
    repeat = true;
    await expect(syncDirectory(db, admin, new DingTalkClient(fetcher))).rejects.toThrow('游标异常');
    expect(db.prepare('SELECT * FROM employees WHERE missing_since IS NULL').all()).toHaveLength(2); db.close();
  });
  it('does not bind same-name assets automatically and requires a reviewed stable ID and current version', async () => {
    config(); const { db, app, auth, admin } = await setup();
    await syncDirectory(db, admin, new DingTalkClient(directoryFetch()));
    db.prepare("INSERT INTO employees(name,corp_id,dingtalk_user_id,department) VALUES ('张三','corp','u2','行政部')").run();
    db.prepare("INSERT INTO assets(asset_code,name,status,assignee,department) VALUES ('EMP-001','电脑','在用','张三','研发部')").run();
    const match = await request(app).get('/api/directory/mappings').set('Authorization', auth);
    expect(match.body.assets[0].candidates).toHaveLength(2);
    expect(db.prepare('SELECT assignee_employee_id FROM assets').get()?.assignee_employee_id).toBeNull();
    const path = '/api/directory/mappings/EMP-001';
    expect((await request(app).post(path).set('Authorization', auth).send({ employeeId: 1, expectedVersion: 9, reason: '核对工号' })).status).toBe(409);
    expect((await request(app).post(path).set('Authorization', auth).send({ employeeId: 1, expectedVersion: 1, reason: '核对工号' })).status).toBe(200);
    expect(db.prepare('SELECT assignee_employee_id FROM assets').get()?.assignee_employee_id).toBe(1);
    expect(db.prepare('SELECT type FROM asset_events').get()?.type).toBe('人员关联'); db.close();
  });
  it('limits employee assets, statistics and documents and supports approval without granting edit/import rights', async () => {
    const { db, app, auth } = await setup();
    db.prepare("INSERT INTO employees(name,department) VALUES ('张三','研发部'),('李四','行政部')").run();
    const e1 = createUser(db, { username: 'employee1', display_name: '张三', password: 'test123', role: 'employee' }) as any;
    const e2 = createUser(db, { username: 'employee2', display_name: '李四', password: 'test123', role: 'employee' }) as any;
    const a = createUser(db, { username: 'approver', display_name: '审批人', password: 'test123', role: 'approver' }) as any;
    db.prepare('UPDATE users SET employee_id = ? WHERE id = ?').run(1, e1.user.id); db.prepare('UPDATE users SET employee_id = ? WHERE id = ?').run(2, e2.user.id);
    db.prepare("INSERT INTO assets(asset_code,name,status,assignee,department,assignee_employee_id) VALUES ('ME-001','我的电脑','在用','张三','研发部',1),('OTHER-001','他人电脑','在用','李四','行政部',2),('IDLE-001','库存','空闲','','',NULL)").run();
    const login = async (username: string) => `Bearer ${(await request(app).post('/api/auth/login').send({ username, password: 'test123' })).body.token}`;
    const emp = await login('employee1'); const other = await login('employee2'); const approver = await login('approver');
    expect((await request(app).get('/api/assets?scope=mine').set('Authorization', emp)).body.items.map((a: any) => a.asset_code)).toEqual(['ME-001']);
    expect((await request(app).get('/api/stats').set('Authorization', emp)).body.total).toBe(1);
    expect((await request(app).get('/api/assets/OTHER-001').set('Authorization', emp)).status).toBe(404);
    expect((await request(app).get('/api/assets?scope=available').set('Authorization', emp)).body.total).toBe(1);
    expect((await request(app).get('/api/inventory/tasks').set('Authorization', emp)).status).toBe(403);
    expect((await request(app).post('/api/imports/confirm').set('Authorization', emp).send({})).status).toBe(403);
    expect((await request(app).patch('/api/assets/ME-001').set('Authorization', emp).send({ name: '越权', expectedVersion: 1 })).status).toBe(403);
    const input = { type: '归还', assetCodes: ['ME-001'], applicantDepartment: '冒用部门', approverIds: [a.user.id], requestId: 'my-return' };
    expect((await request(app).post('/api/flows').set('Authorization', emp).send({ ...input, assetCodes: ['OTHER-001'] })).status).toBe(403);
    const f = await request(app).post('/api/flows').set('Authorization', emp).send(input); expect(f.status).toBe(201); expect(f.body.flow.applicant_department).toBe('研发部');
    expect((await request(app).patch(`/api/auth/users/${a.user.id}`).set('Authorization', auth).send({ role: 'viewer', active: false })).status).toBe(409);
    const outbox = JSON.parse(String(db.prepare('SELECT payload_json FROM notification_outbox').get()?.payload_json)); expect(outbox.recipient.localUserId).toBe(a.user.id);
    expect((await request(app).get(`/api/flows/${f.body.flow.id}`).set('Authorization', other)).status).toBe(404);
    expect((await request(app).get('/api/flows').set('Authorization', other)).body.total).toBe(0);
    expect((await request(app).get('/api/assets/ME-001').set('Authorization', approver)).status).toBe(200);
    const done = await request(app).post(`/api/flows/${f.body.flow.id}/approve`).set('Authorization', approver).send({}); expect(done.status).toBe(200);
    expect(db.prepare('SELECT assignee_employee_id FROM assets WHERE asset_code = ?').get('ME-001')?.assignee_employee_id).toBeNull();
    const claim = await request(app).post('/api/flows').set('Authorization', emp).send({ ...input, type: '领用', assetCodes: ['IDLE-001'], toEmployeeId: 1, requestId: 'my-claim' }); expect(claim.status).toBe(201);
    await request(app).post(`/api/flows/${claim.body.flow.id}/approve`).set('Authorization', approver).send({});
    expect(db.prepare('SELECT assignee_employee_id FROM assets WHERE asset_code = ?').get('IDLE-001')?.assignee_employee_id).toBe(1);
    const update = await request(app).patch(`/api/auth/users/${e1.user.id}`).set('Authorization', auth).send({ role: 'viewer', active: true }); expect(update.status).toBe(200);
    expect((await request(app).get('/api/auth/me').set('Authorization', emp)).status).toBe(401); db.close();
  });
  it('rejects unrecognized or other-enterprise DingTalk identities', async () => {
    config(); const { db, app } = await setup();
    db.prepare("INSERT INTO employees(name,corp_id,union_id,dingtalk_user_id) VALUES ('外部员工','other-corp','external-union','u')").run();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(Response.json({ accessToken: 'user-token' })).mockResolvedValueOnce(Response.json({ unionId: 'external-union', nick: '外部员工' })));
    expect((await request(app).post('/api/auth/dingtalk/login').send({ code: 'code' })).status).toBe(403);
    expect(db.prepare('SELECT * FROM users WHERE employee_id IS NOT NULL').all()).toHaveLength(0); db.close();
  });
});
