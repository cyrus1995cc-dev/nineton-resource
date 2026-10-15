import { afterEach, describe, expect, it } from 'vitest';
import request from 'supertest';
import * as XLSX from 'xlsx';
import { openDatabase } from './db';
import { createApp } from './index';
import { createUser, type AuthUser } from './auth';
import { createFlow } from './repositories/flows';
import { createInventoryTask, recordInventoryScan, completeInventoryTask } from './repositories/inventory';
import { beijingTime } from './exports';
import { parseWorkbook, previewImport } from './importer';

const databases: ReturnType<typeof openDatabase>[] = [];
afterEach(() => { for (const db of databases.splice(0)) db.close(); });
async function setup() {
  const db = openDatabase(':memory:'); databases.push(db); const app = createApp({ db, auth: true });
  db.exec("INSERT INTO employees (name, department, dingtalk_user_id, corp_id) VALUES ('张三', '研发部', 'e1', 'corp'), ('李四', '行政部', 'e2', 'corp')");
  const employees = db.prepare('SELECT id FROM employees ORDER BY id').all();
  for (const [name, role, employee] of [['employee', 'employee', 0], ['approver', 'approver', 1], ['manager', 'asset_manager', null], ['viewer', 'viewer', null]] as const) {
    createUser(db, { username: name, password: 'test123', display_name: name, role });
    if (employee != null) db.prepare('UPDATE users SET employee_id = ? WHERE username = ?').run(employees[employee].id, name);
  }
  const insert = db.prepare('INSERT INTO assets (asset_code, name, status, location, assignee, department, assignee_employee_id, serial_number, amount_taxed, qr_raw) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)');
  for (let i = 1; i <= 26; i++) insert.run(`EXP-${String(i).padStart(3, '0')}`, i === 1 ? '=1+1' : `验收电脑${i}`, i <= 25 ? '在用' : '空闲', i <= 25 ? '12楼' : '16楼', i <= 25 ? '张三' : '', i <= 25 ? '研发部' : '', i <= 24 ? employees[0].id : null, '00001234567890123456', i === 1 ? 1234.5 : 0, i === 1 ? 'https://old.example/asset?code=EXP-001' : null);
  const users: Record<string, { token: string; actor: AuthUser }> = {};
  for (const name of ['admin', 'employee', 'approver', 'manager', 'viewer']) {
    const login = await request(app).post('/api/auth/login').send({ username: name, password: name === 'admin' ? 'admin123' : 'test123' });
    users[name] = { token: `Bearer ${login.body.token}`, actor: login.body.user };
  }
  const download = async (path: string, name = 'admin') => {
    const response = await request(app).get(path).set('Authorization', users[name].token).buffer(true).parse((res, callback) => {
      const chunks: Buffer[] = []; res.on('data', (chunk) => chunks.push(Buffer.from(chunk))); res.on('end', () => callback(null, Buffer.concat(chunks)));
    });
    expect(response.status).toBe(200); expect(response.headers['content-type']).toContain('spreadsheetml.sheet');
    return { response, book: XLSX.read(response.body, { type: 'buffer', cellNF: true }), buffer: response.body as Buffer };
  };
  return { db, app, users, download };
}
function rows(book: XLSX.WorkBook, name: string) { return XLSX.utils.sheet_to_json<Record<string, any>>(book.Sheets[name], { defval: '' }); }

