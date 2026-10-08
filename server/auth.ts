import { createHash, randomBytes, randomUUID, scryptSync, timingSafeEqual } from 'node:crypto';
import type { DatabaseSync } from 'node:sqlite';

export type UserRole = 'admin' | 'asset_manager' | 'viewer';
export type AuthUser = { id: number; username: string; display_name: string; role: UserRole };

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
  const token = randomBytes(32).toString('base64url');
  const expiresAt = Date.now() + 8 * 60 * 60 * 1000;
  db.prepare('INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?)').run(row.id as number, hashToken(token), expiresAt);
  return { token, user: publicUser(row), expiresAt };
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
