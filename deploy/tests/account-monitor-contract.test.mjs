import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
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
    assert.match(compose, /EXTENSIONS_SELF_ACCOUNT_MONITOR_WEB_DIR=\/app\/account-monitor/)
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
    'EXTENSIONS_SELF_ACCOUNT_MONITOR_WEB_DIR=/app/account-monitor',
  ]) {
    assert.match(env, new RegExp(`^${setting.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`, 'm'))
  }
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

test('the extensions image contains both modules and the monitor web assets', () => {
  const dockerfile = read('extensions-self/Dockerfile')
  assert.match(dockerfile, /COPY account-monitor\/go\.mod account-monitor\/go\.sum/)
  assert.match(dockerfile, /COPY risk-control\/go\.mod risk-control\/go\.sum/)
  assert.match(dockerfile, /go build[^\n]*-o \/out\/extensions-self/)
  assert.match(dockerfile, /COPY account-monitor\/web\/ \/app\/account-monitor\//)
})

test('the publisher gates enabled monitoring on source privileges and readiness', () => {
  const publisher = read('deploy/ops/publish-custom.sh')
  const backupIndex = publisher.indexOf("log \"Backing up approved release")
  const installIndex = publisher.indexOf('install-account-monitor-source.sql')
  const buildIndex = publisher.indexOf("log 'Building main application image'")

  assert.ok(backupIndex >= 0 && installIndex > backupIndex && buildIndex > installIndex)
  assert.match(publisher, /ACCOUNT_MONITOR_ENABLED/)
  assert.match(publisher, /docker exec risk-control-postgres pg_dump/)
  assert.match(publisher, /risk_control_db\.dump/)
  assert.match(publisher, /extensions_self_monitor/)
  assert.match(publisher, /SET ROLE extensions_self_monitor_ro/)
  assert.match(publisher, /extensions_self_ro\.usage_source/)
  assert.match(publisher, /public\.api_keys/)
  assert.match(publisher, /public\.accounts/)
  assert.match(publisher, /SELECT credentials/)
  assert.match(publisher, /account-monitor\//)
  assert.match(publisher, /api\/v1\/admin\/account-monitor\/data-quality/)
  assert.match(publisher, /api\/v1\/extensions-self\/account-monitor\//)
  assert.doesNotMatch(publisher, /up -d[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /rm[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /down[^\n]*risk-control-postgres/)
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
    '/api/v1/extensions-self/account-monitor/',
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
    '/admin/account-monitor',
    'extensions_self_monitor',
    'ACCOUNT_MONITOR_ENABLED',
    'data-quality',
  ]) {
    assert.match(deployDocs, new RegExp(marker))
  }
  assert.match(deployDocs, /backup|备份/i)
  assert.match(deployDocs, /rollback|回滚/i)
})
