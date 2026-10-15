export const assetStatuses = ['空闲', '在用', '借出', '维修', '报废', '丢失'] as const;
export const assetCodePattern = /^[A-Z][A-Z0-9]{0,31}-[A-Z0-9]{1,32}$/;
export const assetTextFields = ['category_level1', 'category_path', 'name', 'location', 'company', 'model', 'cpu', 'discrete_gpu', 'memory', 'gpu_model', 'serial_number', 'remark', 'photo_url', 'assignee', 'department', 'received_at'] as const;
export const assetAmountFields = ['amount_taxed', 'amount_untaxed', 'tax_amount'] as const;
export const assetEditFields = [...assetTextFields, ...assetAmountFields];
export const assetOwnershipFields = ['status', 'assignee', 'department', 'location', 'received_at'] as const;
export function fieldProblem(field: string, value: unknown): string | null {
  if ((assetAmountFields as readonly string[]).includes(field)) return typeof value !== 'number' || !Number.isFinite(value) || value < 0 || value > 1e12 ? '金额必须为非负数字且不超过一万亿' : null;
  const limit = ['remark', 'photo_url'].includes(field) ? 2000 : 200;
  if (typeof value !== 'string' || value.length > limit) return `${field} 必须为文本且不超过 ${limit} 字`;
  if (field === 'name' && !value.trim()) return '资产名称不能为空';
  if (field === 'status' && !(assetStatuses as readonly string[]).includes(value)) return '资产状态无效';
  if (field === 'asset_code' && !assetCodePattern.test(value)) return '资产编码应为大写字母/数字前缀、短横线和字母/数字编号';
  if (['received_at', 'created_at'].includes(field) && value) {
    if (!/^\d{4}-\d{2}-\d{2}(?:[ T].*)?$/.test(value) || !Number.isFinite(Date.parse(value))) return '日期请使用 YYYY-MM-DD 或完整日期时间';
    const day = value.slice(0, 10); const parsed = new Date(`${day}T00:00:00Z`);
    if (!Number.isFinite(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== day) return '日期不存在，请核对年月日';
  }
  return null;
}
