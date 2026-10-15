import { createHash, randomBytes, scryptSync, timingSafeEqual } from 'node:crypto';
import type { DatabaseSync } from 'node:sqlite';
import { AppError, boundedText, integer } from './errors';

export const userRoles = ['admin', 'asset_manager', 'viewer', 'employee', 'approver'] as const;
export type UserRole = typeof userRoles[number];
export type AuthUser = { id: number; username: string; display_name: string; role: UserRole; employee_id?: number | null; department?: string };
export type DingTalkConfig = { enabled: boolean; corpId: string };

function passwordHash(password: string, salt = randomBytes(16).toString('hex')) {
  return `scrypt$${salt}$${scryptSync(password, salt, 64).toString('hex')}`;
}

export type UserInput = { username: string; password: string; display_name: string; role: UserRole };

function verifyPassword(password: string, stored: string) {
  const [, salt, expectedHex] = stored.split('$');
  if (!salt || !expectedHex) return false;
  const actual = scryptSync(password, salt, 64);
  const expected = Buffer.from(expectedHex, 'hex');
  return actual.length === expected.length && timingSafeEqual(actual, expected);
}

function hashToken(token: string) {
  return createHash('sha256').update(token).digest('hex');
}

function sessionForRow(db: DatabaseSync, row: Record<string, unknown>) {
  const token = randomBytes(32).toString('base64url');
  const expiresAt = Date.now() + 8 * 60 * 60 * 1000;
  db.prepare('INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?)').run(row.id as number, hashToken(token), expiresAt);
  return { token, user: publicUser(row), expiresAt };
}

export function ensureDefaultAdmin(db: DatabaseSync) {
  const existing = db.prepare('SELECT id FROM users WHERE username = ?').get(process.env.ASSET_ADMIN_USERNAME ?? 'admin');
  if (existing) return;
  const username = process.env.ASSET_ADMIN_USERNAME ?? 'admin';
  const password = process.env.ASSET_ADMIN_PASSWORD ?? 'admin123';
  db.prepare('INSERT INTO users (username, password_hash, display_name, role) VALUES (?, ?, ?, ?)')
    .run(username, passwordHash(password), '系统管理员', 'admin');
}

function publicUser(row: Record<string, unknown>): AuthUser {
  return { id: Number(row.id), username: String(row.username), display_name: String(row.display_name), role: row.role as UserRole, employee_id: row.employee_id == null ? null : Number(row.employee_id), department: String(row.employee_department ?? '') };
}

export function listUsers(db: DatabaseSync) {
  return (db.prepare('SELECT u.*, e.department AS employee_department FROM users u LEFT JOIN employees e ON e.id = u.employee_id ORDER BY u.id').all() as Array<Record<string, unknown>>)
    .map((row) => ({ ...publicUser(row), active: Boolean(row.active), created_at: String(row.created_at) }));
}

export function createUser(db: DatabaseSync, input: UserInput) {
  if (!input || typeof input.username !== 'string' || typeof input.password !== 'string' || typeof input.display_name !== 'string' || !userRoles.includes(input.role)) return { status: 'invalid' as const };
  const username = boundedText(input.username, '用户名', 100, true);
  const displayName = boundedText(input.display_name, '显示名称', 200) || username;
  if (input.password.length < 6 || input.password.length > 200) throw new AppError(400, '密码长度需为 6 至 200 位');
  try {
    const result = db.prepare('INSERT INTO users (username, password_hash, display_name, role) VALUES (?, ?, ?, ?)')
      .run(username, passwordHash(input.password), displayName, input.role);
    const row = db.prepare('SELECT id, username, display_name, role, active, created_at FROM users WHERE id = ?').get(result.lastInsertRowid) as Record<string, unknown>;
    return { status: 'created' as const, user: { ...publicUser(row), active: Boolean(row.active), created_at: String(row.created_at) } };
  } catch (error) {
    if (error instanceof Error && error.message.includes('UNIQUE')) return { status: 'duplicate' as const };
    throw error;
  }
}

