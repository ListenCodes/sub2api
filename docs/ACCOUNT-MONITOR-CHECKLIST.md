# Account Monitor Release Checklist

代码完成不等于生产发布。按“验证 -> 合并/推送 -> 备份 -> 安装权限 -> 构建 -> 发布 ->
浏览器与数据对账”的顺序记录结果。

## Before Merge

- [ ] 功能分支基于批准的 `custom`，工作树干净。
- [ ] account-monitor、risk-control、后端、前端和 deploy 契约测试通过。
- [ ] `git diff --check` 通过，官方代码改动只包含模型归因、代理、路由、菜单和 i18n。
- [ ] 单个重建任务最大 31 天；重叠任务返回冲突。
- [ ] 90 天事实/分钟和 365 天日聚合保留策略有测试。
- [ ] 分组时间线测试覆盖应用使用 UTC、PostgreSQL 会话使用非 UTC 时区，等价瞬时桶不得被回填为零。

## Production Preparation

- [ ] 记录批准的 `origin/custom` commit、当前镜像 ID 和回滚 tag。
- [ ] `sub2api_db.dump` 与 `risk_control_db.dump` 都已生成并校验。
- [ ] Compose、`.env`、Nginx vhost、证书/私钥、容器和镜像元数据已备份。
- [ ] 两个 dump 均通过 `pg_restore --list`，`SHA256SUMS`、`release-metadata.env` 和匹配回滚 tag 已记录。
- [ ] `.env` 设置专用 DSN，且不在日志或文档中打印密码：

```text
ACCOUNT_MONITOR_ENABLED=true
ACCOUNT_MONITOR_SOURCE_DATABASE_URL=postgres://extensions_self_monitor:<URL-encoded-password>@postgres:5432/sub2api?sslmode=disable
```

- [ ] `deploy/ops/install-account-monitor-source.sql` 在构建前执行成功。
- [ ] 权限组探测通过：

```sql
BEGIN;
SET ROLE extensions_self_monitor_ro;
SELECT 1 FROM extensions_self_ro.usage_source LIMIT 1;
SELECT 1 FROM extensions_self_ro.group_dimension LIMIT 1;
ROLLBACK;
```

- [ ] `extensions_self_monitor` 可经 TCP 登录并读取视图，不能读取
      `public.api_keys.key` 或 `public.accounts.credentials`。
- [ ] Docker Compose `config --quiet` 通过；运行时只有一个 `extensions-self` 应用容器。

## Publish And Verify

- [ ] 只重建 `sub2api` 与 `extensions-self`；不重建 `risk-control-postgres`。
- [ ] 主应用 `/health`、`extensions-self /healthz` 和首页代理正常。
- [ ] 签名 `/api/v1/admin/account-monitor/data-quality` 正常，旧静态 `/account-monitor/` 路由不存在。
- [ ] 未认证访问 `/api/v1/admin/extensions-self/account-monitor/data-quality` 被管理员鉴权拒绝。
- [ ] 管理员 `/admin/extensions/account-monitor` 在桌面和移动视口可加载、筛选、翻页、展开账号。
- [ ] 管理员 `/admin/extensions/group-monitor` 可筛选、分页并打开实际模型详情。
- [ ] 侧边栏“扩展中心”可展开用户风控、账号监控、分组监控三个子菜单，父页面不重复一级页签。
- [ ] 风控三页和自定义首页无回归。
- [ ] 抽样对账成功、失败、重试后成功、模型、Token、成本、图片和视频。
- [ ] `data-quality` 显示最近同步、延迟、未归属错误和 exact/estimated 比例。
- [ ] 主库现存错误范围不足时，页面明确显示数据缺口。
- [ ] 对实际 `available_from/to` 执行 `deploy/ops/backfill-account-monitor.sh`；每段不超过 31 天、
      不重叠，`backfill-jobs.tsv` 中所有 job 均 completed 并记录 `processed_rows`。
- [ ] 抽样核对分组总数满足 `total_requests=successes+failures`，缺失分组只进入数据质量。

## Troubleshooting

```bash
docker compose --project-name deploy -f deploy/docker-compose.yml --env-file deploy/.env config --quiet
docker logs --tail=200 extensions-self
docker inspect extensions-self --format '{{.State.Health.Status}}'
docker exec extensions-self wget -qO- http://127.0.0.1:8090/healthz
docker exec sub2api-postgres psql -U sub2api -d sub2api -c '\dp extensions_self_ro.*'
```

查询同步游标、重建任务和事实计数时，应进入 `risk-control-postgres` 并使用生产 `.env`
中的扩展数据库用户/库名。不要把密码拼进共享命令或截图。

## Rollback

- [ ] 先设置 `ACCOUNT_MONITOR_ENABLED=false` 并恢复匹配的 Compose/环境备份。
- [ ] 将 `sub2api:rollback-<timestamp>` 和 `deploy-extensions-self:rollback-<timestamp>` 恢复为活动 tag。
- [ ] 回滚目标从同一备份目录的 `release-metadata.env` 读取，不能混用不同发布点镜像和配置。
- [ ] 只重建两个应用服务；不要删除 `risk-control-postgres`、数据卷或账号监控表。
- [ ] 如果 schema/data 已损坏，再从同一发布点的 `risk_control_db.dump` 恢复；普通代码回滚不恢复数据库。
- [ ] 重跑主应用、扩展、首页、风控和公网健康检查，记录失败 commit、回滚目标和原因。
