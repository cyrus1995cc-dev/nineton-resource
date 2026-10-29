import type { EmployeeMapping } from './directory';

export type MappingGroup = {
  key: string; assignee: string; department: string; company: string;
  assets: EmployeeMapping[]; candidates: EmployeeMapping['candidates'];
  state: 'unique' | 'ambiguous' | 'departed' | 'inactive';
};

export function groupEmployeeMappings(assets: EmployeeMapping[]): MappingGroup[] {
  const grouped = new Map<string, MappingGroup>();
  for (const asset of assets) {
    const key = JSON.stringify([asset.assignee, asset.department || '', asset.company || '']);
    const existing = grouped.get(key);
    if (existing) { existing.assets.push(asset); continue; }
    grouped.set(key, {
      key, assignee: asset.assignee, department: asset.department || '', company: asset.company || '', assets: [asset], candidates: asset.candidates,
      state: asset.candidates.length === 1 ? 'unique' : asset.candidates.length > 1 ? 'ambiguous' : (asset.roster_match_count || 0) > 0 ? 'inactive' : 'departed',
    });
  }
  return [...grouped.values()];
}

export function mappingBatch(groups: MappingGroup[], picks: Record<string, number>) {
  return groups.flatMap(group => {
    const employeeId = picks[group.key];
    if (!employeeId || !group.candidates.some(candidate => candidate.id === employeeId)) throw new Error('请为每个分组选择有效的对应员工');
    return group.assets.map(asset => ({ assetCode: asset.asset_code, employeeId, expectedVersion: asset.version }));
  });
}
