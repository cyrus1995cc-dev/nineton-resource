import { createHash, randomBytes, randomUUID, scryptSync, timingSafeEqual } from 'node:crypto';
import type { DatabaseSync } from 'node:sqlite';

export type UserRole = 'admin' | 'asset_manager' | 'viewer';
export type AuthUser = { id: number; username: string; display_name: string; role: UserRole };
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
  return { id: Number(row.id), username: String(row.username), display_name: String(row.display_name), role: row.role as UserRole };
}

export function listUsers(db: DatabaseSync) {
  return (db.prepare('SELECT id, username, display_name, role, active, created_at FROM users ORDER BY id').all() as Array<Record<string, unknown>>)
    .map((row) => ({ ...publicUser(row), active: Boolean(row.active), created_at: String(row.created_at) }));
}

export function createUser(db: DatabaseSync, input: UserInput) {
  const username = input.username.trim();
  const displayName = input.display_name.trim() || username;
  if (!username || !input.password || !['admin', 'asset_manager', 'viewer'].includes(input.role)) return { status: 'invalid' as const };
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
  const row = db.prepare('SELECT * FROM users WHERE username = ? AND active = 1').get(username) as Record<string, unknown> | undefined;
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
  const dingTalkUserId = String(profile.unionId ?? profile.unionid ?? profile.openId ?? profile.openid ?? '');
  if (!dingTalkUserId) throw new Error('钉钉未返回稳定用户标识');
  const displayName = String(profile.nick ?? profile.name ?? dingTalkUserId);
  let row = db.prepare('SELECT * FROM users WHERE dingtalk_user_id = ?').get(dingTalkUserId) as Record<string, unknown> | undefined;
  if (!row) {
    const username = `ding_${createHash('sha256').update(dingTalkUserId).digest('hex').slice(0, 20)}`;
    db.prepare(`INSERT INTO users (username, password_hash, display_name, role, dingtalk_user_id)
      VALUES (?, ?, ?, 'viewer', ?)`).run(username, passwordHash(randomUUID()), displayName, dingTalkUserId);
    row = db.prepare('SELECT * FROM users WHERE dingtalk_user_id = ?').get(dingTalkUserId) as Record<string, unknown>;
  } else if (String(row.display_name) !== displayName) {
    db.prepare('UPDATE users SET display_name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?').run(displayName, row.id as number);
    row = db.prepare('SELECT * FROM users WHERE id = ?').get(row.id as number) as Record<string, unknown>;
  }
  if (!row || Number(row.active) !== 1) return { status: 'disabled_user' as const };
  return { status: 'authenticated' as const, ...sessionForRow(db, row) };
}

export function resolveSession(db: DatabaseSync, token: string | undefined) {
  if (!token) return null;
  const row = db.prepare(`SELECT u.* FROM sessions s JOIN users u ON u.id = s.user_id
    WHERE s.token_hash = ? AND s.expires_at > ? AND u.active = 1`).get(hashToken(token), Date.now()) as Record<string, unknown> | undefined;
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
