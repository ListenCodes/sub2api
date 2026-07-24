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

test('operator documentation describes the one-hour and Docker bootstrap contracts', () => {
  const documents = [
    'deploy/README.md',
    'deploy/ops/README.md',
    'deploy/RELEASE-RUNBOOK.md',
    'docs/SUB2API-CUSTOM-OPERATIONS.md',
  ]

  for (const path of documents) {
    const document = read(path)
    assert.match(document, /(?:one[- ]hour|60-minute|1 小时)/i, `${path} must document the one-hour confirmation window`)
    assert.match(document, /export-custom-site\.sh/, `${path} must document complete-site export`)
    assert.match(document, /bootstrap-custom-site\.sh fresh/, `${path} must document fresh bootstrap`)
    assert.match(document, /bootstrap-custom-site\.sh migrate/, `${path} must document migration bootstrap`)
  }
})
