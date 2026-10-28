import { describe, expect, it } from 'vitest';
import { descendantIds, treeRows } from './organization';

describe('organization tree', () => {
  const rows = [{ id: 3, parent: 2 }, { id: 1, parent: null }, { id: 2, parent: 1 }, { id: 4, parent: 99 }];
  it('places children after parents and supports partially authorized roots', () => {
    expect(treeRows(rows, r => r.id, r => r.parent).map(r => [r.item.id, r.depth])).toEqual([[1, 0], [2, 1], [3, 2], [4, 0]]);
    expect([...descendantIds(rows, 1, r => r.id, r => r.parent)]).toEqual([1, 2, 3]);
  });
  it('does not loop or hide rows when malformed data forms a cycle', () => {
    const cycle = [{ id: 1, parent: 2 }, { id: 2, parent: 1 }];
    expect(treeRows(cycle, r => r.id, r => r.parent)).toHaveLength(2);
    expect(descendantIds(cycle, 1, r => r.id, r => r.parent).size).toBe(2);
  });
});
