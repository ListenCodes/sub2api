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
  assert.equal(existsSync(resolve(repoRoot, 'extensions-self/Dockerfile')), true)
})

test('homepage navigation escapes the iframe without changing destinations', () => {
  const homepage = read('extensions-self/homepage/index.html')
  for (const href of ['/', '/admin/dashboard']) {
    const escapedHref = href.replace('/', '\\/')
    assert.match(homepage, new RegExp(`<a(?=[^>]*href="${escapedHref}")(?=[^>]*target="_top")[^>]*>`))
  }
})

test('homepage clips decorative overflow at the document boundary', () => {
  const homepage = read('extensions-self/homepage/index.html')
  assert.match(homepage, /html,body\{[^}]*overflow-x:hidden/)
})

test('compose runs one extensions-self application container and preserves the risk database', () => {
  for (const composeFile of ['deploy/docker-compose.yml', 'deploy/docker-compose.local.yml']) {
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
    assert.match(compose, /RISK_CONTROL_URL=\$\{RISK_CONTROL_URL:-http:\/\/extensions-self:8090\}/)
    assert.match(compose, /^  risk-control-postgres:\s*$/m)
    assert.doesNotMatch(compose, /^  risk-control:\s*$/m)
  }
})

test('the publisher targets extensions-self without managing the risk database lifecycle', () => {
  const publisher = read('deploy/ops/publish-custom.sh')
  assert.match(publisher, /deploy-extensions-self:rollback-\$STAMP/)
  assert.match(publisher, /build extensions-self/)
  assert.match(publisher, /sub2api extensions-self/)
  assert.doesNotMatch(publisher, /up -d[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /rm[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /down[^\n]*risk-control-postgres/)
  assert.doesNotMatch(publisher, /docker inspect extensions-self[^\n]*\|\| docker inspect risk-control/)
  assert.match(publisher, /elif docker container inspect risk-control/)
})
