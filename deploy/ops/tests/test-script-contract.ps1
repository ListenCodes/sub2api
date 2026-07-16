$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
$syncScript = Get-Content -Raw -LiteralPath (Join-Path $repoRoot 'deploy\ops\sync-upstream.sh')
$triggerScript = Get-Content -Raw -LiteralPath (Join-Path $repoRoot 'deploy\ops\sync-trigger.sh')
$syncPublishPath = Join-Path $repoRoot 'deploy\ops\sync-and-publish.sh'
$syncPublishScript = if (Test-Path -LiteralPath $syncPublishPath) {
    Get-Content -Raw -LiteralPath $syncPublishPath
} else {
    ''
}
$autoUpdateScript = Get-Content -Raw -LiteralPath (Join-Path $repoRoot 'deploy\ops\auto-update.sh')
$publishScript = Get-Content -Raw -LiteralPath (Join-Path $repoRoot 'deploy\ops\publish-custom.sh')

function Assert-Matches {
    param(
        [string]$Text,
        [string]$Pattern,
        [string]$Message
    )

    if ($Text -notmatch $Pattern) {
        throw "ASSERTION FAILED: $Message"
    }
}

function Assert-NotMatches {
    param(
        [string]$Text,
        [string]$Pattern,
        [string]$Message
    )

    if ($Text -match $Pattern) {
        throw "ASSERTION FAILED: $Message"
    }
}

# Stable Release preparation must be isolated from custom and production.
Assert-Matches $syncScript 'resolve-stable-release\.sh' 'sync resolves the latest stable Release'
Assert-Matches $syncScript 'integration/release-' 'sync publishes a Release integration branch'
Assert-Matches $syncScript 'release_tag' 'sync records the stable Release tag'
Assert-Matches $syncScript 'release_commit' 'sync records the peeled Release commit'
Assert-Matches $syncScript 'release_published_at' 'sync records the Release publication time'
Assert-Matches $syncScript 'git\s+-C\s+"?\$WORKTREE"?\s+merge' 'sync merges upstream in a temporary worktree'
Assert-Matches $syncScript 'refs/tags/' 'sync fetches the verified Release tag'
Assert-Matches $syncScript 'need_restart:false' 'sync reports preparation-only status'
Assert-NotMatches $syncScript 'merge[^\r\n]*upstream/main' 'stable sync never merges upstream/main'
Assert-NotMatches $syncScript 'fetch[^\r\n]*main' 'stable sync never fetches main for publication'
Assert-NotMatches $syncScript 'git\s+rebase' 'sync must not rebase'
Assert-NotMatches $syncScript 'docker\s+(build|compose\s+up)' 'sync must not build or deploy'
Assert-NotMatches $syncScript 'git\s+push\s+[^\r\n]*\bcustom\b' 'sync must not push custom'
Assert-NotMatches $syncScript '--force' 'sync must not force-update refs'
Assert-Matches $syncScript 'SUB2API_SYNC_DEFER_RESULT' 'sync supports deferred trigger results'
Assert-Matches $syncScript 'trap\s+[^\r\n]*ERR' 'sync converts unexpected shell errors into terminal status'
Assert-Matches $syncScript '"\$RESOLVER"' 'sync quotes the configured Release resolver path'
Assert-Matches $syncScript 'BASELINE_RELATIVE[^\r\n]*BASELINE_FILE' 'sync derives a repository-relative baseline path'
Assert-Matches $syncScript 'baseline metadata path must stay inside' 'sync rejects baseline metadata outside the repository'
Assert-Matches $syncScript 'base_commit' 'sync records the approved branch base commit'
Assert-Matches $syncScript 'SCHEDULED_RUN' 'scheduled syncs use an independent run mode'
Assert-Matches $syncScript 'CONFLICT_DIR' 'sync stores conflict artifacts under a configured directory'
Assert-Matches $syncScript 'conflict_files' 'sync records conflicted files'
Assert-Matches $syncScript 'conflict_log' 'sync records the conflict artifact path'
Assert-Matches $syncScript 'conflict_release' 'sync records the conflicting Release identity'
Assert-Matches $triggerScript 'conflict_files' 'admin trigger initializes conflict metadata fields'
Assert-Matches $triggerScript 'release_tag' 'admin trigger initializes Release metadata fields'
Assert-Matches $triggerScript 'release_commit' 'admin trigger initializes Release commit field'
Assert-Matches $triggerScript 'release_published_at' 'admin trigger initializes Release timestamp field'
Assert-Matches $triggerScript 'conflict_release' 'admin trigger initializes Release conflict field'

