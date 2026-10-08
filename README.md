# 自有资产管理系统

当前版本是第一版可运行竖切片，包含：

- Web 资产看板和清单
- Excel 资产导入预览与确认
- 资产详情和流转时间线
- 二维码原文解析与资产编码查询
- Android 摄像头二维码扫描（ZXing）与手工输入兜底
- 领用、归还、借出、维修、报废事件接口
- 按位置创建盘点任务、扫码登记和差异清单
- Android 友好的响应式页面基础

## 本地运行

```powershell
npm install --registry=https://registry.npmmirror.com
npm run import -- "C:\Users\张洋华\Downloads\资产清单2026-10-08.xlsx"
npm run verify:seed
npm run dev
```

浏览器打开 `http://localhost:5173`。开发 API 运行在 `http://localhost:3000`，Vite 会代理 `/api` 请求。

首次登录使用 `admin / admin123`。部署前请设置 `ASSET_ADMIN_PASSWORD`（也可以设置 `ASSET_ADMIN_USERNAME`）再启动服务。管理员可在“系统设置”创建管理员、资产管理员和只读用户；未登录请求不会读取资产数据。

## Android APK

安卓 APK 使用构建时的 `VITE_API_BASE_URL` 连接电脑上的 API。手机和电脑需要在同一局域网，先让后端监听 `0.0.0.0`，再把电脑局域网 IP 写入构建参数：

```powershell
$env:VITE_API_BASE_URL="http://192.168.1.100:3000"
npm run android:sync
Push-Location android
gradle assembleDebug
Pop-Location
```

生成文件：`android/app/build/outputs/apk/debug/app-debug.apk`。如果只是浏览器本地开发，不设置该变量即可，Vite 会继续代理 `/api` 到 `localhost:3000`。

钉钉 H5 免登需要配置以下环境变量后重启后端：

```powershell
$env:DINGTALK_CLIENT_ID="你的AppKey"
$env:DINGTALK_CLIENT_SECRET="你的AppSecret"
$env:DINGTALK_CORP_ID="你的CorpId"
```

钉钉登录默认调用 `https://api.dingtalk.com/v1.0/oauth2/userAccessToken` 和 `https://api.dingtalk.com/v1.0/contact/userinfo`，如企业使用不同版本，可通过 `DINGTALK_TOKEN_URL`、`DINGTALK_USER_URL` 覆盖。首次通过钉钉登录的账号会自动创建为只读用户。

## 测试和构建

```powershell
npm test
npx tsc --noEmit
npm run build
```

## 数据和二维码

运行时数据库位于 `.data/asset.db`，不会提交到 Git。二维码只在系统内部解析为字符串，旧公贝云二维码的原文保存在 `qr_raw` 字段中，客户端不会自动打开该 URL。

## 当前范围

当前已完成 Android 原生包装、在线盘点、钉钉 H5 免登适配。离线盘点、YLF-280 USB 标签打印和真机兼容性验收继续分阶段接入；当前网页已按 Android 小屏设计。
