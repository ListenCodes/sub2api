import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, relative, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const deployRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(deployRoot, '..')
const frontendSource = resolve(repoRoot, 'frontend', 'src')

const permanentAllowlist = new Map([
  [
    'frontend/src/components/common/__tests__/AnnouncementPopup.spec.ts',
    {
      readerCalls: 1,
      targetLiterals: 2,
      reason: 'JSDOM does not load the shared Markdown stylesheet.',
    },
  ],
  [
    'frontend/src/views/user/__tests__/stripeLazyLoading.spec.ts',
    {
      readerCalls: 1,
      targetLiterals: 4,
      reason: 'The test protects the Stripe loader and production chunk boundary.',
    },
  ],
])

// Existing debt is frozen by path, filesystem-reader call sites, and statically
// named source targets. Migrate these to imports, mounts, or browser tests; do
// not add new source-reading tests to this list.
const legacyDebt = new Map([
  ['frontend/src/components/admin/account/__tests__/ReAuthAccountModal.grok.spec.ts', { readerCalls: 1, targetLiterals: 1 }],
  ['frontend/src/components/channels/__tests__/AvailableChannelsTable.spec.ts', { readerCalls: 1, targetLiterals: 1 }],
  ['frontend/src/components/layout/__tests__/AppSidebar.spec.ts', { readerCalls: 3, targetLiterals: 3 }],
  ['frontend/src/components/layout/__tests__/TablePageLayout.spec.ts', { readerCalls: 1, targetLiterals: 1 }],
  ['frontend/src/components/layout/__tests__/docUrlSanitization.spec.ts', { readerCalls: 3, targetLiterals: 3 }],
  ['frontend/src/components/layout/__tests__/siteLogoSanitization.spec.ts', { readerCalls: 3, targetLiterals: 3 }],
  ['frontend/src/features/channel-monitor-v2/__tests__/designSystem.structure.spec.ts', { readerCalls: 1, targetLiterals: 7 }],
  ['frontend/src/features/prompt-audit/__tests__/integrationSurface.spec.ts', { readerCalls: 1, targetLiterals: 4 }],
  ['frontend/src/router/__tests__/account-monitor-route.spec.ts', { readerCalls: 2, targetLiterals: 2 }],
  ['frontend/src/router/__tests__/user-risk-control-routes.spec.ts', { readerCalls: 8, targetLiterals: 14 }],
  ['frontend/src/views/admin/__tests__/ExtensionStyleAlignment.spec.ts', { readerCalls: 1, targetLiterals: 12 }],
  ['frontend/src/views/admin/__tests__/groupsModelsListLayout.spec.ts', { readerCalls: 1, targetLiterals: 1 }],
])

function listTestFiles(directory, insideTestDirectory = false) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = resolve(directory, entry.name)
    const nextInsideTestDirectory = insideTestDirectory || entry.name === '__tests__'
    if (entry.isDirectory()) return listTestFiles(path, nextInsideTestDirectory)
    const isScript = /\.[cm]?[jt]sx?$/.test(entry.name)
    const isTest = /\.(?:spec|test)\.[cm]?[jt]sx?$/.test(entry.name)
    return isScript && (insideTestDirectory || isTest) ? [path] : []
  })
}

