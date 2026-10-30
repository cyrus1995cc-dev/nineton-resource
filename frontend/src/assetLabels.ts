import QRCode from 'qrcode';
import { assetCodePattern } from '../shared/assets';
import { parseQr } from '../shared/qr';
import { assetDepartmentLabel } from './assetWorkspace';

export type LabelAsset = { asset_code: string; name: string; company: string; model: string; location: string; department: string; qr_mode?: 'legacy' | 'native'; qr_raw?: string | null };
export type LabelTemplate = { id: string; version: number; name: string; width: number; height: number; qrSize: number; padding: number; fontSize: number; title: string; fields: string[] };
export type LabelCalibration = { x: number; y: number };
export const labelPublicOrigin = import.meta.env.VITE_PUBLIC_APP_URL || 'https://xz.vodayapp.com';
export const labelFields = { name: '资产名称', asset_code: '资产编码', company: '所属公司', model: '型号', location: '位置', department: '使用部门' };
export const sampleLabelAsset: LabelAsset = { asset_code: 'BJB-0001', name: '笔记本电脑', company: '重庆市九吨科技有限公司', model: 'ThinkPad X1', location: '16楼', department: '萌宠事业部', qr_mode: 'native' };
export const defaultLabelTemplate = (): LabelTemplate => ({ id: '', version: 1, name: '新标签样式', width: 50, height: 30, qrSize: 20, padding: 2, fontSize: 7, title: '', fields: ['name', 'asset_code'] });
export function assetLabelCode(path: string) { const match = path.match(/^\/a\/([^/]+)$/); return match && assetCodePattern.test(match[1]!) ? match[1]! : null; }
export function labelURL(code: string, origin = 'https://xz.vodayapp.com') {
  if (!assetCodePattern.test(code)) throw new Error('资产编码无效');
  const url = new URL(origin);
  if (!['http:', 'https:'].includes(url.protocol)) throw new Error('标签网址无效');
  return `${url.origin}/a/${encodeURIComponent(code)}`;
}
export function escapeLabelText(value: unknown) { return String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]!)); }
export function labelQRValue(asset: LabelAsset, origin = 'https://xz.vodayapp.com') {
  if (!assetCodePattern.test(asset.asset_code)) throw new Error('资产编码无效');
  if (asset.qr_mode === 'native') return labelURL(asset.asset_code, origin);
  const parsed = parseQr(asset.qr_raw || '');
  if (parsed.assetCode === asset.asset_code && parsed.format === 'gongbeiyun_url' && /^https?:\/\//i.test(parsed.raw)) return parsed.raw;
  // Legacy imports may not contain the original QR. The system scanner accepts
  // the asset number directly; never invent a former provider's tenant URL.
  return asset.asset_code;
}
export function labelQR(value: string) {
  const { modules } = QRCode.create(value, { errorCorrectionLevel: 'M' });
  const size = modules.size; let path = '';
  for (let y = 0; y < size; y++) for (let x = 0; x < size; x++) if (modules.get(y, x)) path += `M${x + 4},${y + 4}h1v1h-1z`;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size + 8} ${size + 8}" role="img" aria-label="资产二维码" shape-rendering="crispEdges"><rect width="100%" height="100%" fill="#fff"/><path d="${path}" fill="#000"/></svg>`;
}
const finite = (v: number, min: number, max: number) => Number.isFinite(v) && v >= min && v <= max;
export function labelTemplateProblem(t: LabelTemplate) {
  if (!t.name.trim() || t.name.length > 60) return '请填写60字以内的样式名称';
  if (!finite(t.width, 40, 100) || !finite(t.height, 25, 80) || !finite(t.qrSize, 16, 40) || !finite(t.padding, 1, 4) || !finite(t.fontSize, 6, 12)) return '请检查标签尺寸、二维码大小、边距和字号';
  if (t.qrSize + t.padding * 2 > t.height || t.width - t.padding * 2 - t.qrSize - 1 < 16) return '二维码尺寸过大，需为文字和边距留出空间';
  if (t.title.length > 20) return '标签标题最多20字';
  if (t.fields.length < 2 || t.fields.length > 4 || new Set(t.fields).size !== t.fields.length || t.fields.some(f => !Object.hasOwn(labelFields,f)) || !['name', 'asset_code'].every(f => t.fields.includes(f))) return '必须包含名称和编码，最多选择4个字段';
  return '';
}
export const labelContentCSS = `
.asset-label{position:relative;box-sizing:border-box;overflow:hidden;background:#fff;color:#000;font-family:"Microsoft YaHei","Noto Sans CJK SC",Arial,sans-serif;text-align:left;flex:none;}
.label-content{position:absolute;inset:0;box-sizing:border-box;display:flex;align-items:center;gap:1mm;}
.label-qr{flex:none;display:block;}.label-qr svg{display:block;width:100%;height:100%;}
.label-copy{min-width:0;flex:1;display:flex;flex-direction:column;gap:1.8mm;}
.label-title,.label-field{margin:0;line-height:1.3;overflow-wrap:anywhere;font-weight:700;}.label-title{font-size:1.15em;}
.label-title,.label-field:not([data-field="asset_code"]){display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden;}
`;
export function labelMarkup(t: LabelTemplate, asset: LabelAsset, origin: string, calibration: LabelCalibration = { x: 0, y: 0 }) {
  const problem = labelTemplateProblem(t); if (problem) throw new Error(problem);
  if (!finite(calibration.x, -3, 3) || !finite(calibration.y, -3, 3)) throw new Error('打印偏移应在 -3 至 3 mm 之间');
  const rows = t.fields.map(field => `<p class="label-field" data-field="${field}">${labelFields[field as keyof typeof labelFields]}：${escapeLabelText(field === 'department' ? assetDepartmentLabel(asset) : asset[field as keyof LabelAsset]) || '—'}</p>`).join('');
  return `<article class="asset-label" style="width:${t.width}mm;height:${t.height}mm;font-size:${t.fontSize}pt"><div class="label-content" style="padding:${t.padding}mm;transform:translate(${calibration.x}mm,${calibration.y}mm)"><div class="label-qr" style="width:${t.qrSize}mm;height:${t.qrSize}mm">${labelQR(labelQRValue(asset, origin))}</div><div class="label-copy">${t.title ? `<p class="label-title">${escapeLabelText(t.title)}</p>` : ''}${rows}</div></div></article>`;
}
export function labelPrintDocument(t: LabelTemplate, assets: LabelAsset[], copies: number, origin: string, calibration: LabelCalibration) {
  if (!assets.length || assets.length > 100 || !Number.isInteger(copies) || copies < 1 || copies > 20 || assets.length * copies > 1000) throw new Error('每件1至20张，一批最多1000张标签');
  const labels = assets.flatMap(asset => Array.from({ length: copies }, () => labelMarkup(t, asset, origin, calibration)));
  return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>九吨资产标签</title><style>@page{size:${t.width}mm ${t.height}mm;margin:0;}html,body{padding:0;margin:0;}body{-webkit-print-color-adjust:exact;print-color-adjust:exact;}*{box-sizing:border-box;}${labelContentCSS}.asset-label{break-after:page;page-break-after:always;break-inside:avoid;}.asset-label:last-child{break-after:auto;page-break-after:auto;}</style></head><body>${labels.join('')}</body></html>`;
}

// Check the actual rendered text and QR quiet zone before letting the user print.
export function overflowingLabels(root: HTMLElement): number {
  return [...root.querySelectorAll<HTMLElement>('.asset-label')].filter(label => {
    const bounds = label.getBoundingClientRect();
    return [...label.querySelectorAll<HTMLElement>('.label-qr,.label-copy')].some(el => {
      const box = el.getBoundingClientRect(); return box.left < bounds.left - .5 || box.right > bounds.right + .5 || box.top < bounds.top - .5 || box.bottom > bounds.bottom + .5 || el.scrollWidth > el.clientWidth + 1;
    });
  }).length;
}
