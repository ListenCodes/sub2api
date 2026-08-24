# Sub2API 自定义版项目与生产运维指南

双版本账本基线为 Official v0.1.163 / Custom v1.0.0，对应生产 commit
`aa2d24106cab0a03785330d8e0ff4e02b0474a0e`。首次成功的自定义运行时发布为
v1.0.1；仅官方更新不增加自定义版本，combined 只增加一次。docs-only、失败、
过期、漂移和未确认任务不占号。完整回退使用 prepare + 明确 apply 确认，1 小时
过期，只允许最近三个成功完整快照且排除当前版本；恢复历史双版本但高水位不下降、
不复用，普通回退不恢复任一数据库。生产账本迁移需要单独管理员授权，旧单阶段更新
和官方二进制回退入口必须 fail-closed。

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

生产 Compose 也分成两个职责明确的文件：`deploy/docker-compose.yml` 必须与当前记录的官方
Stable Release 原文件完全一致；`deploy/docker-compose.custom.yml` 承载双 digest 镜像、只读 trigger 挂载、
风控环境、`extensions-self`、`risk-control-postgres` 和 `risk_control_postgres_data`。生产不得依赖
隐式 `docker-compose.override.yml`，所有 Compose 命令都按 base 在前、custom 在后的顺序显式加载。

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

完成有限发现并进入已验证仓库后的推荐起点：

```powershell
$repo = (git rev-parse --show-toplevel).Trim()
$worktree = Join-Path (Split-Path $repo -Parent) 'sub2api-worktrees\feature-name'
git -C $repo fetch origin --prune --tags
git -C $repo fetch upstream --prune --tags
git -C $repo worktree add `
  $worktree `
  -b feature/feature-name origin/custom-release
```

开始前检查目标 worktree 的 `git status --short --branch`，并完整阅读仓库 `AGENTS.md` 和
`deploy/RELEASE-RUNBOOK.md`。

### 稳定扩展入口与冲突预算

自定义功能不能继续接入官方高频文件。日常开发使用以下稳定入口：

| 范围 | 稳定入口 | 禁止做法 |
|---|---|---|
| 后端启动和路由 | `SetupCustomRouter`、`RegisterCustomExtensionRoutes` | 在官方 `router.go` 或 gateway 路由组装中注册自定义逻辑 |
| 后端管理功能 | `CustomExtensions`、`CustomUserHandler` | 扩展官方 `UserHandler` 或 Wire provider 图 |
| 首页响应头 | `ExtensionsHomepageFrameHeaders`，只挂到对应 GET/HEAD 路由 | 为扩展修改全局安全中间件 |
| 前端路由 | `installExtensionRoutes` 和 `features/extensions/routes.ts` | 向中央 `router/index.ts` 导入或展开扩展路由 |
| 前端侧栏 | `features/extensions/navigation.ts` 的 provider | 在 `AppSidebar.vue` 写扩展路径或业务状态机 |
| 本地 Compose | `docker-compose.custom.local.yml`、`bootstrap-custom-local.sh` | 向官方 `docker-compose.local.yml` 添加自定义服务 |

根 `AGENTS.md` 列出 10 个必须与当前 Stable 基线字节一致的零重叠文件，以及认证生命周期、
三条旧更新/回退路由和“不输出秘密值”补丁的剩余预算。`deploy/tests/custom-overlap-budget.test.mjs`
在 CI 中执行这些约束。测试失败时应把行为迁移到上表的加法式入口，不能通过放宽预算、删除断言
或对冲突文件整份选择 `ours`/`theirs` 来绕过。完整背景和验收证据见
`docs/superpowers/plans/2026-07-29-stable-custom-extension-seams.md`。

## 4. 自动化边界

当前发布不是完全无人值守自动发布：

