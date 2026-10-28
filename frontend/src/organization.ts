// Flat tree rows keep native keyboard navigation and handle partially authorized roots.
export function treeRows<T>(items: T[], id: (item: T) => number, parent: (item: T) => number | null) {
  const known = new Set(items.map(id));
  const children = new Map<number, T[]>();
  for (const item of items) { const key = parent(item) ?? 0; children.set(key, [...(children.get(key) ?? []), item]); }
  const result: Array<{ item: T; depth: number }> = []; const seen = new Set<number>();
  function visit(item: T, depth: number) {
    const stack = [{ item, depth }];
    while (stack.length) {
      const row = stack.pop()!;
      if (seen.has(id(row.item))) continue;
      seen.add(id(row.item)); result.push(row);
      const next = children.get(id(row.item)) ?? [];
      for (let i = next.length - 1; i >= 0; i--) stack.push({ item: next[i]!, depth: row.depth + 1 });
    }
  }
  for (const item of items) if (!known.has(parent(item) ?? 0)) visit(item, 0);
  for (const item of items) visit(item, 0);
  return result;
}

export function descendantIds<T>(items: T[], root: number, id: (item: T) => number, parent: (item: T) => number | null) {
  const result = new Set<number>(); const stack = [root]; const children = new Map<number, number[]>();
  for (const item of items) { const key = parent(item) ?? 0; children.set(key, [...(children.get(key) ?? []), id(item)]); }
  while (stack.length) { const next = stack.pop()!; if (result.has(next)) continue; result.add(next); stack.push(...(children.get(next) ?? [])); }
  return result;
}
