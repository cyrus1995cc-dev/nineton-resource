import { describe, expect, it } from 'vitest';
import request from 'supertest';
import { openDatabase } from './db';
import { createApp } from './index';

function seededApp(auth = false) {
  const db = openDatabase(':memory:');
  db.prepare(`INSERT INTO assets (asset_code, name, status, location, department)
    VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)`)
    .run(
      'INV-001', '办公电脑', '在用', '12楼', '研发部',
      'INV-002', '备用电脑', '空闲', '12楼', '研发部',
      'INV-003', '会议投影', '在用', '16楼', '行政部',
    );
  return { app: createApp({ db, auth }), db };
}

describe('inventory API', () => {
  it('rejects invalid task IDs and empty task scopes', async () => {
    const { app } = seededApp();
    expect((await request(app).get('/api/inventory/tasks/not-a-number')).status).toBe(400);
    expect((await request(app).post('/api/inventory/tasks/0/complete').send({})).status).toBe(400);
    expect((await request(app).post('/api/inventory/tasks').send({ name: ' ' })).status).toBe(400);
    expect((await request(app).post('/api/inventory/tasks').send({ name: '空范围', location: '不存在' })).status).toBe(400);
  });
  it('creates a task with a materialized asset scope', async () => {
    const { app } = seededApp();
    const created = await request(app).post('/api/inventory/tasks').send({
      name: '12楼季度盘点', location: '12楼', operatorName: '管理员',
    });
    expect(created.status).toBe(201);
    expect(created.body.task.name).toBe('12楼季度盘点');
    expect(created.body.task.total_count).toBe(2);
    expect(created.body.records).toHaveLength(2);
    expect(created.body.records[0].result).toBe('pending');
  });

  it('records a found scan and reports remaining differences', async () => {
    const { app } = seededApp();
    const created = await request(app).post('/api/inventory/tasks').send({ name: '快速盘点', location: '12楼' });
    const taskId = created.body.task.id;
    const scanned = await request(app).post(`/api/inventory/tasks/${taskId}/records`).send({
      assetCode: 'INV-001', result: 'found', actualLocation: '12楼', operatorName: '张三',
    });
    expect(scanned.status).toBe(200);
    expect(scanned.body.record.result).toBe('found');
    expect(scanned.body.task.checked_count).toBe(1);
    expect(scanned.body.task.difference_count).toBe(0);

    const detail = await request(app).get(`/api/inventory/tasks/${taskId}`);
    expect(detail.status).toBe(200);
    expect(detail.body.records.find((row: any) => row.asset_code === 'INV-001').checked_at).toBeTruthy();
    expect(detail.body.records.find((row: any) => row.asset_code === 'INV-002').result).toBe('pending');
  });

  it('rejects a scan for an asset outside the task scope', async () => {
    const { app } = seededApp();
    const created = await request(app).post('/api/inventory/tasks').send({ name: '12楼盘点', location: '12楼' });
    const response = await request(app).post(`/api/inventory/tasks/${created.body.task.id}/records`).send({
      assetCode: 'INV-003', result: 'found', operatorName: '张三',
    });
    expect(response.status).toBe(400);
    expect(response.body.message).toContain('不在本次盘点范围');
  });

  it('marks a task complete and keeps missing assets as differences', async () => {
    const { app } = seededApp();
    const created = await request(app).post('/api/inventory/tasks').send({ name: '12楼盘点', location: '12楼' });
    const taskId = created.body.task.id;
    await request(app).post(`/api/inventory/tasks/${taskId}/records`).send({ assetCode: 'INV-001', result: 'found' });
    const completed = await request(app).post(`/api/inventory/tasks/${taskId}/complete`).send({ operatorName: '管理员' });
    expect(completed.status).toBe(200);
    expect(completed.body.task.status).toBe('completed');
    expect(completed.body.task.difference_count).toBe(1);
    expect(completed.body.task.checked_count).toBe(1);
    expect(completed.body.records.find((row: any) => row.asset_code === 'INV-002').result).toBe('missing');
    expect(completed.body.records.find((row: any) => row.asset_code === 'INV-002').checked_at).toBeNull();
  });

  it('audits findings and missing results, avoids duplicate scans and never moves the asset', async () => {
    const { app, db } = seededApp();
    const created = await request(app).post('/api/inventory/tasks').send({ name: '12楼盘点', location: '12楼' });
    const taskId = created.body.task.id;
    const input = { assetCode: 'INV-001', actualLocation: '16楼', note: '发现位置不同', operatorName: '张三' };
    const scanned = await request(app).post(`/api/inventory/tasks/${taskId}/records`).send(input);
    expect(scanned.body.record.result).toBe('different');
    const duplicate = await request(app).post(`/api/inventory/tasks/${taskId}/records`).send(input);
    expect(duplicate.body.replayed).toBe(true);
    expect(duplicate.body.record.checked_at).toBe(scanned.body.record.checked_at);
    await request(app).post(`/api/inventory/tasks/${taskId}/complete`).send({ operatorName: '管理员' });
    await request(app).post(`/api/inventory/tasks/${taskId}/complete`).send({ operatorName: '管理员' });
    const events = db.prepare("SELECT * FROM asset_events WHERE type = '盘点' ORDER BY id").all();
    expect(events).toHaveLength(2);
    expect(events[0]).toMatchObject({ operator_name: '张三', from_location: '12楼', to_location: '12楼' });
    expect(String(events[0].note)).toContain('16楼');
    expect(String(events[1].note)).toContain('未盘到');
    expect((await request(app).get('/api/assets/INV-001')).body).toMatchObject({ location: '12楼', status: '在用' });
    expect((await request(app).post(`/api/inventory/tasks/${taskId}/records`).send(input)).status).toBe(409);
  });

  it('rolls back inventory records and counters if the audit cannot be saved', async () => {
    const { app, db } = seededApp();
    const created = await request(app).post('/api/inventory/tasks').send({ name: '原子盘点', location: '12楼' });
    const taskId = created.body.task.id;
    db.exec("CREATE TRIGGER fail_audit BEFORE INSERT ON asset_events BEGIN SELECT RAISE(ABORT, 'test audit failure'); END");
    expect((await request(app).post(`/api/inventory/tasks/${taskId}/records`).send({ assetCode: 'INV-001' })).status).toBe(500);
    expect((await request(app).get(`/api/inventory/tasks/${taskId}`)).body.task.checked_count).toBe(0);
    expect((await request(app).post(`/api/inventory/tasks/${taskId}/complete`).send({})).status).toBe(500);
    const detail = (await request(app).get(`/api/inventory/tasks/${taskId}`)).body;
    expect(detail.task.status).toBe('in_progress');
    expect(detail.records.every((row: any) => row.result === 'pending')).toBe(true);
  });

  it('uses the logged-in identity for inventory, edits and flow events, and denies viewer writes', async () => {
    const { app } = seededApp(true);
    const admin = (await request(app).post('/api/auth/login').send({ username: 'admin', password: 'admin123' })).body;
    const authorization = `Bearer ${admin.token}`;
    const created = await request(app).post('/api/inventory/tasks').set('Authorization', authorization).send({ name: '身份盘点', operatorName: '冒用者' });
    expect(created.body.task.operator_name).toBe(admin.user.display_name);
    const scanned = await request(app).post(`/api/inventory/tasks/${created.body.task.id}/records`).set('Authorization', authorization).send({ assetCode: 'INV-001', operatorName: '冒用者' });
    expect(scanned.body.record.operator_name).toBe(admin.user.display_name);
    const edited = await request(app).patch('/api/assets/INV-001').set('Authorization', authorization).send({ name: '新名称', operatorName: '冒用者' });
    expect(edited.body.event.operator_name).toBe(admin.user.display_name);
    const moved = await request(app).post('/api/assets/INV-001/events').set('Authorization', authorization).send({ type: '调拨', toLocation: '16楼', operatorName: '冒用者' });
    expect(moved.status).toBe(409);
    await request(app).post('/api/auth/users').set('Authorization', authorization).send({ username: 'readonly', password: 'readonly123', display_name: '访客', role: 'viewer' });
    const viewer = (await request(app).post('/api/auth/login').send({ username: 'readonly', password: 'readonly123' })).body;
    const readAuthorization = `Bearer ${viewer.token}`;
    expect((await request(app).get('/api/assets/INV-001').set('Authorization', readAuthorization)).status).toBe(200);
    expect((await request(app).get(`/api/inventory/tasks/${created.body.task.id}`).set('Authorization', readAuthorization)).status).toBe(200);
    for (const path of ['/api/inventory/tasks', `/api/inventory/tasks/${created.body.task.id}/records`, `/api/inventory/tasks/${created.body.task.id}/complete`, '/api/assets/INV-001/qr', '/api/imports/preview']) {
      expect((await request(app).post(path).set('Authorization', readAuthorization).send({ assetCode: 'INV-001', raw: 'INV-001', name: '无权限任务' })).status).toBe(403);
    }
  });
});
