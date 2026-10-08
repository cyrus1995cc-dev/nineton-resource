# 自有资产管理系统

当前版本是第一版可运行竖切片，包含：

- Web 资产看板和清单
- Excel 资产导入预览与确认
- 资产详情和流转时间线
- 二维码原文解析与资产编码查询
- 领用、归还、借出、维修、报废事件接口
- Android 友好的响应式页面基础

## 本地运行

```powershell
npm install --registry=https://registry.npmmirror.com
npm run import -- "C:\Users\张洋华\Downloads\资产清单2026-10-08.xlsx"
npm run verify:seed
npm run dev
```

浏览器打开 `http://localhost:5173`。开发 API 运行在 `http://localhost:3000`，Vite 会代理 `/api` 请求。

## 测试和构建

```powershell
npm test
npx tsc --noEmit
npm run build
```

## 数据和二维码

运行时数据库位于 `.data/asset.db`，不会提交到 Git。二维码只在系统内部解析为字符串，旧公贝云二维码的原文保存在 `qr_raw` 字段中，客户端不会自动打开该 URL。

## 当前范围

Android 原生包装、钉钉免登、离线盘点和 YLF-280 打印分阶段接入。当前网页已按 Android 小屏设计，并预留 Capacitor 包装边界。
