# 旧后端参考实现

这是迁移前的 TypeScript + Express + SQLite 后端，保留用于行为核对和既有测试。正式服务使用上级目录的 Go 程序。不要同时运行旧库写入与新库写入。

这里的 npm 依赖只用于旧实现验证，不随 Go 发布包交付。

```powershell
npm ci
npm test
```

旧库默认相对进程工作目录解析，需显式设置 `DB_PATH` 指向原 `.data/asset.db`；任何回退写入前先停止新系统并处理切换后的新增数据。
