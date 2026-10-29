export type Asset = {
  id: number;
  version: number;
  assignee_employee_id: number | null;
  asset_code: string;
  qr_raw: string;
  qr_format: string;
  category_level1: string;
  category_path: string;
  created_at: string;
  name: string;
  status: string;
  location: string;
  company: string;
  model: string;
  cpu: string;
  discrete_gpu: string;
  memory: string;
  gpu_model: string;
  serial_number: string;
  remark: string;
  photo_url: string;
  assignee: string;
  department: string;
  received_at: string;
  amount_taxed: number;
  amount_untaxed: number;
  tax_amount: number;
  updated_at_system: string;
  events?: AssetEvent[];
  category_id?: number;
  card_type?: { id: number; name: string; fields: import('./catalogue').CardField[] };
  custom_values?: Record<string, string | number | null>;
};

export type AuthUser = { id: number; username: string; display_name: string; role: 'admin' | 'asset_manager' | 'viewer' | 'employee' | 'approver' | 'guest'; employee_id?: number | null; department?: string };
export type ManagedUser = AuthUser & { active: boolean; created_at: string; sso_bound?: boolean; sso_identity?: { issuer: string; subject: string } };

export type AssetUpdateInput = Pick<Asset, 'category_level1' | 'category_path' | 'name' | 'location' | 'company' | 'model' | 'cpu' | 'discrete_gpu' | 'memory' | 'gpu_model' | 'serial_number' | 'remark' | 'photo_url' | 'assignee' | 'department' | 'received_at' | 'amount_taxed' | 'amount_untaxed' | 'tax_amount'> & {
  operatorName?: string;
  note?: string;
  expectedVersion?: number;
};

export type AssetEvent = {
  id: number;
  type: string;
  operator_name: string;
  from_status: string;
  to_status: string;
  from_assignee: string;
  to_assignee: string;
  from_department: string;
  to_department: string;
  from_location: string;
  to_location: string;
  note: string;
  created_at: string;
  flow_id?: number | null;
  document_no?: string | null;
  changes?: Array<{ field: string; before: string | number; after: string | number }>;
};

export type AssetListResponse = { items: Asset[]; total: number; page: number; pageSize: number };
export type Stats = {
  total: number;
  status: Record<string, number>;
  categories: Array<{ name: string; count: number }>;
  locations: Array<{ name: string; count: number }>;
  companies: Array<{ name: string; count: number }>;
  amounts: { taxed: number; untaxed: number; tax: number };
};

export type ImportPreview = {
  newRows: number;
  updatedRows: number;
  unchangedRows: number;
  invalidRows: number;
};

export type ImportResponse = {
  batchId: string;
  fileName: string;
  expiresAt: number;
  changes?: Array<{ assetCode: string; fields: Array<{ field: string; before: unknown; after: unknown }> }>;
  preview: ImportPreview;
  errors?: Array<{ row: number; assetCode: string; reason: string }>;
};

export type InventoryTask = {
  id: number;
  name: string;
  location_filter: string;
  department_filter: string;
  status: 'in_progress' | 'completed';
  operator_name: string;
  total_count: number;
  checked_count: number;
  difference_count: number;
  created_at: string;
  completed_at: string | null;
};

export type InventoryRecord = {
  id: number;
  task_id: number;
  asset_id: number;
  asset_code: string;
  asset_name: string;
  expected_status: string;
  expected_location: string;
  expected_assignee: string;
  expected_department: string;
  result: 'pending' | 'found' | 'different' | 'missing';
  actual_location: string;
  note: string;
  operator_name: string;
  checked_at: string | null;
};

export type InventoryTaskDetail = { task: InventoryTask; records: InventoryRecord[] };
