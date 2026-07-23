$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path

function Read-RepoFile {
    param([string]$RelativePath)
    Get-Content -Raw -LiteralPath (Join-Path $repoRoot $RelativePath)
}

function Assert-True {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) {
        throw "ASSERTION FAILED: $Message"
    }
}

function Assert-Matches {
    param([string]$Text, [string]$Pattern, [string]$Message)
    if ($Text -notmatch $Pattern) {
        throw "ASSERTION FAILED: $Message"
    }
}

function Assert-NotMatches {
    param([string]$Text, [string]$Pattern, [string]$Message)
    if ($Text -match $Pattern) {
        throw "ASSERTION FAILED: $Message"
    }
}

function Assert-Before {
    param([string]$Text, [string]$Earlier, [string]$Later, [string]$Message)
    $earlierIndex = $Text.IndexOf($Earlier, [System.StringComparison]::Ordinal)
    $laterIndex = $Text.IndexOf($Later, [System.StringComparison]::Ordinal)
    if ($earlierIndex -lt 0 -or $laterIndex -lt 0 -or $earlierIndex -ge $laterIndex) {
        throw "ASSERTION FAILED: $Message"
    }
}

$sync = Read-RepoFile 'deploy\ops\sync-upstream.sh'
$orchestrator = Read-RepoFile 'deploy\ops\sync-and-publish.sh'
$prepare = Read-RepoFile 'deploy\ops\prepare-release.sh'
$apply = Read-RepoFile 'deploy\ops\apply-release.sh'
$common = Read-RepoFile 'deploy\ops\release-common.sh'
$trigger = Read-RepoFile 'deploy\ops\sync-trigger.sh'
$promoter = Read-RepoFile 'deploy\ops\promote-release.sh'
$publisher = Read-RepoFile 'deploy\ops\publish-custom.sh'
$state = Read-RepoFile 'deploy\ops\release-state.sh'
$imageVerifier = Read-RepoFile 'deploy\ops\verify-release-images.sh'
$actionsWaiter = Read-RepoFile 'deploy\ops\wait-for-actions.sh'

foreach ($executor in @($prepare, $apply, $common)) {
    Assert-Matches $executor 'SUB2API_ENV_FILE:-\$REPO/deploy/\.env' 'release executors must default to the production deploy/.env path'
}

# Stable Release integration is exact, isolated, and preparation-only.
Assert-Matches $sync 'resolve-stable-release\.sh' 'sync resolves the latest official stable Release'
Assert-Matches $sync '\[\[\s+"\$BRANCH"\s+==\s+custom-release\s+\]\]' 'sync rejects non-production branches'
Assert-Matches $sync 'refs/tags/\$RELEASE_TAG:refs/tags/\$RELEASE_TAG' 'sync fetches only the exact verified tag'
Assert-Matches $sync 'integration/release-' 'sync creates a Release candidate branch'
Assert-Matches $sync 'git\s+-C\s+"\$WORKTREE"\s+merge' 'sync merges in a temporary worktree'
Assert-Matches $sync 'release_job_update\s+"\$JOB_ID"\s+conflict' 'sync persists conflicts separately from failures'
Assert-Matches $sync 'conflict_files' 'sync persists exact conflict files'
Assert-Matches $sync 'artifact_path' 'sync persists conflict evidence paths'
Assert-NotMatches $sync 'upstream/main|fetch[^\r\n]*\bmain\b' 'sync never publishes upstream/main'
Assert-NotMatches $sync 'git\s+rebase|--force' 'sync never rewrites history'
Assert-NotMatches $sync 'docker\s+(build|compose\s+up)' 'sync never builds or deploys'

# The host orchestrator owns only locking, action dispatch, and one-at-a-time trigger consumption.
foreach ($marker in @(
    'release-trigger', 'flock -n', 'prepare-release.sh', 'apply-release.sh',
    'ACTION', 'LEGACY_SINGLE_PHASE_UNSUPPORTED'
)) {
    Assert-Matches $orchestrator ([regex]::Escape($marker)) "orchestrator is missing $marker"
}
Assert-Before $orchestrator 'flock -n 9' 'claim_job ||' 'orchestrator must acquire its lock before claiming the durable trigger'
Assert-NotMatches $orchestrator 'git\s+(merge|rebase)|--force' 'orchestrator delegates guarded promotion and never rewrites history'
Assert-NotMatches $orchestrator 'publish-custom\.sh' 'orchestrator must not call the deprecated publisher'

# Promotion advances only the already-tested remote ref; the apply executor owns local mutation.
Assert-Matches $promoter 'refs/remotes/' 'promoter pushes an immutable remote-tracking candidate ref'
Assert-Matches $promoter 'refs/heads/\$BRANCH' 'promoter targets only the approved remote branch'
Assert-NotMatches $promoter 'git\s+merge(?:\s|$)|git\s+reset|--force' 'promoter must not move local source or rewrite history'
# Preparation owns remote gates, immutable evidence, Compose rendering, and backups.
foreach ($marker in @('wait-for-actions.sh', 'verify-release-images.sh', 'docker pull', 'pg_dump', 'pg_restore --list', 'SHA256SUMS', 'prepared_manifest', 'expires_at', 'prepared')) {
    Assert-Matches $prepare ([regex]::Escape($marker)) "prepare executor is missing $marker"
}
Assert-NotMatches $prepare 'compose[^\r\n]*\b(?:up|down|rm|restart|stop|kill)\b' 'prepare must not mutate container lifecycle'
Assert-NotMatches $prepare 'release_production_state_write' 'prepare must not write production release state'
Assert-Matches $prepare 'docker inspect sub2api sub2api-postgres sub2api-redis risk-control-postgres extensions-self' 'prepare must back up metadata for the exact production container names'

