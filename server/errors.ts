export class AppError extends Error {
  constructor(public status: number, message: string) { super(message); }
}
export function invalid(message: string): never { throw new AppError(400, message); }
export function integer(value: unknown, label: string, fallback?: number, max = Number.MAX_SAFE_INTEGER): number {
  if (value === undefined && fallback !== undefined) return fallback;
  if (typeof value !== 'number' && typeof value !== 'string') invalid(`${label}必须为正整数`);
  if (value === '') invalid(`${label}必须为正整数`);
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < 1 || parsed > max) invalid(`${label}必须为 1 至 ${max} 的整数`);
  return parsed;
}
export function boundedText(value: unknown, label: string, max = 200, required = false): string {
  if (typeof value !== 'string' || value.length > max) invalid(`${label}请填写有效文本（最多 ${max} 字）`);
  const text = value.trim(); if (required && !text) invalid(`${label}不能为空`); return text;
}
