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
$trigger = Read-RepoFile 'deploy\ops\sync-trigger.sh'
$promoter = Read-RepoFile 'deploy\ops\promote-release.sh'
$publisher = Read-RepoFile 'deploy\ops\publish-custom.sh'
$state = Read-RepoFile 'deploy\ops\release-state.sh'
$imageVerifier = Read-RepoFile 'deploy\ops\verify-release-images.sh'
$actionsWaiter = Read-RepoFile 'deploy\ops\wait-for-actions.sh'

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

# The host orchestrator owns the durable state machine and one-at-a-time trigger.
foreach ($marker in @(
    'release-trigger', 'flock -n', 'waiting_actions', 'waiting_images',
    'promoting_release', 'publish-custom.sh', '--main-digest', '--extensions-digest'
)) {
    Assert-Matches $orchestrator ([regex]::Escape($marker)) "orchestrator is missing $marker"
}
Assert-Before $orchestrator 'waiting_actions' 'waiting_images' 'Actions must finish before image verification'
Assert-Before $orchestrator 'waiting_images' 'promoting_release' 'images must be verified before branch promotion'
Assert-Before $orchestrator 'promoting_release' 'Publishing commit' 'promotion must precede publication'
Assert-Before $orchestrator 'flock -n 9' 'claim_job ||' 'orchestrator must acquire its lock before claiming the durable trigger'
Assert-NotMatches $orchestrator 'git\s+(merge|rebase)|--force' 'orchestrator delegates guarded promotion and never rewrites history'

# Promotion advances only the already-tested remote ref; publisher moves local source after backup.
Assert-Matches $promoter 'refs/remotes/' 'promoter pushes an immutable remote-tracking candidate ref'
Assert-Matches $promoter 'refs/heads/\$BRANCH' 'promoter targets only the approved remote branch'
Assert-NotMatches $promoter 'git\s+merge(?:\s|$)|git\s+reset|--force' 'promoter must not move local source or rewrite history'
Assert-Before $publisher 'job_update backing_up' 'git merge --ff-only' 'publisher backs up before local source fast-forward'

# Publisher validates immutable images, creates complete rollback evidence, and stages both services.
foreach ($marker in @(
    '--commit', '--main-digest', '--extensions-digest', 'verify-release-images.sh',
    'release-state.json', 'docker exec sub2api-postgres pg_dump',
    'docker exec risk-control-postgres pg_dump', 'pg_restore --list',
    'certificate-metadata.tsv', 'container-metadata.json', 'image-metadata.json',
    'docker-compose.custom.yml', 'main-docker-compose.yml', 'custom-docker-compose.yml',
    'MAIN_ROLLBACK_TAG', 'EXTENSIONS_ROLLBACK_TAG', 'SHA256SUMS',
    'deploying_extensions', 'deploying_main', 'health_checking',
    'rolling_back', 'perform_rollback', 'artifact_path', 'LEGACY_BOOTSTRAP'
)) {
    Assert-Matches $publisher ([regex]::Escape($marker)) "publisher is missing $marker"
}
Assert-Matches $publisher 'SUB2API_IMAGE="\$TARGET_MAIN_REF"' 'publisher validates the target main digest in Compose'
Assert-Matches $publisher 'EXTENSIONS_SELF_IMAGE="\$TARGET_EXTENSIONS_REF"' 'publisher validates the target extensions digest in Compose'
Assert-Matches $publisher '-f "\$COMPOSE_BASE" -f "\$COMPOSE_CUSTOM"' 'publisher renders the explicit base and custom Compose pair'
Assert-Matches $publisher 'git cat-file -e "\$APPROVED_COMMIT:deploy/docker-compose\.custom\.yml"' 'publisher requires the approved target to contain the custom Compose overlay'
Assert-Matches $publisher 'cp -p "\$CURRENT_COMPOSE_CUSTOM" "\$BACKUP_DIR/custom-docker-compose\.yml"' 'publisher backs up the exact custom Compose overlay used for the current deployment'
Assert-Matches $publisher 'compose_with "\$ROLLBACK_BASE" "\$ROLLBACK_CUSTOM"' 'rollback uses the backed-up Compose pair'
Assert-Matches $publisher 'full_health_check "\$ROLLBACK_BASE" "\$ROLLBACK_CUSTOM"' 'rollback health checks validate the backed-up Compose pair'
Assert-Matches $publisher 'full_health_check "\$COMPOSE_BASE" "\$COMPOSE_CUSTOM"' 'target health checks validate the current Compose pair'
Assert-Before $publisher 'cp -p "$COMPOSE_BASE" "$BACKUP_DIR/main-docker-compose.yml"' 'git merge --ff-only' 'publisher backs up the base Compose before source fast-forward'
Assert-Before $publisher 'custom-docker-compose.yml' 'git merge --ff-only' 'publisher backs up the custom Compose before source fast-forward'
Assert-Matches $publisher 'force-recreate\s+extensions-self' 'publisher deploys extensions first'
Assert-Matches $publisher 'force-recreate\s+sub2api' 'publisher deploys main separately'
Assert-NotMatches $publisher 'force-recreate\s+sub2api\s+extensions-self' 'publisher must not recreate both services in one command'
Assert-NotMatches $publisher 'docker\s+build|compose[^\r\n]*\bbuild\b' 'publisher never builds on the VPS'
Assert-NotMatches $publisher 'up[^\r\n]*risk-control-postgres|(?:rm|down)[^\r\n]*risk-control-postgres' 'publisher never recreates or removes risk-control-postgres'
Assert-NotMatches $publisher 'pg_restore[^\r\n]*(?:--clean|--if-exists|-d\s)' 'publisher never restores a database automatically'
Assert-NotMatches $publisher 'git\s+reset|git\s+push[^\r\n]*--force' 'publisher never discards source changes or force-pushes'

foreach ($marker in @(
    'org.opencontainers.image.revision', 'org.opencontainers.image.version',
    'org.opencontainers.image.source', 'linux/amd64', '.RepoDigests', 'docker pull',
    'DOCKER_CONFIG'
)) {
    Assert-Matches $imageVerifier ([regex]::Escape($marker)) "image verifier is missing $marker"
}
foreach ($status in @(
    'checking_release', 'validating_tag', 'merging_release', 'waiting_actions',
    'waiting_images', 'promoting_release', 'backing_up', 'deploying_extensions',
    'deploying_main', 'health_checking', 'rolling_back', 'success', 'failed', 'conflict'
)) {
    Assert-Matches $state ([regex]::Escape($status)) "durable state helper is missing $status"
}
Assert-Matches $actionsWaiter 'EXPECTED_CHECKS' 'Actions waiter requires the complete validation suite'
Assert-Matches $actionsWaiter 'TIMEOUT_SECONDS' 'Actions waiter has a bounded long-running wait'

# The application helper only creates an atomic trigger and returns.
Assert-Matches $trigger 'release-trigger' 'container helper writes the systemd path trigger'
Assert-Matches $trigger 'mv -f' 'container helper writes the trigger atomically'
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
