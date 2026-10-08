import { describe, expect, it } from 'vitest';
import request from 'supertest';
import { openDatabase } from './db';
import { createApp } from './index';

function seededApp() {
  const db = openDatabase(':memory:');
  db.prepare(`INSERT INTO assets (asset_code, qr_raw, qr_format, name, status, category_path, location, company, assignee, department)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?), (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
    .run(
      'CSJ-0431', 'https://d.gongbeiyun.com/a/58b7d/57771/CSJ-0431', 'gongbeiyun_url', '测试机', '空闲', '计算机和网络设备/手机', '12楼', '重庆飞闪科技有限公司', '', '',
      'BJB-0051', 'BJB-0051', 'asset_code', '笔记本电脑', '在用', '计算机和网络设备/笔记本电脑', '16楼', '重庆赋比兴科技有限公司', '张三', '研发部',
    );
  return { app: createApp({ db }), db };
}

describe('asset API', () => {
  it('filters and paginates assets', async () => {
    const { app } = seededApp();
    const response = await request(app).get('/api/assets?status=空闲&page=1&pageSize=10');
    expect(response.status).toBe(200);
    expect(response.body.total).toBe(1);
    expect(response.body.items[0].asset_code).toBe('CSJ-0431');
  });

  it('returns details and timeline for a QR asset code', async () => {
    const { app } = seededApp();
    const response = await request(app).get('/api/assets/CSJ-0431');
    expect(response.status).toBe(200);
    expect(response.body.asset_code).toBe('CSJ-0431');
    expect(response.body.events).toEqual([]);
  });

  it('stores the exact scanned QR string without navigating to it', async () => {
    const { app } = seededApp();
    const raw = 'https://d.gongbeiyun.com/a/58b7d/57771/CSJ-0431';
    const saved = await request(app).post('/api/assets/CSJ-0431/qr').send({ raw });
    expect(saved.status).toBe(200);
    expect(saved.body.qr_raw).toBe(raw);
    expect(saved.body.qr_format).toBe('gongbeiyun_url');
  });

  it('edits asset fields while keeping the asset code immutable', async () => {
    const { app } = seededApp();
    const response = await request(app).patch('/api/assets/CSJ-0431').send({
      asset_code: 'SHOULD-NOT-CHANGE', name: '办公测试机', model: 'iPhone 15', location: '16楼', assignee: '王五', department: '行政部',
      amount_taxed: 2999, operatorName: '管理员',
    });
    expect(response.status).toBe(200);
    expect(response.body.asset.asset_code).toBe('CSJ-0431');
    expect(response.body.asset.name).toBe('办公测试机');
    expect(response.body.asset.location).toBe('16楼');
    expect(response.body.asset.amount_taxed).toBe(2999);
    expect(response.body.asset.events[0].type).toBe('编辑');
  });

  it('records an idempotent claim and return event', async () => {
    const { app } = seededApp();
    const claim = await request(app).post('/api/assets/CSJ-0431/events').send({
      type: '领用', operatorName: '管理员', toAssignee: '李四', toDepartment: '测试部', requestId: 'claim-1',
    });
    expect(claim.status).toBe(201);
    expect(claim.body.asset.status).toBe('在用');
    expect(claim.body.asset.assignee).toBe('李四');

    const duplicate = await request(app).post('/api/assets/CSJ-0431/events').send({
      type: '领用', operatorName: '管理员', toAssignee: '李四', toDepartment: '测试部', requestId: 'claim-1',
    });
    expect(duplicate.status).toBe(200);
    expect(duplicate.body.replayed).toBe(true);

    const returned = await request(app).post('/api/assets/CSJ-0431/events').send({
      type: '归还', operatorName: '管理员', requestId: 'return-1',
    });
    expect(returned.status).toBe(201);
    expect(returned.body.asset.status).toBe('空闲');
    expect(returned.body.asset.assignee).toBe('');
  });

  it('returns dashboard statistics', async () => {
    const { app } = seededApp();
    const response = await request(app).get('/api/stats');
    expect(response.status).toBe(200);
    expect(response.body.total).toBe(2);
    expect(response.body.status).toEqual({ 在用: 1, 空闲: 1 });
  });
});
