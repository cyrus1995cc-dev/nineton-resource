import { describe, expect, it } from 'vitest';
import { groupEmployeeMappings, mappingBatch } from './employeeMappings';
import type { EmployeeMapping } from './directory';

const person = { id: 7, name: '张三', department: '研发', job_number: 'A007' };
function asset(id: number, extra: Partial<EmployeeMapping> = {}): EmployeeMapping {
  return { id, asset_code: `ZC-${id}`, name: '电脑', assignee: '张三', department: '研发', company: '九吨', version: id, roster_match_count: 1, candidates: [person], ...extra };
}

describe('legacy employee mappings', () => {
  it('groups one person while retaining distinct historical departments and companies', () => {
    const groups = groupEmployeeMappings([asset(1), asset(2), asset(3, { department: '行政' }), asset(4, { company: '分公司' })]);
    expect(groups.map(g => g.assets.length)).toEqual([2, 1, 1]);
    expect(new Set(groups.map(g => g.key)).size).toBe(3);
    expect(groups[0]!.state).toBe('unique');
    expect(mappingBatch([groups[0]!], { [groups[0]!.key]: 7 })).toEqual([
      { assetCode: 'ZC-1', employeeId: 7, expectedVersion: 1 },
      { assetCode: 'ZC-2', employeeId: 7, expectedVersion: 2 },
    ]);
  });
  it('distinguishes ambiguous, departed, and disabled employees', () => {
    const groups = groupEmployeeMappings([
      asset(1, { candidates: [person, { ...person, id: 8 }] }),
      asset(2, { assignee: '离职员工', candidates: [], roster_match_count: 0 }),
      asset(3, { assignee: '停用员工', candidates: [], roster_match_count: 1 }),
    ]);
    expect(groups.map(g => g.state)).toEqual(['ambiguous', 'departed', 'inactive']);
    expect(() => mappingBatch([groups[0]!], {})).toThrow('选择有效');
    expect(mappingBatch([groups[0]!], { [groups[0]!.key]: 8 })[0]!.employeeId).toBe(8);
    for (const group of groups) expect(() => mappingBatch([group], { [group.key]: 999 })).toThrow('选择有效');
  });
});