describe('Excel exports', () => {
  it('exports every matching asset beyond the current page, preserving literal text, numbers, QR and import headers', async () => {
    const { db, download } = await setup(); const before = db.prepare('SELECT * FROM assets ORDER BY id').all();
    const { book, response, buffer } = await download('/api/exports/assets?location=12%E6%A5%BC&page=2&pageSize=1');
    expect(rows(book, '资产清单')).toHaveLength(25);
    const record = rows(book, '资产清单').find((r) => r['资产编码'] === 'EXP-001')!;
    expect(record).toMatchObject({ 资产名称: '=1+1', 设备序列号: '00001234567890123456', '购置/租赁金额（含税）': 1234.5, 二维码原文: 'https://old.example/asset?code=EXP-001' });
    expect(Object.values(book.Sheets['资产清单']).filter((cell: any) => cell && typeof cell === 'object' && cell.f)).toHaveLength(0);
    expect(book.Sheets['资产清单']['!autofilter']).toBeTruthy();
    expect(response.headers['content-disposition']).toContain("filename*=UTF-8''"); expect(response.headers['cache-control']).toBe('no-store');
    expect(previewImport(db, parseWorkbook(buffer)).unchangedRows).toHaveLength(25);
    expect(db.prepare('SELECT * FROM assets ORDER BY id').all()).toEqual(before);
    expect(db.prepare("SELECT actor_name, target FROM audit_logs WHERE action = 'export'").get()).toMatchObject({ actor_name: '系统管理员', target: '资产清单' });
    expect(rows((await download('/api/exports/assets?search=NOT-EXISTS')).book, '资产清单')).toHaveLength(0);
  });
  it('applies employee identity and inventory restrictions even if export scopes are tampered with', async () => {
    const { app, users, download } = await setup();
    const mine = rows((await download('/api/exports/assets?scope=mine', 'employee')).book, '资产清单');
    expect(mine).toHaveLength(24); expect(mine.some((r) => r['资产编码'] === 'EXP-025')).toBe(false);
    expect(rows((await download('/api/exports/assets?scope=all', 'employee')).book, '资产清单')).toHaveLength(24);
    expect(rows((await download('/api/exports/assets?scope=available', 'employee')).book, '资产清单').map((r) => r['资产编码'])).toEqual(['EXP-026']);
    expect(rows((await download('/api/exports/assets', 'viewer')).book, '资产清单')).toHaveLength(26);
    expect((await request(app).get('/api/exports/assets')).status).toBe(401);
    expect((await request(app).get('/api/exports/assets?search=a&search=b').set('Authorization', users.admin.token)).status).toBe(400);
    expect((await request(app).get('/api/exports/inventory/1').set('Authorization', users.employee.token)).status).toBe(403);
    expect((await request(app).get('/api/exports/inventory/0').set('Authorization', users.admin.token)).status).toBe(400);
    expect((await request(app).get('/api/exports/inventory/99').set('Authorization', users.admin.token)).status).toBe(404);
  });
  it('exports all matching flows and only their authorized asset snapshots, steps and processing logs', async () => {
    const { db, users, download } = await setup();
    for (let i = 1; i <= 24; i++) createFlow(db, users.admin.actor, { type: '归还', assetCodes: [`EXP-${String(i).padStart(3, '0')}`], applicantDepartment: '研发部', approverIds: [i === 24 ? users.approver.actor.id : users.manager.actor.id], requestId: `export-flow-${i}` });
    const book = (await download('/api/exports/flows?status=pending')).book;
    expect(rows(book, '流转单据')).toHaveLength(24); expect(rows(book, '流转资产明细')).toHaveLength(24);
    expect(rows(book, '审批步骤')).toHaveLength(24); expect(rows(book, '处理记录')).toHaveLength(24);
    const scoped = (await download('/api/exports/flows?scope=todo', 'approver')).book;
    expect(rows(scoped, '流转单据')).toHaveLength(1);
    expect(rows(scoped, '流转资产明细').map((r) => r['资产编码'])).toEqual(['EXP-024']);
    expect(rows(scoped, '审批步骤').map((r) => r['指定审批人'])).toEqual(['approver']);
    expect(rows(scoped, '处理记录')).toHaveLength(1);
    expect(rows((await download('/api/exports/flows?scope=all', 'employee')).book, '流转单据')).toHaveLength(0);
    expect(rows((await download('/api/exports/flows?search=EXP-001')).book, '流转单据')).toHaveLength(1);
  });
  it('distinguishes pending from missing and exports frozen inventory snapshots and reconciled differences', async () => {
    const { db, download } = await setup();
    const created = createInventoryTask(db, { name: '12楼验收盘点', location: '12楼', operatorName: 'manager' });
    if (created.status !== 'created') throw new Error('fixture'); const id = Number(created.task.id);
    recordInventoryScan(db, id, { assetCode: 'EXP-001', actualLocation: '12楼', operatorName: 'manager' });
    recordInventoryScan(db, id, { assetCode: 'EXP-002', actualLocation: '16楼', note: '待核实', operatorName: 'manager' });
    recordInventoryScan(db, id, { assetCode: 'EXP-002', actualLocation: '16楼', note: '待核实', operatorName: 'manager' });
    db.prepare("UPDATE assets SET location = '11楼', name = '后改名称' WHERE asset_code = 'EXP-002'").run();
    const active = (await download(`/api/exports/inventory/${id}`)).book;
    expect(rows(active, '任务概况')[0]).toMatchObject({ 应盘数量: 25, 实际已盘: 2, 待盘数量: 23, 未盘到: 0, 差异总数: 1 });
    expect(rows(active, '差异清单')).toHaveLength(1);
    expect(rows(active, '差异清单')[0]).toMatchObject({ 资产名称: '验收电脑2', 任务创建时位置: '12楼', 实盘位置: '16楼', 盘点结果: '位置差异' });
    completeInventoryTask(db, id, '系统管理员'); const done = (await download(`/api/exports/inventory/${id}`, 'viewer')).book;
    expect(rows(done, '任务概况')[0]).toMatchObject({ 实际已盘: 2, 待盘数量: 0, 未盘到: 23, 差异总数: 24, 任务状态: '已结束' });
    expect(rows(done, '全部明细')).toHaveLength(25); expect(rows(done, '差异清单')).toHaveLength(24);
    expect(rows(done, '差异清单').find((r) => r['盘点结果'] === '未盘到')).toMatchObject({ 实盘位置: '', '实盘时间（北京时间）': '' });
    expect(beijingTime('2026-10-15 00:00:00')).toBe('2026-10-15 08:00:00');
  });
});