| 阶段 | 是否自动 | 触发方式 |
|---|---|---|
| 测试和双镜像构建 | 自动 | push 到 `custom-release` 或 `integration/release-*` |
| 官方 Stable 兼容性预检 | 自动且只读 | 每日定时或手动触发；仅生成不推送的临时候选并运行测试 |
| 生产发布 | 需要管理员授权 | 管理后台左上角更新按钮 |
| 发布后的备份、部署、健康检查和镜像回退 | 自动 | 按钮创建持久发布任务后执行 |
| 生产更新检查与候选推进 | 仅在按钮任务中执行 | 不使用定时发布或自动推进 |
| 健康监控 | 自动且独立 | 保留现有 health-monitor 计划任务 |

`Upstream Stable Preflight` 使用只读仓库权限，在 GitHub 的临时 checkout
中验证最新 annotated Release、规范 merge、baseline、后端、前端、扩展和发布契约；
不推分支、不发布镜像、不运行 Compose，也不修改生产。由于 GitHub 只从默认 `main`
加载定时工作流，该 workflow YAML 单独逐字节镜像到 `main`，任务实际 checkout 和测试的
仍是 `custom-release`，并在开始时校验两份 YAML 没有漂移。

因此，自定义代码推送后不会自动修改生产。最简人工流程是：

```text
feature 开发并测试
-> 合并、推送 origin/custom-release
-> 等待 Custom Release Actions 和双 GHCR 镜像
-> 管理员点击左上角更新
-> 等待持久任务 success
```

### 文档专用提交例外

只修改 Markdown、`AGENTS.md` 或任意层级 `.gitignore` 的 push 会被
`Custom Release` workflow 的 `paths-ignore` 排除，因此不会启动测试/镜像构建，
也不会推送 GHCR 镜像。管理员按钮触发的持久任务仍会比较生产 commit 与目标 commit
之间的完整差异；如果确认目标只包含这些文档路径，任务直接以 `success` 结束，记录
`docs_only=true`、`published=false`、`production_changed=false`，不等待 Actions、不验证
GHCR、不调用发布器，也不修改 `release-state.json`。

只要一次 push 同时包含源码、Workflow、Dockerfile、Compose、数据库迁移或
`deploy/ops/` 脚本等运行时路径，就按普通运行时提交处理。文档提交之后再合入运行时代码时，
分类器比较生产 commit 到目标 commit 的完整差异，仍会恢复完整 Actions、双镜像和管理员发布门禁。

管理员可以在 Actions 尚未完成时点击更新；状态机会进入 `waiting_actions` / `waiting_images`
并继续等待。为了更快发现代码问题，日常操作仍建议先看 Actions 结果再点击。

## 5. 自定义代码发布标准流程

### 5.1 开发和本地验证

1. 从 `origin/custom-release` 创建 feature worktree。
2. 修改主应用或 `extensions-self`，同时添加与风险匹配的测试。
3. 运行相关本地测试和 `git diff --check`。
4. 审查完整 diff，确认没有凭据、生产 `.env` 或无关改动。
5. commit 到 feature 分支，再合并到 `custom-release`。

运行时提交的 Custom Release workflow 会在 Linux runner 上执行完整门禁：

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
   `integration/release-*` 临时分支合并。合并标题必须精确为
   `merge: integrate stable Release vX.Y.Z`，第一父提交是已批准的
   `origin/custom-release` 基线，第二父提交是 annotated tag 解引用后的官方 commit；
   `stable-release-baseline.json` 必须指向同一 tag/commit。只有 Actions/镜像通过且
   promotion 再次验证这组身份后，才推进 `origin/custom-release`。
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
3. 备份官方 Compose、自定义 Compose、`.env`、Nginx、源站证书/私钥、旧 digest、容器/镜像元数据和 rollback tag。
4. 完成备份后才 fast-forward VPS 的 `/root/sub2api`。
5. 匿名拉取两张目标 digest 镜像。
6. 先部署并检查 `extensions-self`。
7. 再部署 `sub2api`。
8. 执行完整内网、公网、原生管理页、扩展代理和数据质量检查。
9. 健康后写入 `release-state.json`。
10. 仅在健康发布后，按 `data-quality.available_from/to` 执行必要的分段回填。

