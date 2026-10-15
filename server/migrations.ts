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
      role TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin', 'asset_manager', 'viewer', 'employee', 'approver')),
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
    CREATE TABLE IF NOT EXISTS asset_flows (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      document_no TEXT NOT NULL UNIQUE,
      type TEXT NOT NULL,
      status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'rejected', 'withdrawn')),
      applicant_id INTEGER NOT NULL REFERENCES users(id),
      applicant_name TEXT NOT NULL,
      applicant_department TEXT NOT NULL,
      to_assignee TEXT NOT NULL DEFAULT '', to_department TEXT NOT NULL DEFAULT '', to_location TEXT NOT NULL DEFAULT '',
      note TEXT NOT NULL DEFAULT '', current_step INTEGER NOT NULL DEFAULT 1,
      request_id TEXT NOT NULL UNIQUE, request_hash TEXT NOT NULL,
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, completed_at TEXT
    );
    CREATE TABLE IF NOT EXISTS asset_flow_items (
      id INTEGER PRIMARY KEY AUTOINCREMENT, flow_id INTEGER NOT NULL REFERENCES asset_flows(id),
      asset_id INTEGER NOT NULL REFERENCES assets(id), asset_code TEXT NOT NULL, asset_name TEXT NOT NULL,
      category_path TEXT NOT NULL, model TEXT NOT NULL,
      expected_status TEXT NOT NULL, expected_location TEXT NOT NULL, expected_assignee TEXT NOT NULL, expected_department TEXT NOT NULL,
      UNIQUE(flow_id, asset_id)
    );
    CREATE TABLE IF NOT EXISTS asset_flow_steps (
      id INTEGER PRIMARY KEY AUTOINCREMENT, flow_id INTEGER NOT NULL REFERENCES asset_flows(id),
      sequence INTEGER NOT NULL, approver_id INTEGER NOT NULL REFERENCES users(id), approver_name TEXT NOT NULL,
      status TEXT NOT NULL CHECK (status IN ('waiting', 'pending', 'approved', 'rejected', 'cancelled')),
      comment TEXT NOT NULL DEFAULT '', acted_at TEXT, UNIQUE(flow_id, sequence)
    );
    CREATE TABLE IF NOT EXISTS asset_flow_logs (
      id INTEGER PRIMARY KEY AUTOINCREMENT, flow_id INTEGER NOT NULL REFERENCES asset_flows(id),
      action TEXT NOT NULL, actor_id INTEGER NOT NULL REFERENCES users(id), actor_name TEXT NOT NULL,
      note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
    CREATE TABLE IF NOT EXISTS asset_flow_comments (
      id INTEGER PRIMARY KEY AUTOINCREMENT, flow_id INTEGER NOT NULL REFERENCES asset_flows(id),
      user_id INTEGER NOT NULL REFERENCES users(id), user_name TEXT NOT NULL, content TEXT NOT NULL,
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
    CREATE TABLE IF NOT EXISTS notification_outbox (
      id INTEGER PRIMARY KEY AUTOINCREMENT, flow_id INTEGER NOT NULL REFERENCES asset_flows(id),
      kind TEXT NOT NULL, recipient_id INTEGER NOT NULL REFERENCES users(id), recipient_name TEXT NOT NULL,
      dedupe_key TEXT NOT NULL UNIQUE, payload_json TEXT NOT NULL,
      status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed', 'cancelled')),
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, sent_at TEXT
    );
    CREATE INDEX IF NOT EXISTS idx_flows_status ON asset_flows(status, id DESC);
    CREATE INDEX IF NOT EXISTS idx_flow_items_asset ON asset_flow_items(asset_id, flow_id);
    CREATE INDEX IF NOT EXISTS idx_flow_steps_approver ON asset_flow_steps(approver_id, status);
    CREATE INDEX IF NOT EXISTS idx_outbox_status ON notification_outbox(status, id);
  `);
  try { db.exec('ALTER TABLE users ADD COLUMN dingtalk_user_id TEXT'); } catch { /* existing databases already have this column */ }
  db.exec('CREATE UNIQUE INDEX IF NOT EXISTS idx_users_dingtalk ON users(dingtalk_user_id) WHERE dingtalk_user_id IS NOT NULL');
  // Historical events have no original approval documents. Keep them as ordinary history.
  const eventColumns = db.prepare('PRAGMA table_info(asset_events)').all() as Array<{ name: string }>;
  if (!eventColumns.some((column) => column.name === 'flow_id')) db.exec('ALTER TABLE asset_events ADD COLUMN flow_id INTEGER REFERENCES asset_flows(id)');
  if (!eventColumns.some((column) => column.name === 'changes_json')) db.exec("ALTER TABLE asset_events ADD COLUMN changes_json TEXT NOT NULL DEFAULT '[]'");
  const userSchema = String(db.prepare("SELECT sql FROM sqlite_master WHERE name = 'users'").get()?.sql);
  if (!userSchema.includes("'employee'")) {
    // Preserve IDs and referencing history while expanding the original CHECK constraint.
    db.exec('PRAGMA foreign_keys = OFF; BEGIN IMMEDIATE');
    try {
      db.exec(`CREATE TABLE users_expanded (
        id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL,
        display_name TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin','asset_manager','viewer','employee','approver')),
        dingtalk_user_id TEXT UNIQUE, active INTEGER NOT NULL DEFAULT 1,
        created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
        INSERT INTO users_expanded SELECT id, username, password_hash, display_name, role, dingtalk_user_id, active, created_at, updated_at FROM users;
        DROP TABLE users; ALTER TABLE users_expanded RENAME TO users; COMMIT;`);
    } catch (error) { db.exec('ROLLBACK'); throw error; }
    finally { db.exec('PRAGMA foreign_keys = ON'); }
  }
  const add = (table: string, column: string, type: string) => {
    const columns = db.prepare(`PRAGMA table_info(${table})`).all() as Array<{ name: string }>;
    if (!columns.some((c) => c.name === column)) db.exec(`ALTER TABLE ${table} ADD COLUMN ${column} ${type}`);
  };
  add('assets', 'version', 'INTEGER NOT NULL DEFAULT 1');
  add('assets', 'assignee_employee_id', 'INTEGER REFERENCES employees(id)');
  add('users', 'employee_id', 'INTEGER REFERENCES employees(id)');
  add('employees', 'corp_id', "TEXT NOT NULL DEFAULT ''");
  add('employees', 'union_id', 'TEXT');
  add('employees', 'job_number', "TEXT NOT NULL DEFAULT ''");
  add('employees', 'title', "TEXT NOT NULL DEFAULT ''");
  add('employees', 'department_ids_json', "TEXT NOT NULL DEFAULT '[]'");
  add('employees', 'active', 'INTEGER NOT NULL DEFAULT 1');
  add('employees', 'missing_since', 'TEXT');
  add('employees', 'synced_at', 'TEXT');
  add('asset_flows', 'to_employee_id', 'INTEGER REFERENCES employees(id)');
  add('asset_flow_items', 'expected_employee_id', 'INTEGER');
  db.exec(`
    CREATE UNIQUE INDEX IF NOT EXISTS idx_users_employee ON users(employee_id) WHERE employee_id IS NOT NULL;
    CREATE UNIQUE INDEX IF NOT EXISTS idx_employee_corp_user ON employees(corp_id, dingtalk_user_id);
    CREATE INDEX IF NOT EXISTS idx_assets_employee ON assets(assignee_employee_id);
    CREATE TRIGGER IF NOT EXISTS assets_version AFTER UPDATE ON assets WHEN NEW.version = OLD.version
      BEGIN UPDATE assets SET version = OLD.version + 1 WHERE id = NEW.id; END;
    CREATE TABLE IF NOT EXISTS asset_qr_aliases (
      id INTEGER PRIMARY KEY, asset_id INTEGER NOT NULL REFERENCES assets(id), raw TEXT NOT NULL,
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(asset_id, raw));
    CREATE TABLE IF NOT EXISTS import_previews (
      id TEXT PRIMARY KEY, actor_id INTEGER NOT NULL, file_name TEXT NOT NULL, rows_json TEXT NOT NULL,
      versions_json TEXT NOT NULL, expires_at INTEGER NOT NULL, result_json TEXT,
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
    CREATE TABLE IF NOT EXISTS audit_logs (
      id INTEGER PRIMARY KEY, actor_id INTEGER, actor_name TEXT NOT NULL, action TEXT NOT NULL,
      target TEXT NOT NULL, details_json TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
    CREATE TABLE IF NOT EXISTS departments (
      id INTEGER PRIMARY KEY, corp_id TEXT NOT NULL, dingtalk_id INTEGER NOT NULL, parent_id INTEGER,
      name TEXT NOT NULL, synced_at TEXT NOT NULL, UNIQUE(corp_id, dingtalk_id));
    CREATE TABLE IF NOT EXISTS directory_sync_runs (
      id INTEGER PRIMARY KEY, actor_id INTEGER, status TEXT NOT NULL,
      employees_count INTEGER NOT NULL DEFAULT 0, departments_count INTEGER NOT NULL DEFAULT 0,
      message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, completed_at TEXT);
    CREATE TABLE IF NOT EXISTS system_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
  `);
}
