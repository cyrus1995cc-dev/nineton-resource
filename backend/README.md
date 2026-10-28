# 后端

`cmd/server/` 为 Go 服务入口，`cmd/migrate/` 为数据库工具，`internal/app/` 包含业务接口、MySQL 事务和测试。`legacy-node/` 保存旧实现用于核对，不参与 Go 构建和生产发布。

```powershell
$env:MYSQL_DSN='asset_dev:password@tcp(127.0.0.1:3306)/asset_system_dev'
go run ./cmd/migrate
$env:ASSET_ADMIN_PASSWORD='首次初始化时设置至少十二位密码'
go run ./cmd/server
```

MySQL 8.4、UTC、InnoDB；默认连接池最多 8。Go 默认软内存目标 384 MiB，导入导出最多并行 1 个。具体配置与发布流程见 [部署说明](../deploy/README.md)。
