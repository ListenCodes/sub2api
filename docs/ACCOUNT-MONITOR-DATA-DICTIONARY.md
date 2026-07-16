# Account Monitor Data Dictionary

账号监控表全部位于 `risk-control-postgres`。主库只拥有 `extensions_self_ro` 安全视图，
不创建监控事实或聚合表。

## Facts

### `account_monitor_attempt_facts`

每个实际上游账号尝试一行，`event_key` 唯一。关键字段：

- `request_key`：把同一用户请求的多次重试关联起来。
- `account_id` / `parent_account_id`：物理账号与母账号。
- `actual_model` / `model_attribution`：实际上游模型及 `exact|estimated` 质量。
- `result`：`succeeded|failed`；`recovered` 表示失败后最终请求成功。
- `error_category`、状态码和 provider code：稳定中文一级分类及二级维度。
- Token、用户计费、账号成本、延迟、图片和视频字段：仅保存统计需要的数值。
- `identity_quality`：`exact|fallback`；`source_kind/source_id` 用于追踪来源。

### `account_monitor_request_facts`

每个用户最终请求一行，`request_key` 唯一。最终成功不会被前序失败重试改成失败；最终失败
也只计一次。`group_id`、`actual_model` 与 `model_attribution` 来自最终请求事实：成功覆盖失败，
始终失败使用最后一次失败尝试。缺失 `group_id` 的行不进入分组卡片。

事实表与分钟聚合保留 90 天。它们不含账号凭据、完整 API Key、请求体或请求头。

## Aggregates

| 表 | 粒度 | 主键/用途 | 保留 |
|---|---|---|---|
| `account_monitor_account_minute` | 账号/分钟 | 近期请求、成功、失败、恢复失败、成本、延迟 | 90 天 |
| `account_monitor_account_model_minute` | 账号/模型/分钟 | 实际模型近期指标 | 90 天 |
| `account_monitor_account_daily` | 账号/日 | 用户/API Key 去重、媒体、成本、P95 | 365 天（1 年） |
| `account_monitor_account_model_daily` | 账号/模型/日 | 模型调用和成本 | 365 天（1 年） |
| `account_monitor_account_user_daily` | 账号/用户/API Key/日 | 使用分布，API Key 只关联 ID/掩码 | 365 天（1 年） |
| `account_monitor_account_error_daily` | 账号/错误/日 | 失败分类、状态码、provider code | 365 天（1 年） |
| `account_monitor_group_model_10m` | 分组/实际模型/完整 10 分钟桶 | `bucket_at + group_id + actual_model`；总数、成功、失败及 exact/estimated | 90 天 |

## Control Tables

- `account_monitor_schema_migrations`：扩展 schema 版本。
- `account_monitor_sync_state`：`usage` 与 `error` 独立游标、最后成功时间和错误。
- `account_monitor_rebuild_jobs`：31 天以内重建任务、请求管理员、状态和处理行数。
- `account_monitor_thresholds`：global/platform/parent/account 分层阈值 JSON 与修改人。platform 的 `scope_id` 是规范化平台名的稳定 FNV-1a 64 位正整数映射（`PlatformScopeID`）。
- `account_monitor_group_dimensions`：分组 ID、名称、平台、状态、软删除时间和同步时间镜像；
  维度同步失败保留上一版，不删除既有分组。

## Safe Source Views

| 视图 | 内容 |
|---|---|
| `extensions_self_ro.usage_source` | 可计费用量、账号/模型、Token、成本、延迟和媒体值 |
| `extensions_self_ro.error_source` | 脱敏错误、重试数组和新事件的实际上游模型 |
| `extensions_self_ro.account_dimension` | 账号 ID、母账号、名称、平台和状态 |
| `extensions_self_ro.account_group_dimension` | 账号与其全部分组的多对多映射，以及分组名称、平台、状态、倍率和软删除时间；新增列必须追加以保持 `CREATE OR REPLACE VIEW` 升级兼容；不含账号凭据 |
| `extensions_self_ro.user_dimension` | 用户 ID、展示身份和状态 |
| `extensions_self_ro.api_key_dimension` | API Key ID、名称和固定掩码前缀 |
| `extensions_self_ro.group_dimension` | 分组 ID、名称、平台、状态和软删除时间；不含账号凭据 |

## Data Quality

- 主库错误明细保留期短于账号监控 90 天事实保留期，首次回填可能不完整。
- `model_attribution=estimated` 不能冒充精确上游模型。
- `identity_quality=fallback` 表示缺少请求标识，跨来源关联能力较弱。
- 未归属错误、采集停机、同步延迟和源连接失败都必须作为数据缺口显示。
- 历史范围不完整时只能报告实际可用区间，不能把缺口解释为零调用或零失败。
- 共享快照字段为 `data_as_of`、`collection_lag_seconds`、`stale_data_warning`、
  `usage_cursor`、`error_cursor`、`recent_source_error`、`available_from/to`、
  `missing_group_requests`、`exact_model_requests` 和 `estimated_model_requests`。
- 分组页面的四种时间范围都以最终请求事实为权威来源，在对齐的 UTC `[from,to)` 内精确汇总
  24 个展示桶：`6h=15m`、`24h=1h`、`7d=7h`、`30d=30h`。基础 10 分钟聚合不能近似转换
  15 分钟展示桶。