生产 Compose 验证命令固定为：

```bash
docker compose --project-name deploy \
  -f /root/sub2api/deploy/docker-compose.yml \
  -f /root/sub2api/deploy/docker-compose.custom.yml \
  --env-file /root/sub2api/deploy/.env config --quiet
```

需要核对有效服务、镜像、挂载、环境、网络和 volume 时，在相同参数后使用
`config --format json`。Compose 的 map 覆盖和 list 追加以最终渲染结果为准，不能只检查某一源文件。
最终渲染的 `sub2api` 只能包含命名 `/app/data` volume 和
`/opt/sub2api-custom/sync-trigger.sh` -> `/app/scripts/sync-upstream.sh:ro`；禁止
`/root/sub2api:/repo`、Docker socket 和 `/usr/bin/docker`。Git、双镜像、OCI、Compose 和
备份验证全部由宿主机脚本执行，Web 容器不承担这些职责。
备份文件固定为同一目录下的 `main-docker-compose.yml` 与 `custom-docker-compose.yml`；回滚及其健康
检查只能加载这对备份，禁止把备份 base 与当前 custom（或反向组合）混用。

首次从尚无自定义 overlay 的干净生产 commit 迁移时，发布器可为“当前旧配置”生成临时空 overlay，
并把旧 base + 空 overlay 成对备份；无论是否已有 `release-state.json` 都适用。批准的目标 commit
仍必须包含正式 `docker-compose.custom.yml`，否则在备份和生产变更前停止。

禁止事项：

- 不在 VPS 构建正式生产镜像。
- 不直接调用 `publish-custom.sh` 作为最终验收入口。
- 不重建或替换 `risk-control-postgres`。
- 不在普通回滚中恢复数据库。
- 不使用 `docker compose down`、全量 volume prune 或直接编辑运行中容器。
- 不通过 cron、定时任务或后台轮询自动发布新代码。

### 6.1 宿主机脚本同步与发布资产清理

宿主机维护不是发布 apply 的一部分，也不能配置成 cron。只有获得明确授权后才能执行；开始前必须
通过 `/var/lock/sub2api-release.lock` 取得发布锁，确认 ledger 没有活动或待确认任务、
`sub2api-release.service` 已结束、`/root/sub2api` 干净且等于 ledger 当前 commit。

`/opt/sub2api-custom/` 是独立的版本化发布产物，Git 工作树更新不会自动同步它。涉及
`deploy/ops/` 的发布成功后，应先把已安装脚本和两个 systemd unit 备份到
`release-host-backups/` 并生成 `SHA256SUMS`，再从已部署 commit 重新安装、reload systemd、
执行 `bash -n`，逐文件比较源码与安装副本。宿主机专用 `health-monitor.sh` 必须保留。
更新准备会在解析目标前，以账本记录的当前生产 commit 校验完整安装集；`HOST_OPS_DRIFT`
要求先从该生产 commit 重装，不能预装尚未发布的目标脚本，也不能只覆盖报错文件。

移除 Web 高权限挂载必须分两次生产发布。Stage A 只部署兼容旧五挂载和最终两挂载的
transition validator，overlay 仍保持旧形态；Stage A 成功并把同 commit 的宿主脚本同步、逐文件
校验后，Stage B 才能进入 `origin/custom-release`，并同时落地最终 overlay 和只接受两挂载的
严格 validator。Stage A 未完成前不得让 Stage B 成为生产分支目标。

