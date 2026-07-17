# Sub2API 自定义版项目与生产运维指南

本文是 Sub2API 自定义部署的项目介绍和日常操作入口，面向开发者、管理员和接手运维的 Agent。
详细脚本契约仍以 `AGENTS.md`、`deploy/RELEASE-RUNBOOK.md` 和
`deploy/ops/README.md` 为准；本文负责说明这些组件如何组成一条可重复执行的发布流程。

## 1. 项目概览

| 项目 | 当前约定 |
|---|---|
| 用户 Fork | `origin` -> `ListenCodes/sub2api` |
| 官方仓库 | `upstream` -> `Wei-Shaw/sub2api` |
| 唯一生产分支 | `origin/custom-release` |
| 前瞻兼容分支 | `custom`，只测试 `upstream/main`，禁止发布 |
| 生产源码 | `/root/sub2api` |
| 主应用镜像 | `ghcr.io/listencodes/sub2api-custom@sha256:<digest>` |
| 扩展镜像 | `ghcr.io/listencodes/sub2api-extensions@sha256:<digest>` |
| 发布状态 | `/var/lib/docker/volumes/deploy_sub2api_data/_data/release-state.json` |

具体域名、服务器别名、IP、证书和现场凭据不属于 `custom-release` 的版本化契约，不写入本仓库文档。
执行生产操作时，从当前环境的外部 VPS 资产清单和站点访问台账解析这些信息。

生产由以下部分组成：

- `sub2api`：主 API、管理后台、代理网关以及扩展代理入口。
- `extensions-self`：风控、账号监控、分组监控和自定义首页。
- `sub2api-postgres`：主业务数据库。
- `risk-control-postgres`：风控和账号监控扩展数据库。
- `sub2api-redis`：主应用缓存和运行时状态。
- Nginx + 外部 DNS/CDN：当前环境配置的公网入口和源站 TLS。

主应用和 `extensions-self` 是一个发布单元：即使只修改其中一侧，也必须从同一完整 commit SHA
生成双镜像并按 digest 成对发布。`risk-control-postgres` 独立存在，发布和回滚不得重建或替换它。

## 2. 权威来源和边界

日常判断按以下优先级取证：

1. Git 代码和规则：`origin/custom-release`。
2. 官方稳定版本：`Wei-Shaw/sub2api /releases/latest`。
3. Actions 结果：目标 commit 对应的 `Custom Release` workflow。
4. 镜像身份：GHCR manifest、digest 和 OCI 标签。
5. 生产事实：`release-state.json`、当前容器和健康检查。

不要根据某个旧 Actions 链接、可变镜像 tag、日期目录或聊天记忆判断当前生产版本。
代码完成、合并推送、Actions/GHCR 完成和生产发布是四个不同状态，必须分别报告。

## 3. 分支与协作规则

- 所有功能和修复从 `origin/custom-release` 创建 feature 分支或独立 worktree。
- 不在 `custom-release` 上直接开发，不重写其历史，不 force-push。
- 本地测试和审查完成后，才把 feature 合并到 `custom-release` 并推送。
- `custom` 只用于 `upstream/main` 的前瞻兼容测试，不能作为生产来源。
- 稳定功能如需进入 `custom`，只能从 `custom-release` 选择性执行 `cherry-pick -x`。
- 禁止把整个 `custom` 合并回 `custom-release`。
- VPS 应急改动必须创建 `emergency/vps-YYYYMMDD`，commit 并推送，不能让未提交改动成为唯一副本。

推荐的本地起点：

```powershell
git -C E:\Code\sub2api fetch origin --prune --tags
git -C E:\Code\sub2api fetch upstream --prune --tags
git -C E:\Code\sub2api worktree add `
  E:\Code\sub2api-worktrees\feature-name `
  -b feature/feature-name origin/custom-release
```

开始前检查目标 worktree 的 `git status --short --branch`，并完整阅读仓库 `AGENTS.md` 和
`deploy/RELEASE-RUNBOOK.md`。

## 4. 自动化边界

当前发布不是完全无人值守自动发布：

| 阶段 | 是否自动 | 触发方式 |
|---|---|---|
| 测试和双镜像构建 | 自动 | push 到 `custom-release` 或 `integration/release-*` |
| 生产发布 | 需要管理员授权 | 管理后台左上角更新按钮 |
| 发布后的备份、部署、健康检查和镜像回退 | 自动 | 按钮创建持久发布任务后执行 |
| 官方稳定 Release 检查 | 仅在按钮任务中执行 | 不使用定时轮询 |
| 健康监控 | 自动且独立 | 保留现有 health-monitor 计划任务 |

