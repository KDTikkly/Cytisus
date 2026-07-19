# Cytisus v1 本地操作手册

> 仅用于本机合成数据模拟。不要输入真实身份、银行、Card、Crypto、密钥或客户资料；不要把本部署对外网开放或用于真钱业务。

## 1. 启动

前提：Docker Desktop 已启动，Docker Compose v2 可用。仓库根目录执行：

```powershell
docker compose --env-file .env.example -f deploy/docker-compose.yml up --build --wait
```

如果 Windows/Docker 无法绑定默认 PostgreSQL 宿主端口 `5432`，可改用：

```powershell
$env:POSTGRES_HOST_PORT="55432"
docker compose --env-file .env.example -f deploy/docker-compose.yml up --build --wait
```

这只改变宿主机调试端口；容器内应用仍连接 `postgres:5432`。

该命令会启动 PostgreSQL、Redis、迁移、API、Worker、Provider Simulator、Anvil、Web、Admin、Mailpit 和 MinIO。首次构建可能需要几分钟。

## 2. 打开与检查

| 用途               | 地址                          |
| ------------------ | ----------------------------- |
| Web 用户端         | http://localhost:3000         |
| Admin 模拟后台     | http://localhost:3001         |
| API 健康检查       | http://localhost:8080/healthz |
| Provider Simulator | http://localhost:8090/healthz |
| Mailpit            | http://localhost:8025         |
| MinIO 控制台       | http://localhost:9001         |

快速检查所有容器：

```powershell
docker compose --env-file .env.example -f deploy/docker-compose.yml ps
```

健康服务应显示 `healthy`，一次性 `migrate` 和 `rwa-deploy` 应成功退出。

## 3. 最短体验流程

1. 打开 Web，保留默认 Fixture ID `web.paper-demo`，点击 **Create paper account**。它只创建合成账户和 100,000 模拟 USD。
2. 搜索 `AAPL` 或 `SPY`，查看带 `Simulated` 状态的报价，提交 Paper Market/Limit order，并使用 Replay 推进确定性成交。
3. 页面下方可继续体验 Banking、Crypto 和 Card；所有账户、地址、报价和资金都必须使用 Fixture/Simulator 数据。
4. 打开 Admin，使用默认的两个不同合成操作员 `sim-maker-a` 与 `sim-checker-b` 查看案件、Maker-Checker 和对账流程。

Admin 身份当前是开发态请求头，不是真实登录系统。只可在受信任本机使用。

## 4. 日志、重启与停止

查看日志：

```powershell
docker compose --env-file .env.example -f deploy/docker-compose.yml logs --tail 100 api worker web admin-web
```

重新构建并启动：

```powershell
docker compose --env-file .env.example -f deploy/docker-compose.yml up --build --wait
```

停止本地部署：

```powershell
docker compose --env-file .env.example -f deploy/docker-compose.yml down
```

此停止命令保留 PostgreSQL 和 MinIO 命名卷。不要在需要保留证据时删除 volumes。

## 5. 常见问题

- 端口占用：检查 `3000`、`3001`、`5432`、`6379`、`8080`、`8090`、`8545`、`8025`、`9000` 和 `9001`。
- 服务未健康：先运行 `docker compose ... ps`，再用上面的日志命令查看失败服务。
- 页面仍是旧版本：重新执行 `up --build --wait`，然后强制刷新浏览器。
- iOS：需要 macOS 26/Xcode；Windows 本地部署只提供 Web/Admin/API。GitHub CI 已验证 Swift test 和 iOS Simulator build。

## 6. 生产边界

Cytisus v1 当前明确为 **NOT PRODUCTION READY**。真实部署仍需要法律、牌照、Broker/Bank/Custody/Card/Market Data/RWA 合作方、人类安全审批、真实身份认证、网络隔离、KMS/HSM、生产 Secret、监控、备份与灾难恢复。Simulator 在生产必须完全禁用。
