import { openDatabase } from '../db';

const db = openDatabase();
const total = Number((db.prepare('SELECT COUNT(*) AS count FROM assets').get() as { count: number }).count);
const uniqueCodes = Number((db.prepare('SELECT COUNT(DISTINCT asset_code) AS count FROM assets').get() as { count: number }).count);
const statuses = Object.fromEntries((db.prepare('SELECT status, COUNT(*) AS count FROM assets GROUP BY status ORDER BY status').all() as Array<{ status: string; count: number }>).map((row) => [row.status, Number(row.count)]));
const expected = { total: 1762, uniqueCodes: 1762, status: { '在用': 1249, '空闲': 513 } };
const actual = { total, uniqueCodes, status: statuses };
console.log(JSON.stringify({ actual, expected }, null, 2));
if (total !== expected.total || uniqueCodes !== expected.uniqueCodes || statuses['在用'] !== expected.status['在用'] || statuses['空闲'] !== expected.status['空闲']) {
  throw new Error('种子数据校验失败');
}
const asset = db.prepare("SELECT asset_code, name, status FROM assets WHERE asset_code = 'CSJ-0431'").get();
if (!asset) throw new Error('未找到 CSJ-0431');
console.log('Verified asset:', asset);
