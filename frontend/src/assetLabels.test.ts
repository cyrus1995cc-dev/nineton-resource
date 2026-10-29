import { describe, expect, it } from 'vitest';
import { assetLabelCode, defaultLabelTemplate, labelMarkup, labelPrintDocument, labelQR, labelTemplateProblem, labelURL, sampleLabelAsset } from './assetLabels';
import { parseQr } from '../shared/qr';
describe('asset labels', () => {
  it('uses a new native URL without changing the historic QR value', () => {
    const asset = { ...sampleLabelAsset, qr_raw: 'https://d.gongbeiyun.com/a/BJB-0001' };
    const url=labelURL(asset.asset_code);
    expect(url).toBe('https://xz.vodayapp.com/a/BJB-0001');
    expect(parseQr(url).assetCode).toBe(asset.asset_code);
    expect(asset.qr_raw).toContain('gongbeiyun');
    expect(assetLabelCode('/a/BJB-0001')).toBe('BJB-0001');
    for (const path of ['/a/<script>', '/a/BJB-0001/extra', '//evil.test/a/BJB-0001']) expect(assetLabelCode(path)).toBeNull();
  });
  it('escapes label text and rejects unsafe template configuration', () => {
    const t=defaultLabelTemplate();t.title='<img src=x>';
    const html=labelMarkup(t,{...sampleLabelAsset,name:'<script>alert(1)</script>'},'https://xz.vodayapp.com');
    expect(html).not.toContain('<script>');expect(html).not.toContain('<img');expect(html).toContain('&lt;script&gt;');
    expect(labelTemplateProblem({...t,qrSize:30})).toContain('二维码尺寸过大');
    expect(labelTemplateProblem({...t,fields:['name','name']})).toContain('必须包含');
    expect(()=>labelURL('bad')).toThrow('编码无效');
  });
  it('creates fixed physical pages and explicit copies, with an unprinted four-module QR quiet zone', () => {
    const doc=labelPrintDocument(defaultLabelTemplate(),[sampleLabelAsset,{...sampleLabelAsset,asset_code:'BJB-0002'}],3,'https://xz.vodayapp.com',{x:0,y:0});
    expect(doc).toContain('@page{size:50mm 30mm;margin:0;}');
    expect(doc.match(/<article class="asset-label"/g)).toHaveLength(6);
    expect(doc).not.toContain('gongbeiyun');
    expect(labelQR(labelURL('BJB-0001'))).toContain('M4,4');
    expect(()=>labelPrintDocument(defaultLabelTemplate(),[sampleLabelAsset],0,'https://xz.vodayapp.com',{x:0,y:0})).toThrow('最多');
  });
});
