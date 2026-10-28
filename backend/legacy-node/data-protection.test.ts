import { describe, expect, it } from 'vitest';
import request from 'supertest';
import * as XLSX from 'xlsx';
import { DatabaseSync } from 'node:sqlite';
import { openDatabase } from './db';
import { migrate } from './migrations';
import { createApp } from './index';
import { createUser } from './auth';
import { parseQr } from '../../frontend/shared/qr';

async function setup() {
  const db = openDatabase(':memory:'); const app = createApp({ db, auth: true });
  db.prepare("INSERT INTO assets (asset_code, name, status, assignee, department, location, qr_raw, qr_format) VALUES ('AUD-001', '电脑', '在用', '张三', '研发部', '12楼', 'https://d.gongbeiyun.com/a/old/AUD-001', 'gongbeiyun_url')").run();
  const login = await request(app).post('/api/auth/login').send({ username: 'admin', password: 'admin123' });
  const auth = `Bearer ${login.body.token}`;
  const preview = (rows: object[]) => {
    const book = XLSX.utils.book_new(); XLSX.utils.book_append_sheet(book, XLSX.utils.json_to_sheet(rows), '资产');
    return request(app).post('/api/imports/preview').set('Authorization', auth).attach('file', XLSX.write(book, { type: 'buffer', bookType: 'xlsx' }), 'test.xlsx');
  };
  const confirm = (batchId: string, extra = {}) => request(app).post('/api/imports/confirm').set('Authorization', auth).send({ batchId, ...extra });
  return { db, app, auth, preview, confirm };
}
describe('data protection', () => {
  it('rejects ownership imports even during pending approvals and logs ordinary field updates', async () => {
    const { db, app, auth, preview, confirm } = await setup();
    const approver = createUser(db, { username: 'approver', display_name: '审批', password: 'test123', role: 'approver' });
    const created = await request(app).post('/api/flows').set('Authorization', auth).send({ type: '归还', assetCodes: ['AUD-001'], applicantDepartment: '研发部', approverIds: [(approver as any).user.id], requestId: 'pending-import' });
    expect(created.status).toBe(201);
    const p = await preview([{ 资产编码: 'AUD-001', 资产状态: '空闲', 使用人: '李四' }]);
    expect(p.body.preview.invalidRows).toBe(1); await confirm(p.body.batchId);
    expect(db.prepare('SELECT status, assignee FROM assets').get()).toMatchObject({ status: '在用', assignee: '张三' });
    expect(db.prepare('SELECT * FROM asset_events').all()).toHaveLength(0);
    const valid = await preview([{ 资产编码: 'AUD-001', 资产名称: '资料更正' }]);
    expect(valid.body.changes[0].fields).toContainEqual({ field: 'name', before: '电脑', after: '资料更正' });
    await confirm(valid.body.batchId);
    const event = db.prepare('SELECT * FROM asset_events').get()!;
    expect(event).toMatchObject({ type: '导入', operator_name: '系统管理员' });
    expect(JSON.parse(String(event.changes_json))).toContainEqual({ field: 'name', before: '电脑', after: '资料更正' }); db.close();
  });
  it('ignores tampered confirm rows and deduplicates repeated confirmation', async () => {
    const { db, preview, confirm } = await setup();
    const p = await preview([{ 资产编码: 'NEW-000001', 资产名称: '新增设备' }]);
    expect(p.body).not.toHaveProperty('rows');
    expect((await confirm(p.body.batchId, { rows: [{ asset_code: 'AUD-001', status: '报废' }] })).status).toBe(200);
    await confirm(p.body.batchId);
    expect(db.prepare('SELECT * FROM import_batches').all()).toHaveLength(1);
    expect(db.prepare('SELECT name FROM assets WHERE asset_code = ?').get('NEW-000001')?.name).toBe('新增设备');
    expect(db.prepare('SELECT status FROM assets WHERE asset_code = ?').get('AUD-001')?.status).toBe('在用');
    expect(parseQr('NEW-000001').assetCode).toBe('NEW-000001'); db.close();
  });
  it('rejects expired or foreign previews and detects changes after preview', async () => {
    const { db, app, auth, preview, confirm } = await setup();
    const p = await preview([{ 资产编码: 'AUD-001', 资产名称: '导入名称' }]);
    db.prepare('UPDATE import_previews SET actor_id = 999 WHERE id = ?').run(p.body.batchId);
    expect((await confirm(p.body.batchId)).status).toBe(404);
    db.prepare('UPDATE import_previews SET actor_id = 1, expires_at = 1 WHERE id = ?').run(p.body.batchId);
    expect((await confirm(p.body.batchId)).status).toBe(409);
    const p2 = await preview([{ 资产编码: 'AUD-001', 资产名称: '导入名称' }]);
    expect((await request(app).patch('/api/assets/AUD-001').set('Authorization', auth).send({ name: '他人新名称', expectedVersion: 1 })).status).toBe(200);
    expect((await confirm(p2.body.batchId)).status).toBe(409);
    expect(db.prepare('SELECT name FROM assets').get()?.name).toBe('他人新名称'); db.close();
  });
  it('rejects stale editing and invalid inputs, preserving newer values', async () => {
    const { db, app, auth } = await setup();
    const patch = (body: object) => request(app).patch('/api/assets/AUD-001').set('Authorization', auth).send(body);
    expect((await patch({ name: '' , expectedVersion: 1 })).status).toBe(400);
    expect((await patch({ amount_taxed: -123, expectedVersion: 1 })).status).toBe(400);
    expect((await patch({ model: [], expectedVersion: 1 })).status).toBe(400);
    expect((await patch({ name: '新名称' })).status).toBe(400);
    expect((await patch({ name: '新名称', expectedVersion: 1 })).status).toBe(200);
    expect((await patch({ name: '电脑', model: '新型号', expectedVersion: 1 })).status).toBe(409);
    expect((await request(app).get('/api/assets?page=wrong').set('Authorization', auth)).status).toBe(400);
    expect((await request(app).post('/api/auth/users').set('Authorization', auth).send({})).status).toBe(400);
    expect(db.prepare('SELECT name, version FROM assets').get()).toMatchObject({ name: '新名称', version: 2 }); db.close();
  });
  it('preserves original label URLs and separately records alternate scans', async () => {
    const { db, app, auth } = await setup();
    const saved = await request(app).post('/api/assets/AUD-001/qr').set('Authorization', auth).send({ raw: 'https://example.invalid/AUD-001' });
    expect(saved.body.qr_raw).toBe('https://d.gongbeiyun.com/a/old/AUD-001');
    expect(db.prepare('SELECT raw FROM asset_qr_aliases').get()?.raw).toBe('https://example.invalid/AUD-001'); db.close();
  });
  it('migrates old role constraints without losing accounts, sessions or referencing history', () => {
    const db = new DatabaseSync(':memory:');
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, display_name TEXT NOT NULL, role TEXT CHECK(role IN ('admin','asset_manager','viewer')), dingtalk_user_id TEXT UNIQUE, active INTEGER DEFAULT 1, created_at TEXT DEFAULT CURRENT_TIMESTAMP, updated_at TEXT DEFAULT CURRENT_TIMESTAMP); INSERT INTO users (username,password_hash,display_name,role) VALUES ('legacy','hash','旧管理员','admin')");
    db.exec("CREATE TABLE legacy_records (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id)); INSERT INTO legacy_records VALUES (1,1); CREATE TABLE sessions (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, token_hash TEXT NOT NULL UNIQUE, expires_at INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP); INSERT INTO sessions(user_id,token_hash,expires_at) VALUES(1,'old-session',9999999999999);");
    migrate(db);
    expect(db.prepare('SELECT user_id FROM legacy_records').get()?.user_id).toBe(1);
    expect(db.prepare("SELECT user_id FROM sessions WHERE token_hash = 'old-session'").get()?.user_id).toBe(1);
    db.prepare('INSERT INTO sessions (user_id,token_hash,expires_at) VALUES (1,?,?)').run('session', Date.now() + 1000);
    migrate(db);
    expect(db.prepare('SELECT username FROM users WHERE id = 1').get()?.username).toBe('legacy');
    expect(db.prepare('PRAGMA foreign_key_check').all()).toEqual([]);
    expect(createUser(db, { username: 'employee', display_name: '员工', password: 'test123', role: 'employee' }).status).toBe('created'); db.close();
  });
});
