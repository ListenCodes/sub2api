$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
$syncScript = Get-Content -Raw -LiteralPath (Join-Path $repoRoot 'deploy\ops\sync-upstream.sh')
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

# Upstream preparation must be isolated from custom and production.
Assert-Matches $syncScript 'git\s+fetch\s+"?\$UPSTREAM_REMOTE"?' 'sync fetches the configured upstream remote'
Assert-Matches $syncScript 'git\s+-C\s+"?\$WORKTREE"?\s+merge' 'sync merges upstream in a temporary worktree'
Assert-Matches $syncScript 'integration/upstream-' 'sync publishes an integration branch'
Assert-Matches $syncScript 'need_restart:false' 'sync reports preparation-only status'
Assert-NotMatches $syncScript 'git\s+rebase' 'sync must not rebase'
Assert-NotMatches $syncScript 'docker\s+(build|compose\s+up)' 'sync must not build or deploy'
Assert-NotMatches $syncScript 'git\s+push\s+[^\r\n]*\bcustom\b' 'sync must not push custom'
Assert-NotMatches $syncScript '--force' 'sync must not force-update refs'

# Production publishing must require an approved origin/custom commit and preserve rollback data.
Assert-Matches $publishScript -- '--commit' 'publish requires an explicit commit argument'
Assert-Matches $publishScript 'origin/custom' 'publish validates against origin/custom'
Assert-Matches $publishScript 'BACKUP_ROOT' 'publish creates a backup under the configured backup root'
Assert-Matches $publishScript 'pg_dump' 'publish backs up PostgreSQL before deployment'
Assert-Matches $publishScript 'docker\s+compose\s+--project-name\s+deploy' 'publish uses the stable Compose project name'
Assert-Matches $publishScript '--no-deps\s+--force-recreate\s+sub2api\s+risk-control' 'publish recreates only the affected services'
Assert-NotMatches $publishScript 'git\s+reset\s+--hard' 'publish must not discard source changes'
Assert-NotMatches $publishScript 'git\s+push\s+[^\r\n]*--force' 'publish must not force-push'

Write-Output 'script-contract=PASS'
