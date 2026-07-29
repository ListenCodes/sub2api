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

test('custom extensions live under the extensions-self namespace', () => {
  assert.equal(existsSync(resolve(repoRoot, 'risk-control')), false)
  assert.equal(existsSync(resolve(repoRoot, 'extensions-self/risk-control/go.mod')), true)
  assert.equal(existsSync(resolve(repoRoot, 'extensions-self/account-monitor/go.mod')), true)
  assert.equal(existsSync(resolve(repoRoot, 'extensions-self/account-monitor/web/index.html')), false)
  assert.equal(existsSync(resolve(repoRoot, 'extensions-self/homepage/index.html')), true)
  assert.equal(existsSync(resolve(repoRoot, 'extensions-self/homepage/app.js')), true)
  assert.equal(existsSync(resolve(repoRoot, 'extensions-self/Dockerfile')), true)
})

test('homepage navigation escapes the iframe and returns the brand to home', () => {
  const homepage = read('extensions-self/homepage/index.html')
  for (const href of ['/home', '/admin/dashboard']) {
    const escapedHref = href.replace('/', '\\/')
    assert.match(homepage, new RegExp(`<a(?=[^>]*href="${escapedHref}")(?=[^>]*target="_top")[^>]*>`))
  }
})

test('homepage uses configured branding and live public group data', () => {
  const homepage = read('extensions-self/homepage/index.html')
  const app = read('extensions-self/homepage/app.js')
  assert.doesNotMatch(homepage, /fonts\.googleapis\.com|\/logo\.png/)
  assert.match(homepage, /<script src="app\.js" defer><\/script>/)
  assert.doesNotMatch(homepage, /<script(?:\s[^>]*)?>\s*\(\(\)\s*=>/)
  assert.match(app, /fetch\(['"]\/api\/v1\/settings\/public['"]/)
  assert.match(app, /fetch\(['"]api\/public-groups['"]/)
  assert.match(app, /settingsPayload\.data/)
  assert.match(app, /setText\(['"]hero-lead['"]/)
  assert.match(app, /cache:\s*['"]no-store['"]/)
  assert.match(app, /data:image\//)
  assert.match(app, /location\.origin/)
  assert.match(app, /textContent/)
  assert.match(app, /createElement/)
  assert.doesNotMatch(`${homepage}\n${app}`, /innerHTML/)
  assert.doesNotMatch(`${homepage}\n${app}`, /Claude 特价|GPT PLUS 特价|0\.001x/)
})

test('homepage exposes stable loading, empty, and unavailable group states', () => {
  const homepage = read('extensions-self/homepage/index.html')
  const app = read('extensions-self/homepage/app.js')
  for (const state of ['正在读取实时倍率', '暂无公开分组', '倍率暂时不可用']) {
    assert.match(`${homepage}\n${app}`, new RegExp(state))
  }
  for (const id of ['site-logo', 'site-name', 'site-subtitle', 'hero-site-name', 'hero-lead', 'rate-status', 'rate-groups']) {
    assert.match(homepage, new RegExp(`id="${id}"`))
  }
})

test('homepage clips decorative overflow at the document boundary', () => {
  const homepage = read('extensions-self/homepage/index.html')
  assert.match(homepage, /html,body\{[^}]*overflow-x:hidden/)
})

test('compose runs one extensions-self application container and preserves the risk database', () => {
  for (const composeFile of ['deploy/docker-compose.custom.yml', 'deploy/docker-compose.custom.local.yml']) {
    const compose = read(composeFile)
    assert.match(compose, /^  extensions-self:\s*$/m)
    assert.match(compose, /container_name: extensions-self/)
    if (composeFile.endsWith('.local.yml')) {
      assert.match(compose, /image: deploy-extensions-self/)
      assert.match(compose, /context: \.\.\/extensions-self/)
    } else {
      assert.match(
        compose,
        /image: \$\{EXTENSIONS_SELF_IMAGE:\?EXTENSIONS_SELF_IMAGE is required\}/
      )
      assert.doesNotMatch(compose, /context: \.\.\/extensions-self/)
    }
    assert.match(compose, /RISK_CONTROL_URL(?:=|:)\s*\$\{RISK_CONTROL_URL:-http:\/\/extensions-self:8090\}/)
    assert.match(compose, /^  risk-control-postgres:\s*$/m)
    assert.doesNotMatch(compose, /^  risk-control:\s*$/m)
  }
})

test('the apply phase targets extensions-self without managing the risk database lifecycle', () => {
  const publisher = read('deploy/ops/apply-release.sh')
  assert.match(publisher, /HOMEPAGE_GROUPS_HEALTH_URL=.*homepage\/api\/public-groups/)
  assert.match(publisher, /curl[^\n]*HOMEPAGE_GROUPS_HEALTH_URL/)
  assert.match(publisher, /--pull never/)
  assert.match(publisher, /force-recreate extensions-self/)
  assert.match(publisher, /force-recreate sub2api/)
  assert.doesNotMatch(publisher, /force-recreate sub2api extensions-self/)
  assert.doesNotMatch(publisher, /docker\s+build|compose[^\n]*build/)
  assert.doesNotMatch(publisher, /up -d[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /rm[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /down[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /docker inspect extensions-self[^\n]*\|\| docker inspect risk-control/)
})