export function authenticate(db: DatabaseSync, username: string, password: string) {
  const row = db.prepare('SELECT u.*, e.department AS employee_department FROM users u LEFT JOIN employees e ON e.id = u.employee_id WHERE u.username = ? AND u.active = 1 AND (e.id IS NULL OR (e.active = 1 AND e.missing_since IS NULL))').get(username) as Record<string, unknown> | undefined;
  if (!row || !verifyPassword(password, String(row.password_hash))) return null;
  return sessionForRow(db, row);
}

export function getDingTalkConfig(): DingTalkConfig {
  const corpId = process.env.DINGTALK_CORP_ID ?? '';
  return { enabled: Boolean(process.env.DINGTALK_CLIENT_ID && process.env.DINGTALK_CLIENT_SECRET && corpId), corpId };
}

async function readJson(response: Response) {
  const payload = await response.json().catch(() => ({})) as Record<string, unknown>;
  if (!response.ok) throw new Error(String(payload.message ?? payload.errmsg ?? `钉钉接口失败（${response.status}）`));
  return payload;
}

export async function authenticateDingTalk(db: DatabaseSync, code: string) {
  const config = getDingTalkConfig();
  if (!config.enabled) return { status: 'disabled' as const };
  if (!code.trim()) return { status: 'invalid_code' as const };
  const tokenResponse = await readJson(await fetch(process.env.DINGTALK_TOKEN_URL ?? 'https://api.dingtalk.com/v1.0/oauth2/userAccessToken', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ clientId: process.env.DINGTALK_CLIENT_ID, clientSecret: process.env.DINGTALK_CLIENT_SECRET, code, grantType: 'authorization_code' }),
  }));
  const accessToken = String(tokenResponse.accessToken ?? tokenResponse.access_token ?? '');
  if (!accessToken) throw new Error('钉钉未返回用户访问令牌');
  const profile = await readJson(await fetch(process.env.DINGTALK_USER_URL ?? 'https://api.dingtalk.com/v1.0/contact/userinfo', {
    headers: { 'x-acs-dingtalk-access-token': accessToken },
  }));
  const dingTalkUserId = String(profile.unionId ?? profile.unionid ?? '');
  if (!dingTalkUserId) throw new Error('钉钉未返回稳定用户标识');
  const displayName = String(profile.nick ?? profile.name ?? dingTalkUserId);
  const employees = db.prepare('SELECT * FROM employees WHERE corp_id = ? AND union_id = ?').all(config.corpId, dingTalkUserId) as Array<Record<string, any>>;
  if (employees.length !== 1) throw new AppError(403, '当前钉钉身份尚未关联企业通讯录，请管理员先同步并核对员工身份');
  const employee = employees[0];
  if (!employee.active || employee.missing_since) return { status: 'disabled_user' as const };
  ensureEmployeeUser(db, Number(employee.id));
  const row = db.prepare('SELECT u.*, e.department AS employee_department FROM users u JOIN employees e ON e.id = u.employee_id WHERE e.id = ?').get(employee.id) as Record<string, unknown>;
  if (!row || Number(row.active) !== 1) return { status: 'disabled_user' as const };
  return { status: 'authenticated' as const, ...sessionForRow(db, row) };
}

export function resolveSession(db: DatabaseSync, token: string | undefined) {
  if (!token) return null;
  const row = db.prepare(`SELECT u.*, e.department AS employee_department FROM sessions s JOIN users u ON u.id = s.user_id LEFT JOIN employees e ON e.id = u.employee_id
    WHERE s.token_hash = ? AND s.expires_at > ? AND u.active = 1 AND (e.id IS NULL OR (e.active = 1 AND e.missing_since IS NULL))`).get(hashToken(token), Date.now()) as Record<string, unknown> | undefined;
  return row ? publicUser(row) : null;
}