function repoPath(path) {
  return relative(repoRoot, path).replaceAll('\\', '/')
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function sourceReadingProfile(source) {
  const readerNames = new Set()
  const namespaces = new Set()
  const fsModulePattern = /(?:from\s*['"](?:node:)?fs(?:\/promises)?['"]|require\s*\(\s*['"](?:node:)?fs(?:\/promises)?['"]\s*\)|import\s*\(\s*['"](?:node:)?fs(?:\/promises)?['"]\s*\))/g
  const hasFsModule = fsModulePattern.test(source)

  for (const match of source.matchAll(/import\s*\{([^}]+)\}\s*from\s*['"](?:node:)?fs(?:\/promises)?['"]/g)) {
    for (const binding of match[1].split(',')) {
      const parts = binding.trim().split(/\s+as\s+/)
      if (parts[0] === 'readFile' || parts[0] === 'readFileSync') readerNames.add(parts[1] || parts[0])
      if (parts[0] === 'promises' && parts[1]) namespaces.add(parts[1])
    }
  }
  for (const match of source.matchAll(/(?:const|let|var)\s*\{([^}]+)\}\s*=\s*require\s*\(\s*['"](?:node:)?fs(?:\/promises)?['"]\s*\)/g)) {
    for (const binding of match[1].split(',')) {
      const parts = binding.trim().split(/\s*:\s*/)
      if (parts[0] === 'readFile' || parts[0] === 'readFileSync') readerNames.add(parts[1] || parts[0])
      if (parts[0] === 'promises' && parts[1]) namespaces.add(parts[1])
    }
  }
  for (const match of source.matchAll(/import\s+(?:\*\s+as\s+|)([A-Za-z_$][\w$]*)\s+from\s*['"](?:node:)?fs(?:\/promises)?['"]/g)) {
    namespaces.add(match[1])
  }
  for (const match of source.matchAll(/(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*require\s*\(\s*['"](?:node:)?fs(?:\/promises)?['"]\s*\)(?:\s*\.\s*promises)?/g)) {
    namespaces.add(match[1])
  }

  let readerCalls = 0
  for (const name of readerNames) {
    readerCalls += source.match(new RegExp(`\\b${escapeRegExp(name)}\\s*\\(`, 'g'))?.length ?? 0
  }
  for (const namespace of namespaces) {
    readerCalls += source.match(new RegExp(`\\b${escapeRegExp(namespace)}\\s*\\.(?:\\s*promises\\s*\\.)?\\s*readFile(?:Sync)?\\s*\\(`, 'g'))?.length ?? 0
  }
  readerCalls += source.match(/require\s*\(\s*['"](?:node:)?fs(?:\/promises)?['"]\s*\)(?:\s*\.\s*promises)?\s*\.\s*readFile(?:Sync)?\s*\(/g)?.length ?? 0

  const rawImports = source.match(/(?:from\s*['"][^'"]+\?raw(?:[^'"]*)['"]|import\s*\(\s*['"][^'"]+\?raw(?:[^'"]*)['"]\s*\))/g)?.length ?? 0
  const targetLiterals = new Set()
  for (const match of source.matchAll(/(['"`])([^'"`\r\n]*\.(?:vue|[cm]?[jt]sx?|css|scss|json))\1/g)) {
    targetLiterals.add(match[2])
  }

  if ((!hasFsModule || readerCalls === 0) && rawImports === 0) return null
  const esmExports = source.match(/\bexport\s+(?:default\b|(?:async\s+)?function\b|(?:const|let|var|class)\b|\{|\*)/g)?.length ?? 0
  const commonJsExports = source.match(/\b(?:module\s*\.\s*exports(?:\s*\.|\s*=)|exports\s*\.\s*[A-Za-z_$][\w$]*)/g)?.length ?? 0
  const exportedReaderSurface = esmExports + commonJsExports
  return { readerCalls, targetLiterals: targetLiterals.size, rawImports, exportedReaderSurface }
}

test('source-reading scanner covers aliases, namespaces, async readers, wrappers, and raw imports', () => {
  assert.deepEqual(
    sourceReadingProfile(`
      import { readFile as readSource } from 'node:fs/promises'
      import { promises as filesystem } from 'fs'
      import * as fs from 'node:fs'
      const read = (path) => readSource(path, 'utf8')
      read('../First.vue')
      read('../Second.ts')
      filesystem.readFileSync('../Third.css', 'utf8')
      fs.promises.readFile('../Fifth.json', 'utf8')
      import rawSource from '../Fourth.vue?raw'
    `),
    { readerCalls: 3, targetLiterals: 4, rawImports: 1, exportedReaderSurface: 0 }
  )
  assert.equal(sourceReadingProfile(`import { writeFileSync } from 'node:fs'`), null)
  assert.equal(
    sourceReadingProfile(`
      import { readFileSync } from 'node:fs'
      export const readSource = (path) => readFileSync(path, 'utf8')
    `)?.exportedReaderSurface,
    1,
    'source-reading helpers must not be exportable to untracked consumer files'
  )
  assert.equal(
    sourceReadingProfile(`
      const { readFileSync } = require('node:fs')
      const readSource = (path) => readFileSync(path, 'utf8')
      module.exports = { readSource }
    `)?.exportedReaderSurface,
    1,
    'CommonJS source-reading helpers must not be exportable either'
  )
})

test('frontend source-reading tests stay within the reviewed debt budget', () => {
  const actual = new Map()
  for (const path of listTestFiles(frontendSource)) {
    const source = readFileSync(path, 'utf8')
    const profile = sourceReadingProfile(source)
    if (profile) actual.set(repoPath(path), profile)
  }

  const expected = new Map([
    ...[...permanentAllowlist].map(([path, policy]) => [path, {
      readerCalls: policy.readerCalls,
      targetLiterals: policy.targetLiterals,
      rawImports: 0,
      exportedReaderSurface: 0,
    }]),
    ...[...legacyDebt].map(([path, policy]) => [path, {
      ...policy,
      rawImports: 0,
      exportedReaderSurface: 0,
    }]),
  ])
  assert.deepEqual(
    [...actual].sort(([left], [right]) => left.localeCompare(right)),
    [...expected].sort(([left], [right]) => left.localeCompare(right)),
    'migrate an existing entry or add a behavior test; do not expand source-reading test debt'
  )

  for (const [path, policy] of permanentAllowlist) {
    assert.ok(policy.reason.length >= 20, `${path} needs a concrete allowlist reason`)
  }
})
