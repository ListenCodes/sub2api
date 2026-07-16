import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const deployRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(deployRoot, '..')

function read(relativePath) {
  return readFileSync(resolve(repoRoot, relativePath), 'utf8')
}

test('compose wires the account monitor into the existing extensions-self service', () => {
  for (const composeFile of ['deploy/docker-compose.yml', 'deploy/docker-compose.local.yml']) {
    const compose = read(composeFile)
    assert.equal((compose.match(/^  extensions-self:\s*$/gm) ?? []).length, 1)
    assert.doesNotMatch(compose, /^  account-monitor:\s*$/m)
    assert.match(compose, /ACCOUNT_MONITOR_ENABLED=\$\{ACCOUNT_MONITOR_ENABLED:-false\}/)
    assert.match(compose, /ACCOUNT_MONITOR_SOURCE_DATABASE_URL=\$\{ACCOUNT_MONITOR_SOURCE_DATABASE_URL:-\}/)
    assert.match(compose, /ACCOUNT_MONITOR_POLL_SECONDS=\$\{ACCOUNT_MONITOR_POLL_SECONDS:-60\}/)
    assert.match(compose, /ACCOUNT_MONITOR_LOOKBACK_SECONDS=\$\{ACCOUNT_MONITOR_LOOKBACK_SECONDS:-300\}/)
    assert.match(compose, /ACCOUNT_MONITOR_BATCH_SIZE=\$\{ACCOUNT_MONITOR_BATCH_SIZE:-1000\}/)
    assert.match(compose, /ACCOUNT_MONITOR_QUERY_TIMEOUT_MS=\$\{ACCOUNT_MONITOR_QUERY_TIMEOUT_MS:-3000\}/)
    assert.doesNotMatch(compose, /EXTENSIONS_SELF_ACCOUNT_MONITOR_WEB_DIR/)
    assert.match(compose, /extensions-self:[\s\S]*depends_on:[\s\S]*postgres:[\s\S]*condition: service_healthy/)
    assert.match(compose, /^  risk-control-postgres:\s*$/m)
  }
})

test('the example environment is disabled by default and documents every monitor setting', () => {
  const env = read('deploy/.env.example')
  for (const setting of [
    'ACCOUNT_MONITOR_ENABLED=false',
    'ACCOUNT_MONITOR_SOURCE_DATABASE_URL=',
    'ACCOUNT_MONITOR_POLL_SECONDS=60',
    'ACCOUNT_MONITOR_LOOKBACK_SECONDS=300',
    'ACCOUNT_MONITOR_BATCH_SIZE=1000',
    'ACCOUNT_MONITOR_QUERY_TIMEOUT_MS=3000',
  ]) {
    assert.match(env, new RegExp(`^${setting.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`, 'm'))
  }
  assert.doesNotMatch(env, /EXTENSIONS_SELF_ACCOUNT_MONITOR_WEB_DIR/)
  assert.match(env, /extensions_self_monitor/)
  assert.match(env, /dedicated read-only login|专用只读登录角色/)
})

test('the source installer keeps group privileges separate from the login role', () => {
  const sourceViews = read('extensions-self/account-monitor/sql/main_source_views.sql')
  const installer = read('deploy/ops/install-account-monitor-source.sql')

  assert.match(sourceViews, /CREATE ROLE extensions_self_monitor_ro NOLOGIN/)
  assert.match(installer, /CREATE ROLE extensions_self_monitor LOGIN/)
  assert.match(installer, /ALTER ROLE extensions_self_monitor[\s\S]*NOSUPERUSER[\s\S]*NOCREATEDB[\s\S]*NOCREATEROLE[\s\S]*NOBYPASSRLS/)
  assert.match(installer, /GRANT extensions_self_monitor_ro TO extensions_self_monitor/)
  assert.doesNotMatch(installer, /GRANT[^\n]*(sub2api|risk_control_app)/)
})

test('the extensions image contains both modules without monitor web assets', () => {
  const dockerfile = read('extensions-self/Dockerfile')
  assert.match(dockerfile, /COPY account-monitor\/go\.mod account-monitor\/go\.sum/)
  assert.match(dockerfile, /COPY risk-control\/go\.mod risk-control\/go\.sum/)
  assert.match(dockerfile, /go build[^\n]*-o \/out\/extensions-self/)
  assert.doesNotMatch(dockerfile, /account-monitor\/web|EXTENSIONS_SELF_ACCOUNT_MONITOR_WEB_DIR/)
})