# Both the scheduled and admin-triggered paths must use the same auto-publish wrapper.
Assert-Matches $autoUpdateScript 'sync-and-publish\.sh' 'scheduled updates use the unified wrapper'
Assert-Matches $syncPublishScript 'publish-custom\.sh' 'unified flow invokes the production publisher'
Assert-Matches $syncPublishScript 'git\s+merge\s+--ff-only' 'unified flow promotes only by fast-forward'
Assert-Matches $syncPublishScript 'BRANCH="\$\{SUB2API_BRANCH:-custom-release\}"' 'unified flow defaults to custom-release'
Assert-Matches $syncPublishScript 'ORIGIN_REMOTE="\$\{SUB2API_ORIGIN_REMOTE:-origin\}"' 'unified flow parameterizes the origin remote'
Assert-Matches $syncPublishScript 'ORIGIN_REF="\$ORIGIN_REMOTE/\$BRANCH"' 'unified flow derives the approved origin ref'
Assert-Matches $syncPublishScript 'SUB2API_SYNC_PUBLISH_LOCK' 'unified flow has an end-to-end lock'
Assert-Matches $syncPublishScript 'published_commit' 'unified flow records the published commit'
Assert-Matches $syncPublishScript 'sync-pending-publish' 'unified flow preserves a failed publish for retry'
Assert-Matches $syncPublishScript 'prepare_scheduled_status' 'scheduled runs initialize independent status metadata'
Assert-NotMatches $syncPublishScript 'origin/custom' 'unified flow must not hardcode origin/custom'
Assert-NotMatches $syncPublishScript 'git\s+fetch\s+origin\s+custom' 'unified flow must not hardcode fetching custom'
Assert-NotMatches $syncPublishScript 'git\s+push\s+origin\s+custom' 'unified flow must not hardcode pushing custom'
Assert-NotMatches $syncPublishScript 'git\s+push\s+[^\r\n]*--force' 'unified flow must not force-push'

# Production publishing must require the configured approved branch commit and preserve rollback data.
Assert-Matches $publishScript -- '--commit' 'publish requires an explicit commit argument'
Assert-Matches $publishScript 'BRANCH="\$\{SUB2API_BRANCH:-custom-release\}"' 'publish defaults to custom-release'
Assert-Matches $publishScript 'ORIGIN_REMOTE="\$\{SUB2API_ORIGIN_REMOTE:-origin\}"' 'publish parameterizes the origin remote'
Assert-Matches $publishScript 'ORIGIN_REF="\$ORIGIN_REMOTE/\$BRANCH"' 'publish derives the approved origin ref'
Assert-Matches $publishScript 'BACKUP_ROOT' 'publish creates a backup under the configured backup root'
Assert-Matches $publishScript 'pg_dump' 'publish backs up PostgreSQL before deployment'
Assert-Matches $publishScript 'docker\s+compose\s+--project-name\s+deploy' 'publish uses the stable Compose project name'
Assert-Matches $publishScript '--no-deps\s+--force-recreate\s+sub2api\s+extensions-self' 'publish recreates only the affected services'
Assert-NotMatches $publishScript 'origin/custom' 'publish must not hardcode origin/custom'
Assert-NotMatches $publishScript 'git\s+fetch\s+origin\s+custom' 'publish must not hardcode fetching custom'
Assert-NotMatches $publishScript 'git\s+push\s+origin\s+custom' 'publish must not hardcode pushing custom'
Assert-NotMatches $publishScript 'git\s+reset\s+--hard' 'publish must not discard source changes'
Assert-NotMatches $publishScript 'git\s+push\s+[^\r\n]*--force' 'publish must not force-push'

Write-Output 'script-contract=PASS'
