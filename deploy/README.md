# Go + MySQL 部署（2 核 2 GiB 起步）

目录：`frontend/` 为 Vue 前端及安卓工程；`backend/` 为 Go API；`deploy/` 为服务配置。旧 Node 后端放在 `backend/legacy-node/`，仅作业务核对和回退参考，不随发布包交付。

## 本地开发与验证

构建机安装 Go 1.26.5 或更新兼容版本、Node.js 24 及 npm。服务器不需要这些编译工具。

```powershell
npm --prefix frontend ci
npm --prefix frontend run dev
# 另开终端，使用独立的开发数据库
$env:MYSQL_DSN='asset_dev:password@tcp(127.0.0.1:3306)/asset_system_dev'
go -C backend run ./cmd/migrate
$env:ASSET_ADMIN_PASSWORD='首次初始化使用的强密码'
go -C backend run ./cmd/server
```

Vite 将 `/api` 代理到 `127.0.0.1:3000`。启动服务默认不自动建表；`cmd/migrate` 使用升级账号建表，运行服务使用业务账号。已有用户表非空时不会根据环境变量新增默认管理员或重置已有密码。

```powershell
npm --prefix frontend run typecheck
npm --prefix frontend test
$env:ASSET_TEST_MYSQL_DSN='test_user:password@tcp(127.0.0.1:3306)/asset_system_test'
go -C backend test ./... -count=1
go -C backend vet ./...
.\scripts\build-release.ps1
```

集成测试会清空指定测试库，必须使用专用库，库名含 `_test`。未设置测试连接时集成测试明确跳过；不能将仅单元测试通过视为数据库验证。发布脚本输出 `release/linux-amd64/`，包含 Go 服务、迁移程序、前端静态文件、部署配置与 SHA256 清单。发布包没有 Node.js 或 Go 编译器依赖，Go 使用 `CGO_ENABLED=0` 构建。

## Ubuntu 服务器安装

推荐 Ubuntu Server 24.04 LTS x86_64、2 核 2 GiB、SSD；容量需按真实并发验收。安装 Nginx、MySQL Community Server 8.4 LTS 和 CA 根证书，校准系统时间。对外只开放 HTTPS；数据库仅监听回环地址。钉钉登录与推送的真实企业联调仍需要企业应用配置。

1. 创建 `/opt/asset-system` 和无登录权限的系统用户 `asset-system`，解压发布包；程序文件由运维账号持有，服务用户仅可读取与执行。对 `asset-server`、`asset-migrate` 执行 `chmod 0755`。
2. 将 `mysql-small.cnf` 放入 MySQL 配置目录，验证并重启数据库。256 MiB 缓存只是起点，不能视为 MySQL 的总内存上限。连接池默认 8，Go `GOMEMLIMIT=384MiB` 为软内存目标，systemd 设置 640 MiB 进程硬上限。
3. 创建库 `asset_system`，字符集 `utf8mb4`、排序规则 `utf8mb4_0900_bin`。创建仅允许本机连接的升级账号和业务账号；升级账号具有建表及迁移权限，业务账号只授予本库 `SELECT, INSERT, UPDATE, DELETE`。密码使用随机强密码。
4. 复制 `asset-system.env.example` 为 `/etc/asset-system.env`，填写业务连接串、钉钉配置和首次初始化密码，权限设为 `0600`。密码不放入命令行历史或 Git。含 DSN 特殊字符的密码按 MySQL Go 驱动格式正确配置。
5. 使用升级账号的 `MYSQL_DSN` 执行 `./asset-migrate`，或按下一节从 SQLite 导入。完成后用业务账号启动程序。
6. 安装 `asset-system.service` 到 `/etc/systemd/system/`，执行 `systemctl daemon-reload` 与 `systemctl enable --now asset-system`。
7. 将 `nginx.conf` 中的域名、证书路径改为实际值，检查 `nginx -t` 后启用。前端静态文件由 Nginx 提供，`/api` 转发至 Go。
8. 通过 HTTPS 检查 `/api/health`、登录、资产查询、Excel、审批、盘点与钉钉通讯录。安卓重新同步并打包；钉钉移动端和 PC 入口使用相同 HTTPS 地址。

## SQLite 正式迁移与回退

迁移工具只读取 SQLite，保留资产与人员 ID、二维码原文/别名、密码哈希、审批和盘点历史；旧登录会话不迁移，用户需要重新登录。目标库只要任一业务表非空即拒绝导入，禁止覆盖已有数据。

1. 备份原 SQLite，停止旧服务写入；如有 WAL，使用 SQLite backup API 生成一致性快照，不可只复制仍在写入的 `.db` 主文件。
2. 将快照传到受控目录，用升级账号执行 `./asset-migrate -sqlite /path/to/snapshot.db`。工具先检查源库完整性，在一个 MySQL 事务中导入；任何错误回滚整批业务数据。MySQL DDL 可能已建好空表，失败后检查空库再重试。
3. 校验各表数量、资产编码、二维码、人员关系、金额、密码兼容及全部关键业务。切换入口到新服务前完成恢复验证。
4. 保留源库和原版本。切换后只允许新库写入；需要回退时先停新服务并制定新增数据回迁方案，不能直接丢弃新系统产生的单据。

`backup.sh` 使用独立的 MySQL 客户端配置文件读取备份凭据，生成一致性压缩备份；由运维配置每日任务、7～30 天保留周期和至少一份异机副本，定期在隔离库恢复验证。

## 公司管理与组织通讯录升级（2026-10-28）

本次新增 `companies`、`company_names`、`employee_departments` 三张表。停止旧服务，用升级账号执行新版本 `asset-migrate`，随后更新 Go 程序和前端并启动。初始化使用现有台账中的公司名称，自动生成的 C0001 等编码可由管理员修改，上级关系由管理员设置。

公司在“公司管理”手工新增和修改，钉钉同步不会覆盖公司资料。改名保留历史台账文字及旧名关联。公司可选绑定钉钉部门，员工按最近祖先的绑定归属；未绑定部门可在同步设置指定默认公司，也可保持未分配。

填写服务端环境 `DINGTALK_CORP_ID`、`DINGTALK_CLIENT_ID`、`DINGTALK_CLIENT_SECRET` 并重启。开放平台需授予部门及人员详情读取权限和通讯录授权范围。在“组织与人员 → 同步设置与记录”设置起始部门及手动/定时同步。页面展示部门树、人员、所属公司、工号、职务及多部门关系。同步是后台任务，最长 15 分钟，页面可关闭；失败保留原数据，记录错误，人员缺失标为待核对。真实企业联调需要实际应用凭证，本地模拟测试不能代替真实接口验收。

## 2G 内存验收

使用实际数据同时查询、扫码、审批，另测 Excel 导入导出。记录 Go 和 MySQL 的 RSS、系统可用内存、交换活动、接口延迟及失败数。导入导出共用一个并发槽，忙时返回 429，登录密码计算最多同时 2 个。CPU 与内存预算不构成支持人数承诺；出现持续内存压力或明显延迟时调优或升配。当前 Windows 本地功能测试不能替代目标 Linux 服务器 2 核 2G 的容量验收。
