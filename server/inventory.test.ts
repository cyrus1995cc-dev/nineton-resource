import { describe, expect, it } from 'vitest';
import request from 'supertest';
import { openDatabase } from './db';
import { createApp } from './index';

function seededApp() {
  const db = openDatabase(':memory:');
  db.prepare(`INSERT INTO assets (asset_code, name, status, location, department)
    VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)`)
    .run(
      'INV-001', '办公电脑', '在用', '12楼', '研发部',
      'INV-002', '备用电脑', '空闲', '12楼', '研发部',
      'INV-003', '会议投影', '在用', '16楼', '行政部',
    );
  return { app: createApp({ db }), db };
}

describe('inventory API', () => {
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
    expect(completed.body.records.find((row: any) => row.asset_code === 'INV-002').result).toBe('missing');
  });
});
