# Extensions-Self

`extensions-self` 是 Sub2API 自定义扩展的统一运行单元。生产环境只运行一个
`extensions-self` 应用容器，并由同一个 `origin/custom` commit 构建以下能力：

| 模块 | 源码 | 数据归属 | HTTP 路径 |
|---|---|---|---|
| 风控 | `risk-control/` | `risk-control-postgres` | `/api/v1/*` |
| 账号与分组监控 | `account-monitor/` | `risk-control-postgres` | `/api/v1/admin/account-monitor/*` |
| 自定义首页 | `homepage/` | 静态文件 | `/homepage/*` |

主应用负责登录、管理员鉴权、合规确认和同源代理。扩展进程负责自己的业务逻辑，
不能成为 Sub2API 主请求链路的硬依赖。账号监控只通过主库
`extensions_self_ro` 安全视图和专用登录角色 `extensions_self_monitor` 读取脱敏数据；
它不能读取账号凭据、完整 API Key、请求体或请求头。

## Build And Test

```bash
cd extensions-self/account-monitor && go test ./...
cd ../risk-control && go test ./...
docker build -f extensions-self/Dockerfile extensions-self
```

生产镜像由 `deploy/ops/publish-custom.sh` 构建。代码完成不等于生产发布；合并、
推送、备份、发布和生产验证必须分别记录。详细架构见
[`../docs/EXTENSIONS-SELF-ARCHITECTURE.md`](../docs/EXTENSIONS-SELF-ARCHITECTURE.md)，
账号监控操作见
[`account-monitor/README.md`](account-monitor/README.md)。
