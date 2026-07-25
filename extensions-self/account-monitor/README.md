# Extensions-Self 账号监控

账号监控从 Sub2API 主库的安全只读视图增量采集成功用量和上游失败事件，生成可幂等
更新的账号尝试事实、用户最终结果、分钟/日聚合和异常状态。它与风控共用
`extensions-self` 进程和 `risk-control-postgres`，但代码包、表、路由和测试互相独立。

## 统计口径

- 账号尝试成功率 = 成功账号尝试 / 总账号尝试
- 用户最终成功率 = 最终成功请求 / 用户请求总数
- 成功用量沿用主仓库口径 `actual_cost > 0`，零成本失败占位行不算成功。
- 一次用户请求可包含多个失败账号尝试和一个最终成功尝试。失败尝试仍计入账号失败；
  最终请求记为成功，前面的失败标记为“重试后成功” (`recovered=true`)。
- 最终失败只生成一次用户结果；路由前拒绝且没有实际账号时不会虚构账号尝试。
- `model_attribution=exact` 表示错误事件记录了实际上游模型；
  `model_attribution=estimated` 表示兼容旧记录时只能回退到请求模型。
- `identity_quality=fallback` 表示缺少稳定请求 ID，只能用来源行 ID 构造幂等键。
- 最终请求事实同时保存 `group_id`。最终成功覆盖前序失败的分组和实际模型；始终失败使用
  最后一次失败尝试。缺失分组的请求不进入分组卡片，只计入数据质量。

## 账号风险分

风险分是所有命中项之和并截断到 100：认证/额度 `min(90,70+5*(次数-阈值))`；低成功率
`min(60,40+round(20*(阈值-实际率)/阈值))`；连续模型失败
`min(60,40+4*(次数-阈值))`；限流/过载最高 35；30 分钟无成功 25；延迟异常 20；
单用户集中 20；流量异常 15。`0–19/20–39/40–69/70–100` 分别对应正常、关注、异常、严重。
没有任何有效样本时 `risk_score_available=false`，页面显示不可用而不是伪造 0 分。
普通账号列表在数据库内分页；风险分筛选或排序会先对业务过滤后的候选账号完整评分。单次最多
允许 5000 个候选账号，超过时 API 返回 `422` 并要求收窄条件，不做近似排序或静默截断。

## 数据和权限边界

```text
Sub2API PostgreSQL
  -> extensions_self_ro 安全视图
  -> extensions_self_monitor 专用只读登录
  -> account-monitor collector
  -> risk-control-postgres 事实/聚合/游标/阈值/任务
```

源连接必须使用 `ACCOUNT_MONITOR_SOURCE_DATABASE_URL`，账号固定为
`extensions_self_monitor`。权限组 `extensions_self_monitor_ro` 保持 `NOLOGIN`；发布器创建
登录角色并只授予该权限组。安全视图不暴露账号凭据、完整 API Key、请求体、请求头、
OAuth token 或 cookie。账号监控数据库也只保存 ID、脱敏快照、分类和聚合值。

## 增量、保留和重建

- 默认每 60 秒采集，成功日志和错误日志使用独立 `(created_at, id)` 游标。
- 每次从游标前回看 5 分钟；事实按 `event_key` / `request_key` upsert，重复读取不重复计数。
- 事实和分钟聚合保留 90 天，日聚合保留 365 天（1 年）。
- 分组维度镜像保留软删除状态；`account_monitor_group_model_10m` 按
  `bucket_at + group_id + actual_model` 幂等聚合，只读取已完整结束的 10 分钟桶并保留 90 天。
- 分组页面的 `6h/24h/7d/30d` 展示区间始终生成 24 个桶，粒度依次为
  15 分钟、1 小时、7 小时和 30 小时。展示桶从最终请求事实按同一 `[from,to)` UTC 区间精确汇总；
  10 分钟表继续用于完整基础聚合，不能把 10 分钟桶近似拆成 15 分钟桶。
- 主库错误明细通常只保留 30 天，首次回填超过现存范围时会产生真实数据缺口，不能外推。
- 单个重建任务最长 31 天；重叠任务由 PostgreSQL advisory lock 拒绝。
- 采集失败保留旧结果并指数退避，最长 15 分钟；主请求链路不等待采集器。

