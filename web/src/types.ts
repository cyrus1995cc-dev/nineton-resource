export type Asset = {
  id: number;
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
};

export type AssetUpdateInput = Pick<Asset, 'category_level1' | 'category_path' | 'name' | 'location' | 'company' | 'model' | 'cpu' | 'discrete_gpu' | 'memory' | 'gpu_model' | 'serial_number' | 'remark' | 'photo_url' | 'assignee' | 'department' | 'received_at' | 'amount_taxed' | 'amount_untaxed' | 'tax_amount'> & {
  operatorName?: string;
  note?: string;
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
  rows: Record<string, unknown>[];
  preview: ImportPreview;
};
