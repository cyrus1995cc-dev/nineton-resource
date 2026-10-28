import { describe, expect, it } from 'vitest';
import request from 'supertest';
import { openDatabase } from './db';
import { createApp } from './index';
import { createUser } from './auth';
import { migrate } from './migrations';

async function setup() {
  const db = openDatabase(':memory:'); const app = createApp({ db, auth: true });
  for (const name of ['first', 'second', 'other', 'viewer']) createUser(db, { username: name, password: 'test123', display_name: name, role: name === 'viewer' ? 'viewer' : 'asset_manager' });
  db.prepare("INSERT INTO assets (asset_code, name, status, location, assignee, department) VALUES ('A-001', '电脑', '在用', '16楼', '张三', '研发部'), ('A-002', '显示器', '在用', '16楼', '张三', '研发部'), ('A-003', '电脑', '空闲', '12楼', '', '')").run();
  const users: Record<string, { id: number; token: string }> = {};
  for (const name of ['admin', 'first', 'second', 'other', 'viewer']) {
    const result = await request(app).post('/api/auth/login').send({ username: name, password: name === 'admin' ? 'admin123' : 'test123' });
    users[name] = { id: result.body.user.id, token: `Bearer ${result.body.token}` };
  }
  const input = { type: '归还', assetCodes: ['A-001', 'A-002'], applicantDepartment: '研发部', approverIds: [users.first.id, users.second.id], toLocation: '12楼', note: '项目结束', requestId: 'req-1', applicant_name: '冒用者' };
  const post = (path: string, name: string, body: object = {}) => request(app).post(path).set('Authorization', users[name].token).send(body);
  return { db, app, users, input, post };
}
describe('approval flows', () => {
  it('runs ordered approvals, records snapshots and changes, and queues each notification exactly once', async () => {
    const { db, app, input, post, users } = await setup();
    const created = await post('/api/flows', 'admin', input); expect(created.status).toBe(201);
    const id = created.body.flow.id;
    expect(created.body.flow).toMatchObject({ applicant_name: '系统管理员', status: 'pending', asset_count: 2 });
    expect(created.body.flow.document_no).toMatch(/^ZCTH\d{13}$/);
    expect(created.body.steps.map((s: any) => s.status)).toEqual(['pending', 'waiting']);
    expect(created.body.notifications).toHaveLength(1);
    expect((await post(`/api/flows/${id}/approve`, 'second')).status).toBe(403);
    expect((await post(`/api/flows/${id}/approve`, 'other')).status).toBe(403);
    expect((await post(`/api/flows/${id}/approve`, 'admin')).status).toBe(403);
    expect((await post(`/api/flows/${id}/approve`, 'first', { comment: '确认归还' })).body.flow.status).toBe('pending');
    expect((db.prepare('SELECT status FROM assets WHERE asset_code = ?').get('A-001') as any).status).toBe('在用');
    const todo = await request(app).get('/api/flows?scope=todo').set('Authorization', users.second.token);
    expect(todo.body.total).toBe(1);
    const done = await post(`/api/flows/${id}/approve`, 'second'); expect(done.body.flow.status).toBe('completed');
    expect(done.body.notifications.map((n: any) => [n.kind, n.status])).toEqual([['approval_pending', 'cancelled'], ['approval_pending', 'cancelled'], ['completed', 'pending']]);
    expect(done.body.items[0]).toMatchObject({ expected_status: '在用', expected_assignee: '张三', expected_location: '16楼' });
    const asset = (await request(app).get('/api/assets/A-001').set('Authorization', users.admin.token)).body;
    expect(asset).toMatchObject({ status: '空闲', assignee: '', department: '', location: '12楼' });
    expect(asset.events[0]).toMatchObject({ flow_id: id, document_no: done.body.flow.document_no, operator_name: 'second' });
    expect(asset.events[0].changes).toContainEqual({ field: 'location', before: '16楼', after: '12楼' });
    await post(`/api/flows/${id}/approve`, 'second'); await post(`/api/flows/${id}/approve`, 'first');
    expect(db.prepare('SELECT * FROM asset_events').all()).toHaveLength(2);
    expect(db.prepare('SELECT * FROM notification_outbox').all()).toHaveLength(3);
  });
  it('deduplicates requests and prevents pending asset overlap, invalid accounts and self approval', async () => {
    const { post, input, users, db } = await setup();
    expect((await post('/api/flows', 'admin', { ...input, approverIds: [users.admin.id] })).status).toBe(400);
    expect((await post('/api/flows', 'admin', { ...input, approverIds: [users.viewer.id] })).status).toBe(400);
    expect((await post('/api/flows', 'admin', { ...input, approverIds: [users.first.id, users.first.id] })).status).toBe(400);
    expect((await post('/api/flows', 'admin', { ...input, assetCodes: ['A-001', 'A-001'] })).status).toBe(400);
    expect((await post('/api/flows', 'admin', { ...input, assetCodes: ['A-003'] })).status).toBe(409);
    const created = await post('/api/flows', 'admin', input);
    const replay = await post('/api/flows', 'admin', input); expect(replay.status).toBe(200); expect(replay.body.flow.id).toBe(created.body.flow.id);
    expect((await post('/api/flows', 'admin', { ...input, note: '不同参数' })).status).toBe(409);
    expect((await post('/api/flows', 'other', { ...input, requestId: 'another' })).status).toBe(409);
    expect(db.prepare('SELECT * FROM asset_flows').all()).toHaveLength(1);
  });
  it('rejects and withdraws without changing assets, preserves comments and releases pending assets', async () => {
    const { post, input, db } = await setup();
    const id = (await post('/api/flows', 'admin', input)).body.flow.id;
    expect((await post(`/api/flows/${id}/withdraw`, 'other')).status).toBe(403);
    expect((await post(`/api/flows/${id}/reject`, 'first')).status).toBe(400);
    const reject = await post(`/api/flows/${id}/reject`, 'first', { comment: '请补充归还原因' });
    expect(reject.body.flow.status).toBe('rejected'); expect(reject.body.steps[1].status).toBe('cancelled');
    const commented = await post(`/api/flows/${id}/comments`, 'admin', { content: '已补充', user_name: '冒用者' });
    expect(commented.body.comments[0]).toMatchObject({ user_name: '系统管理员', content: '已补充' });
    const next = (await post('/api/flows', 'admin', { ...input, requestId: 'next' })).body.flow.id;
    const withdrawn = await post(`/api/flows/${next}/withdraw`, 'admin', { comment: '重填申请' });
    expect(withdrawn.body.flow.status).toBe('withdrawn'); expect(withdrawn.body.steps.every((s: any) => s.status === 'cancelled')).toBe(true);
    await post(`/api/flows/${next}/withdraw`, 'admin');
    expect(db.prepare('SELECT * FROM asset_events').all()).toHaveLength(0);
    expect((db.prepare('SELECT status FROM assets WHERE asset_code = ?').get('A-001') as any).status).toBe('在用');
  });
  it('detects asset changes before the final approval and rolls the entire action back', async () => {
    const { post, input, db } = await setup();
    const id = (await post('/api/flows', 'admin', input)).body.flow.id;
    await post(`/api/flows/${id}/approve`, 'first');
    db.prepare("UPDATE assets SET assignee = '李四' WHERE asset_code = 'A-002'").run();
    const result = await post(`/api/flows/${id}/approve`, 'second'); expect(result.status).toBe(409);
    expect(db.prepare('SELECT * FROM asset_events').all()).toHaveLength(0);
    expect((db.prepare('SELECT status FROM assets WHERE asset_code = ?').get('A-001') as any).status).toBe('在用');
    expect((db.prepare('SELECT status FROM asset_flow_steps WHERE flow_id = ? AND sequence = 2').get(id) as any).status).toBe('pending');
    expect((db.prepare("SELECT status FROM notification_outbox WHERE flow_id = ? AND recipient_name = 'second'").get(id) as any).status).toBe('pending');
  });
  it('rolls back all assets and approval history when the second audit insert fails', async () => {
    const { post, input, db } = await setup();
    const id = (await post('/api/flows', 'admin', input)).body.flow.id;
    await post(`/api/flows/${id}/approve`, 'first');
    db.exec("CREATE TRIGGER fail_second BEFORE INSERT ON asset_events WHEN NEW.asset_code = 'A-002' BEGIN SELECT RAISE(ABORT, 'test audit failure'); END");
    expect((await post(`/api/flows/${id}/approve`, 'second')).status).toBe(500);
    expect(db.prepare('SELECT * FROM asset_events').all()).toHaveLength(0);
    expect((db.prepare('SELECT status FROM assets WHERE asset_code = ?').get('A-001') as any).status).toBe('在用');
    expect((db.prepare('SELECT status FROM asset_flows WHERE id = ?').get(id) as any).status).toBe('pending');
    expect(db.prepare('SELECT * FROM asset_flow_logs WHERE flow_id = ?').all(id)).toHaveLength(2);
  });
  it('keeps viewer read only, requires login, and blocks authenticated direct event writes', async () => {
    const { post, input, app, users } = await setup(); const id = (await post('/api/flows', 'admin', input)).body.flow.id;
    expect((await request(app).get('/api/flows')).status).toBe(401);
    expect((await request(app).get(`/api/flows/${id}`).set('Authorization', users.viewer.token)).status).toBe(200);
    for (const path of ['/api/flows', `/api/flows/${id}/approve`, `/api/flows/${id}/withdraw`, `/api/flows/${id}/comments`]) expect((await post(path, 'viewer', input)).status).toBe(403);
    expect((await post('/api/assets/A-001/events', 'admin', { type: '归还' })).status).toBe(409);
    expect((await request(app).patch('/api/assets/A-001').set('Authorization', users.admin.token).send({ assignee: '跳过审批' })).status).toBe(409);
    const options = (await request(app).get('/api/flows/options').set('Authorization', users.admin.token)).body;
    expect(options.approvers.every((a: any) => !a.password_hash && a.role !== 'viewer')).toBe(true);
  });
  it('supports handover and loss and retains original QR through migration', async () => {
    const { post, input, db, app, users } = await setup();
    db.prepare("UPDATE assets SET qr_raw = 'https://old.example/A-001' WHERE asset_code = 'A-001'").run();
    migrate(db); migrate(db);
    const id = (await post('/api/flows', 'admin', { ...input, type: '交接', assetCodes: ['A-001'], approverIds: [users.first.id], toAssignee: '王五', toDepartment: '行政部' })).body.flow.id;
    await post(`/api/flows/${id}/approve`, 'first');
    const asset = (await request(app).get('/api/assets/A-001').set('Authorization', users.admin.token)).body;
    expect(asset).toMatchObject({ assignee: '王五', department: '行政部', status: '在用', qr_raw: 'https://old.example/A-001' });
    const lost = (await post('/api/flows', 'admin', { ...input, type: '报失', assetCodes: ['A-001'], requestId: 'lost', approverIds: [users.first.id] })).body.flow.id;
    expect((await post(`/api/flows/${lost}/approve`, 'first')).body.flow.status).toBe('completed');
    expect((db.prepare('SELECT status FROM assets WHERE asset_code = ?').get('A-001') as any).status).toBe('丢失');
  });
});
