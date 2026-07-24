import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const read = (path) => readFileSync(new URL(`../../${path}`, import.meta.url), 'utf8')

test('prepared update and rollback manifests remain valid for one hour', () => {
  const update = read('deploy/ops/prepare-release.sh')
  const rollback = read('deploy/ops/prepare-rollback.sh')

  assert.match(update, /date -u -d '\+60 minutes'/)
  assert.match(rollback, /\$prepared_at \+60 minutes/)
  assert.doesNotMatch(update, /\+15 minutes/)
  assert.doesNotMatch(rollback, /\+15 minutes/)
})
