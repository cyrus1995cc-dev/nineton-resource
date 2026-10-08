import type { DatabaseSync } from 'node:sqlite';

export function migrate(db: DatabaseSync) {
  db.exec(`
    PRAGMA foreign_keys = ON;
    CREATE TABLE IF NOT EXISTS assets (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      asset_code TEXT NOT NULL UNIQUE,
      qr_raw TEXT,
      qr_format TEXT,
      category_level1 TEXT,
      category_path TEXT,
      created_at TEXT,
      name TEXT,
      status TEXT NOT NULL DEFAULT '空闲',
      location TEXT,
      company TEXT,
      model TEXT,
      cpu TEXT,
      discrete_gpu TEXT,
      memory TEXT,
      gpu_model TEXT,
      serial_number TEXT,
      remark TEXT,
      photo_url TEXT,
      assignee TEXT,
      department TEXT,
      received_at TEXT,
      amount_taxed REAL NOT NULL DEFAULT 0,
      amount_untaxed REAL NOT NULL DEFAULT 0,
      tax_amount REAL NOT NULL DEFAULT 0,
      source_updated_at TEXT,
      created_at_system TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
      updated_at_system TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
    CREATE INDEX IF NOT EXISTS idx_assets_status ON assets(status);
    CREATE INDEX IF NOT EXISTS idx_assets_category ON assets(category_path);
    CREATE INDEX IF NOT EXISTS idx_assets_location ON assets(location);
    CREATE INDEX IF NOT EXISTS idx_assets_company ON assets(company);
    CREATE TABLE IF NOT EXISTS asset_events (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      asset_id INTEGER NOT NULL,
      asset_code TEXT NOT NULL,
      type TEXT NOT NULL,
      operator_name TEXT,
      from_status TEXT,
      to_status TEXT,
      from_assignee TEXT,
      to_assignee TEXT,
      from_department TEXT,
      to_department TEXT,
      from_location TEXT,
      to_location TEXT,
      note TEXT,
      request_id TEXT UNIQUE,
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
      FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
    );
    CREATE INDEX IF NOT EXISTS idx_asset_events_code ON asset_events(asset_code, created_at DESC);
    CREATE TABLE IF NOT EXISTS import_batches (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      file_name TEXT,
      checksum TEXT,
      total_rows INTEGER NOT NULL DEFAULT 0,
      new_rows INTEGER NOT NULL DEFAULT 0,
      updated_rows INTEGER NOT NULL DEFAULT 0,
      unchanged_rows INTEGER NOT NULL DEFAULT 0,
      invalid_rows INTEGER NOT NULL DEFAULT 0,
      errors_json TEXT,
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
    CREATE TABLE IF NOT EXISTS employees (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT NOT NULL,
      company TEXT,
      department TEXT,
      dingtalk_user_id TEXT UNIQUE,
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
      updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
    CREATE TABLE IF NOT EXISTS users (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      username TEXT NOT NULL UNIQUE,
      password_hash TEXT NOT NULL,
      display_name TEXT NOT NULL,
      role TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin', 'asset_manager', 'viewer')),
      dingtalk_user_id TEXT UNIQUE,
      active INTEGER NOT NULL DEFAULT 1,
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
      updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
    CREATE TABLE IF NOT EXISTS sessions (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      user_id INTEGER NOT NULL,
      token_hash TEXT NOT NULL UNIQUE,
      expires_at INTEGER NOT NULL,
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
      FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
    );
    CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token_hash);
    CREATE TABLE IF NOT EXISTS inventory_tasks (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT NOT NULL,
      location_filter TEXT,
      department_filter TEXT,
      status TEXT NOT NULL DEFAULT 'in_progress' CHECK (status IN ('in_progress', 'completed')),
      operator_name TEXT,
      total_count INTEGER NOT NULL DEFAULT 0,
      checked_count INTEGER NOT NULL DEFAULT 0,
      difference_count INTEGER NOT NULL DEFAULT 0,
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
      completed_at TEXT
    );
    CREATE INDEX IF NOT EXISTS idx_inventory_tasks_status ON inventory_tasks(status, created_at DESC);
    CREATE TABLE IF NOT EXISTS inventory_records (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      task_id INTEGER NOT NULL,
      asset_id INTEGER NOT NULL,
      asset_code TEXT NOT NULL,
      asset_name TEXT,
      expected_status TEXT,
      expected_location TEXT,
      expected_assignee TEXT,
      expected_department TEXT,
      result TEXT NOT NULL DEFAULT 'pending' CHECK (result IN ('pending', 'found', 'different', 'missing')),
      actual_location TEXT,
      note TEXT,
      operator_name TEXT,
      checked_at TEXT,
      UNIQUE(task_id, asset_id),
      FOREIGN KEY (task_id) REFERENCES inventory_tasks(id) ON DELETE CASCADE,
      FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE
    );
    CREATE INDEX IF NOT EXISTS idx_inventory_records_task ON inventory_records(task_id, result);
  `);
  try { db.exec('ALTER TABLE users ADD COLUMN dingtalk_user_id TEXT'); } catch { /* existing databases already have this column */ }
  db.exec('CREATE UNIQUE INDEX IF NOT EXISTS idx_users_dingtalk ON users(dingtalk_user_id) WHERE dingtalk_user_id IS NOT NULL');
}