2026-07-30 已完成 Stage A 门禁：生产 ledger、Git 和双 OCI revision 均为
`7aadd0b682d67a4124d08a006bbb054d0cc8c37d`，版本为 `v0.1.168 / v1.0.12`，现网仍保留
旧五挂载；`/opt/sub2api-custom/release-common.sh` 已与 Stage A 源码逐字节一致，旧宿主文件及
systemd units 的校验备份位于
`release-host-backups/20260730T090022Z-stage-a-7aadd0b68/`。因此 Stage B 可以推进
`origin/custom-release`，但没有单独生产授权时必须继续停留在“代码已推送、生产未部署”。

2026-07-30 随后由管理员完成 Stage B 两阶段确认。生产 release
`release-candidate-20260730T100420651606255Z-5152cebe8` 已运行
`5152cebe82e505adf20a3f75d32276b9ae9c5a74`（`v0.1.168 / v1.0.13`），主应用和扩展
OCI revision 均为该 commit。运行中 `sub2api` 只剩数据卷和只读 trigger 两个挂载，源码、
Docker socket 和 Docker binary 挂载均不存在。最终 21 个宿主脚本和两个 systemd unit 已从该
commit 重新安装并逐文件比较；严格 validator 已实测接受两挂载并拒绝旧五挂载，宿主专用
`health-monitor.sh` 保持不变。安装前校验备份位于
`release-host-backups/20260730T102758Z-stage-b-5152cebe8/`，其中 `SHA256SUMS` 已通过。
完成后 ledger 无活动任务，release service 为 inactive、path 为 enabled + active，发布锁可取得，
五个 Compose 服务及内外 `/health` 均正常；本次未执行回退。

清理统一采用以下口径：

- `release-ledger/` 和兼容期 `release-jobs/` 保留完整审计历史。
- `release-backups/` 只保留最新三个成功 release record 引用的完整备份；删除旧目录前必须先验证
  三份保留备份的 `SHA256SUMS`。
- Docker 必须保护所有运行中容器镜像、当前主/扩展 digest，以及三份保留备份中
  `release-state.json` 记录的三组历史 digest 和回滚 tag；禁止 `docker image prune -a`、
  `docker system prune` 和任何 volume prune，也不得清理其他应用镜像。
- 没有活动任务时，`release-prepared/` 保留与三份成功 release record 对应的目录，
  `sync-conflicts/` 和 `release-host-backups/` 各保留最新三份；活动或仍可确认的 manifest 不得删除。
- `/root/backups/sub2api/` 默认保留最新三份，但显式固定、唯一迁移或应急资料必须额外保留；只有确认
  release backup 已覆盖当前回滚需求后才能删除其余目录。
- 旧 `.prepared-update-*`、`sync-job-id`、`sync-status`、`sync-result` 只有在新 ledger 已生效、
  当前代码不再读取且没有活动任务时才可删除。

递归删除前必须用 `realpath` 校验目标是预期根目录的直接子项，并先把确切目标写入 UTC 维护日志。
清理后重新验证保留备份校验和、当前及三层回滚镜像、双 Compose `config --quiet`、全部关键容器、
公网健康/扩展首页和 systemd path/service。报告必须列出日志路径、保留/删除数量、清理前后空间和
本地可恢复性。详细权威流程以 `deploy/RELEASE-RUNBOOK.md` 的
“Host Artifact Retention And Cleanup”为准。

## 7. 失败、冲突与回滚

### 发布前失败

Release 校验、合并冲突、Actions 失败、镜像缺失、分支基线变化、脏 worktree 或备份失败，都会在生产切换前停止。
生产保持不变。冲突任务会记录冲突文件、双方 commit、处理提示和诊断目录，不能使用通用
`ours` / `theirs` 覆盖。

如果某次规范 Stable 合入随后被紧邻提交完整回退，后续准备任务只有在逐项验证原合入标题、双父身份、
单父回退关系以及回退树与合入前树完全一致后，才允许在临时工作树重新应用该回退的逆补丁，并生成新的
规范双父合入提交。后续准备只把这次新合入视为当前有效版本，并拒绝重复再激活或回退之后出现的非规范
合入；历史不唯一、树不一致或出现冲突时一律在推进分支和生产变更前失败。

