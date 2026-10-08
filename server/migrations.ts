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
  `);
}