因此，自定义代码推送后不会自动修改生产。最简人工流程是：

```text
feature 开发并测试
-> 合并、推送 origin/custom-release
-> 等待 Custom Release Actions 和双 GHCR 镜像
-> 管理员点击左上角更新
-> 等待持久任务 success
```

管理员可以在 Actions 尚未完成时点击更新；状态机会进入 `waiting_actions` / `waiting_images`
并继续等待。为了更快发现代码问题，日常操作仍建议先看 Actions 结果再点击。

## 5. 自定义代码发布标准流程

### 5.1 开发和本地验证

1. 从 `origin/custom-release` 创建 feature worktree。
2. 修改主应用或 `extensions-self`，同时添加与风险匹配的测试。
3. 运行相关本地测试和 `git diff --check`。
4. 审查完整 diff，确认没有凭据、生产 `.env` 或无关改动。
5. commit 到 feature 分支，再合并到 `custom-release`。

Custom Release workflow 会在 Linux runner 上执行完整门禁：

- 后端单元测试和集成测试。
- `golangci-lint`。
- 前端 typecheck、Vitest 和生产构建。
- `extensions-self/account-monitor` 和 `extensions-self/risk-control` 的 Go 测试。
- `deploy/tests/*.test.mjs`、PowerShell 合约、Release resolver、发布 fixture 和 Shell 语法。
- 主应用和扩展双镜像构建、推送。

### 5.2 确认 Actions 和镜像

可发布的 Actions 结果必须同时满足：

- workflow 名称是 `Custom Release`。
- event 是 `push`，branch 是 `custom-release`。
- head SHA 等于刚推送的完整 commit SHA。
- `backend`、`golangci`、`frontend`、`extensions`、`deployment`、`metadata`、`images`
  全部为 `success`。
- `images` 不能是 `skipped`。

同一 SHA 必须存在两张公共镜像：

```text
ghcr.io/listencodes/sub2api-custom:custom-<完整SHA>
ghcr.io/listencodes/sub2api-extensions:custom-<完整SHA>
```

生产状态机会再次自动验证匿名拉取、`linux/amd64`、digest 以及 OCI
`revision`、`version`、`source` 标签。因此人工查看 Actions 是提前发现错误的方法，不是唯一安全门禁。

### 5.3 管理员按钮和持久任务

管理员点击左上角更新按钮后，HTTP 请求立即返回 `job_id`。页面轮询持久任务，刷新页面不会丢失任务。
宿主机执行链路为：

```text
release-trigger
-> sub2api-release.path
-> sub2api-release.service
-> sync-and-publish.sh
```

按钮会自动处理三种场景：

1. 有新的官方稳定 Release：验证 annotated tag 和 Release commit，在
   `integration/release-*` 临时分支合并，仅在 Actions/镜像通过且基线未变化时推进
   `origin/custom-release`。
2. 官方无新版，但 `custom-release` 有未部署自定义 commit：等待该 commit 的 Actions/镜像并直接发布。
3. 官方和自定义代码都没有变化：返回 `success`，不拉取镜像，不重建容器。

常见状态顺序：

```text
checking_release -> validating_tag -> merging_release
-> waiting_actions -> waiting_images -> backing_up
-> deploying_extensions -> deploying_main -> health_checking
-> success
```

异常终态包括 `failed` 和 `conflict`；发生生产切换后健康失败时会先进入 `rolling_back`。

## 6. 生产发布和保护措施

发布器只接受经过批准的 `origin/custom-release` commit 和已验证的双 digest，执行顺序固定为：

1. 再次核对分支基线、官方稳定 Release 身份和镜像元数据。
2. 备份并验证主数据库和扩展数据库 dump。
3. 备份 Compose、`.env`、Nginx、源站证书/私钥、旧 digest、容器/镜像元数据和 rollback tag。
4. 完成备份后才 fast-forward VPS 的 `/root/sub2api`。
5. 匿名拉取两张目标 digest 镜像。
6. 先部署并检查 `extensions-self`。
7. 再部署 `sub2api`。
8. 执行完整内网、公网、原生管理页、扩展代理和数据质量检查。
9. 健康后写入 `release-state.json`。
10. 仅在健康发布后，按 `data-quality.available_from/to` 执行必要的分段回填。

禁止事项：

- 不在 VPS 构建正式生产镜像。
- 不直接调用 `publish-custom.sh` 作为最终验收入口。
- 不重建或替换 `risk-control-postgres`。
- 不在普通回滚中恢复数据库。
- 不使用 `docker compose down`、全量 volume prune 或直接编辑运行中容器。
- 不通过 cron、定时任务或后台轮询自动发布新代码。