### 发布后失败

如果扩展、主应用或完整健康检查失败，发布器会：

1. 记录 `rolling_back`。
2. 恢复上一组 `SUB2API_IMAGE` 和 `EXTENSIONS_SELF_IMAGE` digest，以及同一备份目录中的匹配 Compose 对和 `.env`。
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
- [ ] 备份目录包含双库 dump、两份匹配 Compose、`.env`、校验和、证书、镜像元数据和 rollback 证据。
- [ ] 涉及 `deploy/ops/` 的发布完成后，安装副本与当前部署源码逐文件一致，宿主机专用监控仍保留。
- [ ] 维护清理保留完整 ledger、三层完整回滚备份及其镜像，并记录 UTC 日志和清理前后空间。

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
宿主机脚本同步：
维护日志与保留数量：
清理前后空间：
删除资料的本地可恢复性：
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
- `docs/superpowers/plans/2026-07-29-stable-custom-extension-seams.md`：稳定扩展入口、热点审计和剩余接线预算。

## 13. 两阶段更新与检测

左上角检测同时读取官方最新非 draft、非 prerelease Stable Release，以及
`release-state.json.production_commit` 和 `origin/custom-release` HEAD。更新分类
是 `none`、`official`、`custom`、`combined` 或 `docs-only`；自定义更新始终展示目标
commit 短 SHA，不使用 semver。检测是只读的，不修改生产、不拉镜像、不创建 trigger。
任何运行时代码差异都进入运行时门禁；纯 Markdown、`AGENTS.md` 和 `.gitignore`
可以提示但不会创建生产切换任务。

提示分为两类。只有 `docs-only` 使用 `notice_unread` 控制左上角橙色背景、圆点和动画；管理员
打开下拉后可以按 fingerprint 标记已读，但检测内容仍保留。服务端 fingerprint 固定包含
update kind、目标 Official version/commit 和目标 Custom commit；最后已读 fingerprint 按
管理员 `user_id` 原子写入 `/app/data/custom-release-notice-state.json`，其他管理员互不影响。
状态文件异常只能返回 advisory warning，不得阻断检测。

`official`、`custom` 和 `combined` 等真实运行更新的橙色提醒直接由 `runtime_update` 与
`has_update` 派生，打开/关闭下拉、刷新、重新登录、跨设备或 prepare/apply 都不能标记已读，
也不提供忽略入口。提醒只在成功部署后的重新检测返回 `has_update=false` 时消失；新目标按新的
目标身份替代旧目标。

管理员按钮采用明确的 prepare/apply 两步：

1. `prepare` 持久化锁定 production、Stable 和 custom target，等待七个 Actions job，
   验证两张 GHCR 镜像的 OCI 身份和 immutable digest，按 digest 预拉取，渲染显式
   `docker-compose.yml` + `docker-compose.custom.yml`，备份双库、匹配 Compose、`.env`、
   Nginx/证书、旧 digest、容器/镜像元数据和 rollback tag，并校验 SHA256 与
   `pg_restore --list`。此阶段不切换 Git、不开关容器、不写 `release-state.json`，也不
   触碰 `risk-control-postgres`。
2. `prepared` manifest 有效 1 小时，至少包含 production/target commit、Stable 身份、
   双 digest、Compose/.env 哈希、backup_dir、`prepared_at`、`expires_at`。过期后重新
   备份和复核即可，已经验证的镜像证据可以复用。
3. 只有管理员点击“确认更新”才进入 `apply`。apply 只复核 manifest 和环境漂移，禁止
   GitHub、Actions、pull、数据库备份和网络发布；使用本地镜像和 `--pull never`，先
   `extensions-self` 再 `sub2api`，完成内网、公网、原生管理页、扩展路由和
   `data-quality` 健康检查后才原子写 `release-state.json`。失败自动使用准备阶段的旧
   digest 和匹配 Compose 对回滚。

