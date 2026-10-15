import { Capacitor, registerPlugin } from '@capacitor/core';
import type { DownloadFile } from './apiClient';

const ExportFile = registerPlugin<{ save(input: { filename: string; base64: string }): Promise<{ cancelled?: boolean }> }>('ExportFile');
export async function saveDownload(file: DownloadFile): Promise<string> {
  if (Capacitor.getPlatform() === 'android') {
    if (!Capacitor.isPluginAvailable('ExportFile')) throw new Error('请安装新版安卓包后导出，或使用电脑版网页导出');
    if (file.blob.size > 16 * 1024 * 1024) throw new Error('文件超过手机保存上限，请缩小筛选范围或在电脑导出');
    const base64 = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result).split(',')[1]);
      reader.onerror = () => reject(new Error('读取导出文件失败，请重试'));
      reader.readAsDataURL(file.blob);
    });
    const result = await ExportFile.save({ filename: file.filename, base64 });
    return result.cancelled ? '已取消保存' : 'Excel 已保存到所选位置';
  }
  const url = URL.createObjectURL(file.blob); const link = document.createElement('a');
  link.href = url; link.download = file.filename; document.body.appendChild(link); link.click(); link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30000);
  return 'Excel 已生成，请在浏览器下载列表查看';
}
