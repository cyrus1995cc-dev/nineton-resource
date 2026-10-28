import type { DatabaseSync } from 'node:sqlite';
import type { AuthUser } from '../auth';
import { ensureEmployeeUser } from '../auth';
import { AppError, boundedText, integer } from '../errors';
import { DingTalkClient } from '../dingtalk';

type Department = { dept_id: number; parent_id: number | null; name: string };
type Person = { userid: string; unionid: string; name: string; job_number?: string; title?: string; dept_id_list?: number[] };
export function directorySettings(db: DatabaseSync) {
  const read = (key: string) => String(db.prepare('SELECT value FROM system_settings WHERE key = ?').get(key)?.value ?? '');
  const rootDepartmentIds = JSON.parse(read('directory.roots') || '[1]') as number[];
  return { rootDepartmentIds, intervalMinutes: Number(read('directory.interval') || 0), configured: Boolean(process.env.DINGTALK_CLIENT_ID && process.env.DINGTALK_CLIENT_SECRET && process.env.DINGTALK_CORP_ID), corpId: process.env.DINGTALK_CORP_ID ?? '' };
}
export function saveDirectorySettings(db: DatabaseSync, actor: AuthUser, input: Record<string, unknown>) {
  if (!Array.isArray(input.rootDepartmentIds) || !input.rootDepartmentIds.length || input.rootDepartmentIds.length > 100) throw new AppError(400, '请设置 1 至 100 个授权起始部门编号');
  const roots = [...new Set(input.rootDepartmentIds.map((id) => integer(id, '部门编号')))];
  const interval = Number(input.intervalMinutes);
  if (!Number.isInteger(interval) || (interval !== 0 && (interval < 30 || interval > 1440))) throw new AppError(400, '同步间隔为 0（关闭）或 30 至 1440 分钟');
  db.exec('BEGIN IMMEDIATE');
  try {
    const save = db.prepare('INSERT INTO system_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value');
    save.run('directory.roots', JSON.stringify(roots)); save.run('directory.interval', String(interval));
    db.prepare('INSERT INTO audit_logs (actor_id, actor_name, action, target, details_json) VALUES (?, ?, ?, ?, ?)').run(actor.id, actor.display_name, 'directory_settings', 'dingtalk', JSON.stringify({ roots, interval }));
    db.exec('COMMIT'); return directorySettings(db);
  } catch (error) { db.exec('ROLLBACK'); throw error; }
}
export function listEmployees(db: DatabaseSync, search = '', includeInactive = false) {
  return db.prepare(`SELECT e.id, e.name, e.company, e.department, e.corp_id, e.dingtalk_user_id, e.job_number, e.title, e.active, e.missing_since, e.synced_at, u.id AS user_id, u.role
    FROM employees e LEFT JOIN users u ON u.employee_id = e.id
    WHERE (? = 1 OR (e.active = 1 AND e.missing_since IS NULL)) AND (e.name LIKE ? OR e.department LIKE ? OR e.job_number LIKE ?)
    ORDER BY e.name, e.id`).all(Number(includeInactive), ...Array(3).fill(`%${search.slice(0, 200)}%`));
}
const running = new WeakSet<DatabaseSync>();
const clients = new WeakMap<DatabaseSync, DingTalkClient>();
export async function syncDirectory(db: DatabaseSync, actor?: AuthUser, supplied?: DingTalkClient) {
  if (running.has(db)) throw new AppError(409, '通讯录正在同步，请稍后刷新');
  const config = directorySettings(db); if (!config.configured) throw new AppError(503, '钉钉应用尚未配置，员工通讯录暂未同步');
  running.add(db);
  let client = supplied ?? clients.get(db); if (!client) { client = new DingTalkClient(); clients.set(db, client); }
  const run = db.prepare("INSERT INTO directory_sync_runs (actor_id, status) VALUES (?, 'running')").run(actor?.id ?? null);
  try {
    const departments = new Map<number, Department>();
    const people = new Map<string, Person>(); const memberships = new Map<string, Set<number>>();
    const queue = [...config.rootDepartmentIds]; const visited = new Set<number>();
    while (queue.length) {
      const id = queue.shift()!; if (visited.has(id)) continue; visited.add(id);
      if (visited.size > 10000) throw new AppError(502, '部门数量超出本次同步上限，请缩小同步范围');
      const detail = await client.call('/topapi/v2/department/get', { dept_id: id, language: 'zh_CN' });
      if (!detail.result?.name) throw new AppError(502, '钉钉部门详情格式异常，本次未更新通讯录');
      departments.set(id, { dept_id: id, parent_id: detail.result.parent_id ?? null, name: String(detail.result.name) });
      const children = await client.call('/topapi/v2/department/listsub', { dept_id: id, language: 'zh_CN' });
      if (!Array.isArray(children.result)) throw new AppError(502, '钉钉子部门数据格式异常');
      for (const child of children.result) { if (Number(child.dept_id) > 0) queue.push(integer(child.dept_id, '钉钉部门编号')); }
      let cursor = 0; const cursors = new Set<number>();
      for (;;) {
        if (cursors.has(cursor) || cursors.size > 2000) throw new AppError(502, '钉钉分页游标异常，本次同步已停止');
        cursors.add(cursor);
        const page = await client.call('/topapi/v2/user/list', { dept_id: id, cursor, size: 100, contain_access_limit: false, language: 'zh_CN' });
        if (!Array.isArray(page.result?.list)) throw new AppError(502, '钉钉员工数据格式异常');
        for (const p of page.result.list) {
          if (!p.userid || !p.name) throw new AppError(502, '钉钉员工缺少身份字段');
          const key = String(p.userid); people.set(key, { ...p, userid: key, unionid: String(p.unionid ?? ''), name: String(p.name) });
          if (!memberships.has(key)) memberships.set(key, new Set()); memberships.get(key)!.add(id);
        }
        if (page.result.has_more !== true && page.result.has_more !== 'true') break;
        const next = Number(page.result.next_cursor); if (!Number.isSafeInteger(next) || next < 0) throw new AppError(502, '钉钉分页游标无效'); cursor = next;
      }
    }
    const now = new Date().toISOString();
    db.exec('BEGIN IMMEDIATE');
    try {
      for (const d of departments.values()) db.prepare('INSERT INTO departments (corp_id, dingtalk_id, parent_id, name, synced_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(corp_id, dingtalk_id) DO UPDATE SET parent_id = excluded.parent_id, name = excluded.name, synced_at = excluded.synced_at').run(config.corpId, d.dept_id, d.parent_id, d.name, now);
      for (const p of people.values()) {
        const ids = [...memberships.get(p.userid)!]; const department = ids.map((id) => departments.get(id)?.name).filter(Boolean).join(' / ');
        db.prepare(`INSERT INTO employees (name, department, dingtalk_user_id, corp_id, union_id, job_number, title, department_ids_json, synced_at)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(corp_id, dingtalk_user_id) DO UPDATE SET name = excluded.name, department = excluded.department, union_id = excluded.union_id, job_number = excluded.job_number, title = excluded.title, department_ids_json = excluded.department_ids_json, synced_at = excluded.synced_at, missing_since = NULL, updated_at = CURRENT_TIMESTAMP`)
          .run(p.name, department, p.userid, config.corpId, p.unionid || null, String(p.job_number ?? ''), String(p.title ?? ''), JSON.stringify(ids), now);
        const e = db.prepare('SELECT id FROM employees WHERE corp_id = ? AND dingtalk_user_id = ?').get(config.corpId, p.userid)!;
        ensureEmployeeUser(db, Number(e.id));
      }
      // Absence can be caused by changed authorization. Keep history and mark for review.
      db.prepare('UPDATE employees SET missing_since = COALESCE(missing_since, ?) WHERE corp_id = ? AND (synced_at IS NULL OR synced_at <> ?)').run(now, config.corpId, now);
      db.prepare('DELETE FROM sessions WHERE user_id IN (SELECT u.id FROM users u JOIN employees e ON e.id = u.employee_id WHERE e.corp_id = ? AND e.missing_since IS NOT NULL)').run(config.corpId);
      db.prepare("UPDATE directory_sync_runs SET status = 'completed', employees_count = ?, departments_count = ?, completed_at = CURRENT_TIMESTAMP WHERE id = ?").run(people.size, departments.size, run.lastInsertRowid);
      db.prepare('INSERT INTO audit_logs (actor_id, actor_name, action, target, details_json) VALUES (?, ?, ?, ?, ?)').run(actor?.id ?? null, actor?.display_name ?? '定时同步', 'directory_sync', String(run.lastInsertRowid), JSON.stringify({ employees: people.size, departments: departments.size }));
      db.exec('COMMIT');
    } catch (error) { db.exec('ROLLBACK'); throw error; }
    return { employees: people.size, departments: departments.size };
  } catch (error) {
    const message = error instanceof AppError ? error.message : '通讯录保存失败，原有人员数据已保留，请稍后重试';
    db.prepare("UPDATE directory_sync_runs SET status = 'failed', message = ?, completed_at = CURRENT_TIMESTAMP WHERE id = ?").run(message, run.lastInsertRowid);
    throw new AppError(error instanceof AppError ? error.status : 500, message);
  } finally { running.delete(db); }
}
export function startDirectoryScheduler(db: DatabaseSync) {
  let checkedAt = Date.now();
  const timer = setInterval(() => {
    const settings = directorySettings(db);
    if (!settings.configured || !settings.intervalMinutes || Date.now() - checkedAt < settings.intervalMinutes * 60000) return;
    checkedAt = Date.now(); void syncDirectory(db).catch(() => undefined);
  }, 60000);
  timer.unref(); return () => clearInterval(timer);
}
export function employeeMatches(db: DatabaseSync) {
  return db.prepare("SELECT id, asset_code, name, assignee, department, version FROM assets WHERE assignee_employee_id IS NULL AND COALESCE(assignee, '') <> '' ORDER BY assignee, asset_code").all().map((asset) => ({ ...asset, candidates: db.prepare('SELECT id, name, department, job_number FROM employees WHERE name = ? AND active = 1 AND missing_since IS NULL').all(asset.assignee) }));
}
export function bindAssetEmployee(db: DatabaseSync, actor: AuthUser, code: string, raw: Record<string, unknown>) {
  const id = integer(raw.employeeId, '员工编号'); const version = integer(raw.expectedVersion, '资料版本'); const reason = boundedText(raw.reason, '关联原因', 500, true);
  const asset = db.prepare('SELECT * FROM assets WHERE asset_code = ?').get(code) as Record<string, any> | undefined;
  const employee = db.prepare('SELECT * FROM employees WHERE id = ? AND active = 1 AND missing_since IS NULL').get(id) as Record<string, any> | undefined;
  if (!asset || !employee) throw new AppError(404, '资产或有效员工不存在');
  if (asset.version !== version) throw new AppError(409, '台账已变化，请刷新后重新核对');
  if (asset.assignee_employee_id === id) return;
  if (asset.assignee_employee_id) throw new AppError(409, '资产已关联员工，更换使用人请通过交接审批');
  if (asset.assignee !== employee.name || !asset.assignee) throw new AppError(409, '旧台账姓名与员工姓名不一致，请通过交接审批变更');
  if (db.prepare("SELECT i.id FROM asset_flow_items i JOIN asset_flows f ON f.id = i.flow_id WHERE i.asset_id = ? AND f.status = 'pending'").get(asset.id)) throw new AppError(409, '资产有待审批单据，请先处理后关联');
  db.exec('BEGIN IMMEDIATE');
  try {
    db.prepare('UPDATE assets SET assignee_employee_id = ?, updated_at_system = CURRENT_TIMESTAMP WHERE id = ?').run(id, asset.id);
    db.prepare("INSERT INTO asset_events (asset_id, asset_code, type, operator_name, note, changes_json) VALUES (?, ?, '人员关联', ?, ?, ?)")
      .run(asset.id, code, actor.display_name, reason, JSON.stringify([{ field: 'assignee_employee_id', before: '', after: id }]));
    db.prepare('INSERT INTO audit_logs (actor_id, actor_name, action, target, details_json) VALUES (?, ?, ?, ?, ?)').run(actor.id, actor.display_name, 'bind_asset_employee', code, JSON.stringify({ employeeId: id, reason }));
    db.exec('COMMIT');
  } catch (error) { db.exec('ROLLBACK'); throw error; }
}