## 7. 失败、冲突与回滚

### 发布前失败

Release 校验、合并冲突、Actions 失败、镜像缺失、分支基线变化、脏 worktree 或备份失败，都会在生产切换前停止。
生产保持不变。冲突任务会记录冲突文件、双方 commit、处理提示和诊断目录，不能使用通用
`ours` / `theirs` 覆盖。

### 发布后失败

如果扩展、主应用或完整健康检查失败，发布器会：

1. 记录 `rolling_back`。
2. 恢复上一组 `SUB2API_IMAGE` 和 `EXTENSIONS_SELF_IMAGE` digest 及匹配配置。
3. 先恢复 `extensions-self`，再恢复 `sub2api`。
4. 再次运行健康检查并记录回滚结果。

扩展事实表、游标和数据库默认保留。只有另行确认 schema 或数据损坏并获得授权后，才能从同一发布备份恢复数据库。

## 8. VPS 应急流程

所有服务器操作必须通过 `ssh-skill`，禁止直接使用 `ssh` / `scp`。

1. 确认当前 `release-state.json`、容器镜像、生产 commit 和 worktree 是否干净。
2. 确认没有正在运行的持久发布任务。
3. 从当前生产 commit 创建 `emergency/vps-YYYYMMDD`。
4. 做最小修复，运行测试，commit 并推送应急分支。
5. 将应急分支合入 `custom-release`，等待同一套 Actions 和双 GHCR 镜像。
6. 仍由管理员按钮触发 digest 发布，不绕过门禁。
7. 记录 commit、Actions、双 digest、备份、部署、健康和回滚资料。

不得直接修改运行中容器，也不能让 VPS 未提交代码成为唯一副本。

## 9. 发布后检查清单

- [ ] `release-state.json` 的 `production_commit` 等于批准 commit。
- [ ] `stable_release_tag` / `stable_release_commit` 与官方稳定 Release 一致。
- [ ] 主应用和扩展 digest 与 GHCR 验证结果一致。
- [ ] `sub2api`、`extensions-self`、PostgreSQL 和 Redis 全部 healthy。
- [ ] `risk-control-postgres` 的容器和 volume 身份未变化。
- [ ] 公网 `/health`、首页和扩展首页代理返回成功。
- [ ] `/admin/extensions/account-monitor` 和 `/admin/extensions/group-monitor` 可用。
- [ ] 签名 `data-quality` 没有未解释的源错误、缺失分组或异常采集延迟。
- [ ] `sub2api-release.path` enabled + active，发布 service 完成后 inactive。
- [ ] root crontab 不包含发布、自动更新或 trigger 消费者，仅保留独立健康监控。
- [ ] 备份目录包含双库 dump、校验和、配置、证书、镜像元数据和 rollback 证据。

## 10. 时间预期

| 场景 | 正常耗时 |
|---|---:|
| 按钮返回 `job_id` | 1-2 秒 |
| 没有任何变化 | 5-30 秒 |
| Actions 和双镜像已完成后的生产发布 | 1-3 分钟 |
| 自定义代码刚推送后立即点击 | 12-20 分钟 |
| 新官方稳定 Release 且无冲突 | 12-20 分钟 |
| Actions 排队或冷缓存 | 可能 20-30 分钟 |

Actions 等待默认每 75 秒轮询一次，最长等待 90 分钟。超时或失败不会绕过门禁发布。

## 11. 标准报告格式

每次改动和发布按以下项目分别报告，不能合并成一句“已完成”：

```text
实现 commit：
本地测试：
origin/custom-release 推送：
Custom Release Actions：
主镜像 digest：
扩展镜像 digest：
生产备份目录：
生产部署 commit：
容器健康：
公网健康：
触发器/定时任务状态：
回滚资料：
是否实际执行回滚：否（除非另行授权）
```

## 12. 相关文档

- `AGENTS.md`：仓库级开发、分支、发布和 Agent 规则。
- `deploy/RELEASE-RUNBOOK.md`：正式发布、应急流程、检查和回滚细节。
- `deploy/README.md`：Compose、部署配置和自定义生产发布入口。
- `deploy/ops/README.md`：宿主机脚本职责和状态机边界。
- `extensions-self/account-monitor/README.md`：账号监控服务和数据采集。
- `docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md`：账号监控数据口径。
- `docs/RISK-CONTROL-ADMIN-SPEC.md`：风控管理功能契约。
