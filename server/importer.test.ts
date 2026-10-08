import { describe, expect, it } from 'vitest';
import * as XLSX from 'xlsx';
import { openDatabase } from './db';
import { applyImport, parseWorkbook, previewImport } from './importer';

const headers = ['资产编码', '资产分类(一级)', '创建时间', '资产分类', '资产名称', '资产状态', '所在位置', '所属/承租公司', '型号', 'CPU', '是否有独立显卡', '内存', '显卡型号', '设备序列号', '备注', '资产照片', '使用人', '使用部门', '领用日期', '购置/租赁金额（含税）', '购置/租赁金额（不含税）', '税额'];

function workbookBuffer(rows: Array<Record<string, unknown>>) {
  const sheet = XLSX.utils.json_to_sheet(rows, { header: headers });
  return XLSX.write({ SheetNames: ['sheet1'], Sheets: { sheet1: sheet } }, { type: 'buffer', bookType: 'xlsx' });
}

function row(code: string, overrides: Record<string, unknown> = {}) {
  return {
    资产编码: code,
    '资产分类(一级)': '计算机和网络设备',
    创建时间: '2026-09-24 09:54:23',
    资产分类: '计算机和网络设备/手机',
    资产名称: '测试机',
    资产状态: '空闲',
    所在位置: '12楼',
    '所属/承租公司': '重庆飞闪科技有限公司',
    型号: 'iPhone 14',
    CPU: '',
    是否有独立显卡: '',
    内存: '',
    显卡型号: '',
    设备序列号: '',
    备注: '',
    资产照片: '',
    使用人: '',
    使用部门: '',
    领用日期: '',
    '购置/租赁金额（含税）': 0,
    '购置/租赁金额（不含税）': 0,
    税额: 0,
    ...overrides,
  };
}

describe('asset workbook importer', () => {
  it('maps the supplied Chinese headers into stable fields', () => {
    const [parsed] = parseWorkbook(workbookBuffer([row('CSJ-0431')]));
    expect(parsed).toMatchObject({
      asset_code: 'CSJ-0431',
      category_level1: '计算机和网络设备',
      category_path: '计算机和网络设备/手机',
      name: '测试机',
      status: '空闲',
      amount_taxed: 0,
    });
  });

  it('previews new, updated, unchanged, and invalid rows', () => {
    const db = openDatabase(':memory:');
    db.prepare("INSERT INTO assets (asset_code, name, status, remark) VALUES ('CSJ-0431', '旧名称', '空闲', '保留')").run();
    const rows = parseWorkbook(workbookBuffer([
      row('CSJ-0431', { 资产名称: '新名称' }),
      row('CSJ-0432'),
      row(''),
    ]));
    const preview = previewImport(db, rows);
    expect(preview.updatedRows).toHaveLength(1);
    expect(preview.newRows).toHaveLength(1);
    expect(preview.invalidRows).toHaveLength(1);
  });

  it('does not erase stored remarks when the source cell is blank', () => {
    const db = openDatabase(':memory:');
    db.prepare("INSERT INTO assets (asset_code, name, status, remark) VALUES ('CSJ-0431', '旧名称', '空闲', '保留')").run();
    const [incoming] = parseWorkbook(workbookBuffer([row('CSJ-0431', { 资产名称: '新名称', 备注: '' })]));
    applyImport(db, [incoming], 'batch-test');
    const stored = db.prepare("SELECT name, remark FROM assets WHERE asset_code = 'CSJ-0431'").get() as { name: string; remark: string };
    expect(stored).toEqual({ name: '新名称', remark: '保留' });
  });
});