export function revokeSession(db: DatabaseSync, token: string | undefined) {
  if (!token) return;
  db.prepare('DELETE FROM sessions WHERE token_hash = ?').run(hashToken(token));
}

export function authTokenFromRequest(authorization: string | undefined) {
  const match = /^Bearer\s+(.+)$/i.exec(authorization ?? '');
  return match?.[1];
}

export function ensureEmployeeUser(db: DatabaseSync, employeeId: number) {
  const e = db.prepare('SELECT * FROM employees WHERE id = ?').get(employeeId) as Record<string, any>;
  const existing = db.prepare('SELECT id FROM users WHERE employee_id = ?').get(employeeId);
  if (existing) { db.prepare('UPDATE users SET display_name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(e.name, existing.id); return Number(existing.id); }
  // Legacy union IDs may only bind to the same verified enterprise employee.
  const legacy = e.union_id ? db.prepare('SELECT id, employee_id FROM users WHERE dingtalk_user_id = ?').get(e.union_id) : null;
  if (legacy) {
    if (legacy.employee_id != null && legacy.employee_id !== employeeId) throw new AppError(409, '钉钉身份已关联其他员工，请核对账号');
    db.prepare('UPDATE users SET employee_id = ? WHERE id = ?').run(employeeId, legacy.id); return Number(legacy.id);
  }
  const username = `ding_${createHash('sha256').update(`${e.corp_id}:${e.dingtalk_user_id}`).digest('hex').slice(0, 20)}`;
  const result = db.prepare("INSERT INTO users (username, password_hash, display_name, role, employee_id) VALUES (?, ?, ?, 'employee', ?)")
    .run(username, 'external:dingtalk', e.name, employeeId);
  return Number(result.lastInsertRowid);
}

export function updateUser(db: DatabaseSync, actor: AuthUser, id: number, input: Record<string, unknown>) {
  const current = db.prepare('SELECT * FROM users WHERE id = ?').get(id) as Record<string, any> | undefined;
  if (!current) throw new AppError(404, '账号不存在');
  const role = input.role ?? current.role; const active = input.active ?? Boolean(current.active);
  if (!userRoles.includes(role as UserRole) || typeof active !== 'boolean') throw new AppError(400, '角色或账号状态无效');
  if (id === actor.id && (role !== current.role || !active)) throw new AppError(409, '不能在当前会话修改自己的角色或停用自己');
  if ((!active || !['admin', 'asset_manager', 'approver'].includes(String(role))) && db.prepare("SELECT id FROM asset_flow_steps WHERE approver_id = ? AND status IN ('pending', 'waiting')").get(id)) throw new AppError(409, '该账号仍有待处理审批，请先处理单据后调整');
  const employeeId = input.employee_id === null ? null : input.employee_id === undefined ? current.employee_id : integer(input.employee_id, '员工编号');
  if (employeeId != null && !db.prepare('SELECT id FROM employees WHERE id = ? AND active = 1').get(employeeId)) throw new AppError(400, '关联员工不存在或已停用');
  if (employeeId != null && db.prepare('SELECT id FROM users WHERE employee_id = ? AND id <> ?').get(employeeId, id)) throw new AppError(409, '该员工已关联其他账号');
  db.exec('BEGIN IMMEDIATE');
  try {
    db.prepare('UPDATE users SET role = ?, active = ?, employee_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(role as string, Number(active), employeeId ?? null, id);
    db.prepare('DELETE FROM sessions WHERE user_id = ?').run(id);
    db.prepare('INSERT INTO audit_logs (actor_id, actor_name, action, target, details_json) VALUES (?, ?, ?, ?, ?)').run(actor.id, actor.display_name, 'user_permissions', String(id), JSON.stringify({ before: { role: current.role, active: Boolean(current.active), employee_id: current.employee_id }, after: { role, active, employee_id: employeeId } }));
    db.exec('COMMIT'); return listUsers(db).find((u) => u.id === id);
  } catch (error) { db.exec('ROLLBACK'); throw error; }
}