# Apply is local-only, immutable, and extensions-first.
foreach ($marker in @('release_manifest_valid', 'origin/$BRANCH', 'drifted', '--pull never', 'deploying_extensions', 'deploying_main', 'health_checking', 'release_production_state_write', 'rolling_back', 'restore_source', 'rollback:{attempted:true')) {
    Assert-Matches $apply ([regex]::Escape($marker)) "apply executor is missing $marker"
}
foreach ($marker in @('config --quiet', 'config --format json', '.name ==', 'healthcheck', 'nginx', 'container-metadata', 'rollback-tags.txt', 'SUB2API_IMAGE=')) {
    Assert-Matches $prepare ([regex]::Escape($marker)) "prepare executor is missing $marker"
}
Assert-NotMatches $apply 'git\s+fetch|docker\s+pull|pg_dump|pg_restore|api\.github\.com' 'apply must not access GitHub, pull images, or redo backups'
Assert-NotMatches $apply 'up[^\r\n]*risk-control-postgres|(?:rm|down)[^\r\n]*risk-control-postgres' 'apply must not lifecycle-manage risk-control-postgres'
Assert-Before $apply 'deploying_extensions' 'deploying_main' 'apply deploys extensions before the main application'
Assert-Before $apply 'SOURCE_HEAD=' 'merge --ff-only' 'apply must snapshot the production source before advancing it'
Assert-Before $apply 'status --porcelain' 'merge --ff-only' 'apply must reject a dirty production worktree before advancing it'

Assert-Matches $publisher 'deprecated' 'publisher is a fail-closed compatibility shim'
Assert-Matches $publisher 'exit 64' 'publisher rejects direct invocation'

foreach ($marker in @(
    'org.opencontainers.image.revision', 'org.opencontainers.image.version',
    'org.opencontainers.image.source', 'linux/amd64', '.RepoDigests', 'docker pull',
    'DOCKER_CONFIG'
)) {
    Assert-Matches $imageVerifier ([regex]::Escape($marker)) "image verifier is missing $marker"
}
foreach ($status in @(
    'checking_updates', 'checking_release', 'validating_tag', 'merging_release', 'waiting_actions',
    'waiting_images', 'downloading_images', 'preparing_compose', 'promoting_release', 'backing_up',
    'validating_backup', 'prepared', 'apply_queued', 'deploying_extensions', 'deploying_main',
    'health_checking', 'rolling_back', 'success', 'failed', 'conflict', 'expired', 'drifted'
)) {
    Assert-Matches $state ([regex]::Escape($status)) "durable state helper is missing $status"
}
Assert-Matches $actionsWaiter 'EXPECTED_CHECKS' 'Actions waiter requires the complete validation suite'
Assert-Matches $actionsWaiter 'TIMEOUT_SECONDS' 'Actions waiter has a bounded long-running wait'

# The application helper only creates an atomic trigger and returns.
Assert-Matches $trigger 'release-trigger' 'container helper writes the systemd path trigger'
Assert-Matches $trigger 'mv -f' 'container helper writes the trigger atomically'
Assert-Matches $trigger 'prepare|apply' 'container helper accepts an explicit action'
Assert-NotMatches $trigger '\b(?:sleep|while|until)\b' 'container helper must return immediately'

# Scheduled code is forbidden; systemd.path is the only host consumer.
$autoUpdatePath = Join-Path $repoRoot 'deploy\ops\auto-update.sh'
Assert-True (-not (Test-Path -LiteralPath $autoUpdatePath)) 'auto-update.sh must be deleted'
$pathUnitPath = Join-Path $repoRoot 'deploy\ops\sub2api-release.path'
$serviceUnitPath = Join-Path $repoRoot 'deploy\ops\sub2api-release.service'
Assert-True (Test-Path -LiteralPath $pathUnitPath) 'sub2api-release.path is missing'
Assert-True (Test-Path -LiteralPath $serviceUnitPath) 'sub2api-release.service is missing'
$pathUnit = Get-Content -Raw -LiteralPath $pathUnitPath
$serviceUnit = Get-Content -Raw -LiteralPath $serviceUnitPath
Assert-Matches $pathUnit 'PathExists=/var/lib/docker/volumes/deploy_sub2api_data/_data/release-trigger' 'path unit watches the persistent trigger'
Assert-Matches $pathUnit 'Unit=sub2api-release\.service' 'path unit starts the release service'
Assert-Matches $serviceUnit 'Type=oneshot' 'release service is one-shot'
Assert-Matches $serviceUnit 'Environment=SUB2API_DATA_DIR=/var/lib/docker/volumes/deploy_sub2api_data/_data' 'release service uses the persistent data directory'
Assert-Matches $serviceUnit 'ExecStart=/opt/sub2api-custom/sync-and-publish\.sh' 'release service calls only the unified orchestrator'
Assert-NotMatches $serviceUnit 'sync-upstream\.sh|publish-custom\.sh' 'systemd service must not bypass the orchestrator'

$opsSources = Get-ChildItem -LiteralPath (Join-Path $repoRoot 'deploy\ops') -File -Recurse |
    Where-Object { $_.Extension -in @('.sh', '.service', '.path') } |
    ForEach-Object { Get-Content -Raw -LiteralPath $_.FullName }
$allOpsText = $opsSources -join "`n"
Assert-NotMatches $allOpsText '(?i)--scheduled|scheduled-|SCHEDULED_RUN|prepare_scheduled_status|auto-update\.sh' 'scheduled release behavior remains in deploy/ops'

Write-Output 'script-contract=PASS'
