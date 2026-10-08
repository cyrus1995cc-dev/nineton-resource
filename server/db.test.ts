import { describe, expect, it } from 'vitest';
import { openDatabase } from './db';

describe('asset database', () => {
  it('creates the asset and audit tables with a unique asset code', () => {
    const db = openDatabase(':memory:');
    const tables = db.prepare("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name").all() as Array<{ name: string }>;
    expect(tables.map((table) => table.name).filter((name) => !name.startsWith('sqlite_'))).toEqual([
      'asset_events',
      'assets',
      'employees',
      'import_batches',
      'sessions',
      'users',
    ]);

    db.prepare("INSERT INTO assets (asset_code, name, status) VALUES ('CSJ-0431', '测试机', '空闲')").run();
    expect(() => db.prepare("INSERT INTO assets (asset_code, name, status) VALUES ('CSJ-0431', '重复', '空闲')").run()).toThrow();
  });
});
