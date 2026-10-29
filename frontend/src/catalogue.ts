export type CardField = { key: string; label: string; type: 'text' | 'number' | 'date' | 'select'; required: boolean; options?: string[] };
export type CardType = { id: number; name: string; fields: CardField[]; active: number; version: number; updated_by: string; updated_at: string };
export type AssetCategory = { id: number; code: string; name: string; path: string; parent_id: number | null; card_type_id: number; card_name: string; active: number; version: number; assets_count: number; remark: string };
export type CodeRule = { id: number; prefix: string; digits: number; next_number: number; version: number };
export type AssetCatalogue = { cards: CardType[]; categories: AssetCategory[]; rules: CodeRule[] };
export const standardCardFields: CardField[] = [
  {key:'model',label:'型号',type:'text',required:false}, {key:'serial_number',label:'设备序列号',type:'text',required:false},
  {key:'cpu',label:'CPU',type:'text',required:false}, {key:'memory',label:'内存',type:'text',required:false},
  {key:'discrete_gpu',label:'独立显卡',type:'text',required:false}, {key:'gpu_model',label:'显卡型号',type:'text',required:false},
  {key:'remark',label:'备注',type:'text',required:false}, {key:'photo_url',label:'资产照片地址',type:'text',required:false},
  {key:'amount_taxed',label:'含税金额',type:'number',required:false}, {key:'amount_untaxed',label:'不含税金额',type:'number',required:false}, {key:'tax_amount',label:'税额',type:'number',required:false},
];
export const emptyCatalogue = (): AssetCatalogue => ({ cards:[], categories:[], rules:[] });