刷新、重新登录或浏览器断开时，页面先完成当前目标检测，再同时查询 localStorage 中的精确
job ID 和服务端最新 durable job。localStorage 只作同设备加速；跨设备以服务端
`release-current-job-id` 和 ledger operation 为准。两者不同则优先较新的 `updated_at`，
时间缺失或不明确时优先服务端 current job。

匹配当前 Official tag/commit 和 Custom target commit 的 `failed`、`conflict`、`drifted`、
`failed_rolled_back`、`rollback_failed` 会持续恢复失败卡和橙色提醒；关闭/重开下拉不会清除。
失败卡显示持久化的 `failed_check`、`conclusion`、`error_code`、`check_url`/`workflow_url` 和
`production_changed=false`。只有新目标替代、重新准备后成功、或部署成功使 `has_update=false`
才清除；重试会创建新 prepare job 并替换本地 job ID。`expired` 回到可重新准备状态，旧
单阶段 job 继续 fail-closed。`/admin/system/update` 仅保留为 prepare 兼容别名，不得直接
调用 `publish-custom.sh`。

完整回退与更新共用官方视觉语言，但逻辑仍由自定义组件拥有。页面先显示 loading；current
release 或历史列表失败时显示错误和 retry；无完整历史时显示 empty。候选仅从 ledger 读取，
排除 current 后按新到旧最多三条，每条显示 Official、Custom、短 commit 和时间；Web API
不得调用 Git 或 Docker 做筛选。管理员选择后执行“准备回退”，prepared 后锁定目标并显示一小时
倒计时，只有“确认回退”才 apply；过期或失败后恢复为可重试状态。宿主 prepare 对目标 Git、
双镜像、OCI revision、Compose、备份及其校验和任一失败都 fail-closed，生产保持不变。

## 14. Docker 新站与完整迁移

自建版新站只支持 Linux amd64 空目标：`custom-release` 必须干净且 HEAD 精确等于
`origin/custom-release`，目标容器、命名卷、发布 systemd unit 和生产 `.env` 都不能已存在。
把密钥配置放在源码目录外并设为 `0600`，先运行无写入的检查，再正式部署：

```bash
deploy/ops/bootstrap-custom-site.sh fresh \
  --env-file /root/sub2api-site.env --confirm FRESH-EMPTY-SITE --check-only
deploy/ops/bootstrap-custom-site.sh fresh \
  --env-file /root/sub2api-site.env --confirm FRESH-EMPTY-SITE
```

`fresh` 自动校验双 GHCR digest 和显式双 Compose，按依赖顺序启动完整服务，并登记当前
Official Stable / Custom v1.0.0 / 全局高水位 0。以后更新仍使用左上角“准备更新”并在
1 小时内明确确认；回退同样是 prepare + confirm。

迁移现有站点时，先在健康源站导出完整配对资料：

```bash
deploy/ops/export-custom-site.sh \
  --output /root/sub2api-site-export --confirm EXPORT-SITE
```

安全传输整个目录后，在空目标上检出与导出资料一致的 `origin/custom-release`，先检查再恢复：

```bash
deploy/ops/bootstrap-custom-site.sh migrate \
  --bundle /root/sub2api-site-export --confirm RESTORE-MIGRATION --check-only
deploy/ops/bootstrap-custom-site.sh migrate \
  --bundle /root/sub2api-site-export --confirm RESTORE-MIGRATION
```

导出包包含双库新 dump、`.env`、Compose 对、Nginx/证书、完整双版本账本、发布备份、
容器/镜像元数据和 `SHA256SUMS`。`migrate` 会在应用写入进程启动前恢复双库，并保留原站
双版本显示、历史快照和全局高水位。两个模式都不修改 DNS/CDN；域名解析和公网 TLS
切换仍由站点外部运维单独完成。
