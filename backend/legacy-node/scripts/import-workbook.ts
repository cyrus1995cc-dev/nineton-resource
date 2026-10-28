import { randomUUID } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { openDatabase } from '../db';
import { applyImport, parseWorkbook } from '../importer';

const fileName = process.argv[2];
if (!fileName) {
  console.error('用法：npm run import -- <资产清单.xlsx>');
  process.exit(1);
}

const resolved = resolve(fileName);
const db = openDatabase();
const rows = parseWorkbook(await readFile(resolved));
const result = applyImport(db, rows, randomUUID(), resolved);
console.log(JSON.stringify({ file: resolved, ...result }, null, 2));