## HTTP Contract

扩展内部签名 API 前缀是 `/api/v1/admin/account-monitor`。主应用管理员代理前缀是
`/api/v1/admin/extensions-self/account-monitor`，包含：

- `GET /overview`
- `GET /accounts`、`GET /accounts/:id`
- `GET /accounts/:id/models|users|errors|trends`
- `GET /attempts`
- `GET /data-quality`
- `GET|PUT /thresholds`
- `POST /rebuild-jobs`、`GET /rebuild-jobs/:id`
- `GET /group-monitor/groups`、`GET /group-monitor/groups/:id`

查询区间不得超过 90 天，`page_size` 接受 5 至 1000 的整数；管理页面必须提供 1000 条选项并
保持用户选择。请求体最大 256 KiB。API 只接受主应用
代理生成的 HMAC 时间戳、nonce、签名和管理员 actor ID。

阈值按 `global -> platform -> parent -> account` 依次覆盖。`platform` 作用域的
`scope_id` 使用 `PlatformScopeID`：对去空格、转小写后的平台名计算 FNV-1a 64 位哈希，
再清除符号位；该映射稳定且不依赖主库内部枚举。

页面由主应用原生 Vue 组件提供，入口为 `/admin/extensions/account-monitor`；分组监控入口为
`/admin/extensions/group-monitor`。浏览器只调用主应用管理员代理，扩展不提供 iframe、静态页面
或公开代理路由。兼容路由 `/admin/account-monitor` 只负责保留查询参数并重定向到原生页面。

## Configuration

| 变量 | 默认值 | 说明 |
|---|---:|---|
| `ACCOUNT_MONITOR_ENABLED` | `false` | 采集器和管理 API 总开关 |
| `ACCOUNT_MONITOR_SOURCE_DATABASE_URL` | 空 | 账号监控启用时必填；首页实时倍率启用时也必须配置的专用只读 DSN |
| `ACCOUNT_MONITOR_POLL_SECONDS` | `60` | 采集周期 |
| `ACCOUNT_MONITOR_LOOKBACK_SECONDS` | `300` | 晚到事件回看窗口 |
| `ACCOUNT_MONITOR_BATCH_SIZE` | `1000` | 每页最大来源行数 |
| `ACCOUNT_MONITOR_QUERY_TIMEOUT_MS` | `3000` | 主库查询超时 |

## 数据质量和排障

页面的共享数据质量快照包含 `data_as_of`、`collection_lag_seconds`、usage/error 游标、
`recent_source_error`、`available_from/to`、缺失 `group_id` 数和 `exact/estimated` 最终模型数。
账号页还显示源库连接、未归属错误、重试恢复失败和 fallback 请求标识。旧错误记录、
主库保留期和采集停机都可能造成不完整区间；“没有数据”不等于“没有调用”。

大于 31 天的历史范围必须使用 `deploy/ops/backfill-account-monitor.sh` 分段。脚本串行提交、
轮询并记录每个 job；任一 job 失败立即停止。回填只覆盖 `available_from/to` 的实际源范围，
不生成零桶或外推已清理历史。

```bash
docker logs --tail=200 extensions-self
docker exec extensions-self wget -qO- http://127.0.0.1:8090/healthz
docker exec risk-control-postgres psql -U risk_control_app -d risk_control \
  -c 'SELECT source,cursor_time,cursor_id,last_success_at,last_error FROM account_monitor_sync_state'
```

实际用户名/数据库名以生产 `.env` 为准；示例只展示表和字段，不应把凭据写入命令历史。
完整数据字典和发布检查清单见
[`../../docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md`](../../docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md)
与 [`../../docs/ACCOUNT-MONITOR-CHECKLIST.md`](../../docs/ACCOUNT-MONITOR-CHECKLIST.md)。
生产 commit、双镜像 digest、备份、回填、对账和回滚记录保存在对应发布备份目录；仓库仅
保留可重复使用的数据字典、检查清单和发布手册。
