import { expect, it } from 'vitest';
import { formatSystemTime } from './dateTime';
it('displays SQLite UTC timestamps in China time, consistently with ISO timestamps', () => {
  expect(formatSystemTime('2026-10-14 05:23:39')).toContain('13:23:39');
  expect(formatSystemTime('2026-10-14 05:23:39')).toBe(formatSystemTime('2026-10-14T05:23:39Z'));
  expect(formatSystemTime('invalid')).toBe('—');
  expect(formatSystemTime(null)).toBe('—');
});
