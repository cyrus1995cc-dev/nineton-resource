# 前端

Vue 页面、组件和样式在 `src/`；共享前端类型与二维码规则在 `shared/`；安卓工程在 `android/`。这里的 `package.json` 与锁文件只管理前端依赖。

```powershell
npm ci
npm run dev
npm run typecheck
npm test
npm run build
npm run android:sync
```

开发时 Go API 默认监听 `127.0.0.1:3000`，Vite 代理 `/api`。生产仅交付 `dist/` 静态文件；在本地构建时根据入口设置 `VITE_API_BASE_URL`，网页通常保持同源。完整部署见 [部署说明](../deploy/README.md)。

标签二维码默认指向 `https://xz.vodayapp.com/a/资产编码`；其他域名部署需在构建前设置 `VITE_PUBLIC_APP_URL` 为可从手机访问的正式站点地址。