test('the publisher gates enabled monitoring on source privileges and readiness', () => {
  const publisher = read('deploy/ops/publish-custom.sh')
  const backupIndex = publisher.indexOf('job_update backing_up')
  const installIndex = publisher.indexOf('install-account-monitor-source.sql')
  const pullIndex = publisher.indexOf('docker pull "$TARGET_EXTENSIONS_REF"')

  assert.ok(backupIndex >= 0 && installIndex > backupIndex && pullIndex > installIndex)
  assert.match(publisher, /ACCOUNT_MONITOR_ENABLED/)
  assert.match(publisher, /docker exec risk-control-postgres pg_dump/)
  assert.match(publisher, /risk_control_db\.dump/)
  assert.match(publisher, /pg_restore[^\n]*--list/)
  assert.match(publisher, /ssl_certificate/)
  assert.match(publisher, /certificate-metadata|certificates/)
  assert.match(publisher, /MAIN_ROLLBACK_TAG/)
  assert.match(publisher, /EXTENSIONS_ROLLBACK_TAG/)
  assert.match(publisher, /backfill.*pending|BACKFILL_STATUS.*pending/i)
	assert.match(publisher, /extensions_self_monitor/)
	assert.match(publisher, /SET ROLE extensions_self_monitor_ro/)
	assert.match(publisher, /extensions_self_ro\.usage_source/)
	assert.match(publisher, /extensions_self_ro\.group_dimension/)
	assert.match(publisher, /extensions_self_ro\.account_group_dimension/)
  assert.match(publisher, /public\.api_keys/)
  assert.match(publisher, /public\.accounts/)
  assert.match(publisher, /SELECT credentials/)
  assert.match(publisher, /api\/v1\/admin\/account-monitor\/data-quality/)
  assert.doesNotMatch(publisher, /account monitor static health check|api\/v1\/extensions-self\/account-monitor\//)
  assert.doesNotMatch(publisher, /up -d[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /rm[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /down[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /docker\s+build|compose[^\n]*build/)
})

test('the segmented backfill command bounds, polls, stops, and records every job', () => {
  const relative = 'deploy/ops/backfill-account-monitor.sh'
  assert.equal(existsSync(resolve(repoRoot, relative)), true)
  const script = read(relative)
  assert.match(script, /31\s*\*\s*24|31 days|31-day/i)
  assert.match(script, /rebuild-jobs/)
  assert.match(script, /pending|running/)
  assert.match(script, /completed/)
  assert.match(script, /failed/)
  assert.match(script, /processed_rows/)
  assert.match(script, /data-quality/)
  assert.match(script, /backfill-jobs|BACKFILL_RANGE|backfill_range/i)
  assert.doesNotMatch(script, /continue[^\n]*failed/i)
})

test('account monitor documentation covers ownership, formulas, operations, and handoff', () => {
  const requiredDocs = [
    'extensions-self/README.md',
    'extensions-self/account-monitor/README.md',
    'docs/EXTENSIONS-SELF-ARCHITECTURE.md',
    'docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md',
    'docs/ACCOUNT-MONITOR-CHECKLIST.md',
  ]
  for (const doc of requiredDocs) {
    assert.match(read(doc), /account monitor|账号监控/i, `${doc} does not identify the account monitor`)
  }

  const moduleReadme = read('extensions-self/account-monitor/README.md')
  for (const marker of [
    '账号尝试成功率 = 成功账号尝试 / 总账号尝试',
    '用户最终成功率 = 最终成功请求 / 用户请求总数',
    'model_attribution=exact',
    'model_attribution=estimated',
    '重试后成功',
    'extensions_self_ro',
    'risk-control-postgres',
    '/api/v1/admin/account-monitor',
    '/admin/extensions/account-monitor',
    'ACCOUNT_MONITOR_SOURCE_DATABASE_URL',
  ]) {
    assert.match(moduleReadme, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))
  }

  const dataDictionary = read('docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md')
  for (const table of [
    'account_monitor_attempt_facts',
    'account_monitor_request_facts',
    'account_monitor_sync_state',
    'account_monitor_rebuild_jobs',
    'account_monitor_thresholds',
  ]) {
    assert.match(dataDictionary, new RegExp(table))
  }
  assert.match(dataDictionary, /90 天/)
  assert.match(dataDictionary, /365 天|1 年/)
  assert.match(dataDictionary, /不完整|缺口/)

  const checklist = read('docs/ACCOUNT-MONITOR-CHECKLIST.md')
  for (const marker of [
    '31 天',
    'install-account-monitor-source.sql',
    'SET ROLE extensions_self_monitor_ro',
    'ACCOUNT_MONITOR_ENABLED=false',
    'risk-control-postgres',
    '代码完成不等于生产发布',
  ]) {
    assert.match(checklist, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))
  }

  const deployDocs = [
    read('AGENTS.md'),
    read('deploy/README.md'),
    read('deploy/ops/README.md'),
    read('deploy/RELEASE-RUNBOOK.md'),
  ].join('\n')
  for (const marker of [
    '/admin/extensions/account-monitor',
    'extensions_self_monitor',
    'ACCOUNT_MONITOR_ENABLED',
    'data-quality',
  ]) {
    assert.match(deployDocs, new RegExp(marker))
  }
  assert.match(deployDocs, /backup|备份/i)
  assert.match(deployDocs, /rollback|回滚/i)
})

test('monitor correction documentation defines inventory, grouping, range, and refresh checks', () => {
  const checklist = read('docs/ACCOUNT-MONITOR-CHECKLIST.md')
  const runbook = read('deploy/RELEASE-RUNBOOK.md')
  const dictionary = read('docs/ACCOUNT-MONITOR-DATA-DICTIONARY.md')

  for (const document of [checklist, runbook]) {
    for (const marker of [
      'extensions_self_ro.account_group_dimension',
      '全量非删除账号数',
      '事实活跃账号数',
      '多分组账号样本',
      'page_size=1000',
      '7d/30d',
      '手动刷新',
    ]) {
      assert.match(document, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))
    }
  }

  assert.match(dictionary, /extensions_self_ro\.account_group_dimension/)
  assert.doesNotMatch(`${checklist}\n${runbook}`, /账号详情.{0,20}(?:六|6).{0,5}页签/)
  assert.doesNotMatch(`${checklist}\n${runbook}`, /(?:周期|定时|自动).{0,8}刷新/)
})

test('monitor documentation defines the fixed twenty-four-bucket release contract', () => {
  const documents = `${read('docs/ACCOUNT-MONITOR-CHECKLIST.md')}\n${read('deploy/RELEASE-RUNBOOK.md')}`
  for (const marker of [
    '6h/24h/7d/30d',
    '24 个时间桶',
    '15 分钟',
    '1 小时',
    '7 小时',
    '30 小时',
    'account_monitor_request_facts',
  ]) {
    assert.match(documents, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))
  }
  assert.doesNotMatch(documents, /7d\/30d[^\n]*account_monitor_group_model_10m/)
})
