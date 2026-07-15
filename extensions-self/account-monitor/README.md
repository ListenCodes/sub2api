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

查询区间不得超过 90 天，`page_size` 只接受 20、50、100，请求体最大 256 KiB。API 只接受主应用
代理生成的 HMAC 时间戳、nonce、签名和管理员 actor ID。

阈值按 `global -> platform -> parent -> account` 依次覆盖。`platform` 作用域的
`scope_id` 使用 `PlatformScopeID`：对去空格、转小写后的平台名计算 FNV-1a 64 位哈希，
再清除符号位；该映射稳定且不依赖主库内部枚举。

静态页面由扩展 `/account-monitor/` 提供，通过主应用鉴权后的
`/api/v1/extensions-self/account-monitor/` 加载。Vue 路由 `/admin/account-monitor` 只是
同源 iframe 薄壳，不包含统计业务逻辑。

## Configuration

| 变量 | 默认值 | 说明 |
|---|---:|---|
| `ACCOUNT_MONITOR_ENABLED` | `false` | 采集器和页面总开关 |
| `ACCOUNT_MONITOR_SOURCE_DATABASE_URL` | 空 | 启用时必填的专用只读 DSN |
| `ACCOUNT_MONITOR_POLL_SECONDS` | `60` | 采集周期 |
| `ACCOUNT_MONITOR_LOOKBACK_SECONDS` | `300` | 晚到事件回看窗口 |
| `ACCOUNT_MONITOR_BATCH_SIZE` | `1000` | 每页最大来源行数 |
| `ACCOUNT_MONITOR_QUERY_TIMEOUT_MS` | `3000` | 主库查询超时 |
| `EXTENSIONS_SELF_ACCOUNT_MONITOR_WEB_DIR` | `/app/account-monitor` | 静态页面目录 |

## 数据质量和排障

页面的 `data-quality` 必须和业务指标一起检查：最近同步时间、同步延迟、源库连接、
未归属错误、重试恢复失败、`exact/estimated` 模型比例和 fallback 请求标识。旧错误记录、
主库保留期和采集停机都可能造成不完整区间；“没有数据”不等于“没有调用”。

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
