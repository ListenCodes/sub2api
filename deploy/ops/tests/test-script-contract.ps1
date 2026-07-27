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
$prepareRollback = Read-RepoFile 'deploy\ops\prepare-rollback.sh'
$applyRollback = Read-RepoFile 'deploy\ops\apply-rollback.sh'
$common = Read-RepoFile 'deploy\ops\release-common.sh'
$trigger = Read-RepoFile 'deploy\ops\sync-trigger.sh'
$promoter = Read-RepoFile 'deploy\ops\promote-release.sh'
$publisher = Read-RepoFile 'deploy\ops\publish-custom.sh'
$state = Read-RepoFile 'deploy\ops\release-state.sh'
$updateJob = Read-RepoFile 'backend\internal\service\update_job.go'
$ledger = Read-RepoFile 'deploy\ops\release-ledger.sh'
$ledgerMigration = Read-RepoFile 'deploy\ops\migrate-release-ledger.sh'
$imageVerifier = Read-RepoFile 'deploy\ops\verify-release-images.sh'
$actionsWaiter = Read-RepoFile 'deploy\ops\wait-for-actions.sh'
$prepareSurface = "$prepare`n$common"
$applySurface = "$apply`n$common`n$ledger"

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
Assert-Matches $sync 'conflict_base:\$base_commit' 'sync exposes the approved branch base in conflict metadata'
Assert-NotMatches $sync 'upstream/main|fetch[^\r\n]*\bmain\b' 'sync never publishes upstream/main'
Assert-NotMatches $sync 'git\s+rebase|--force' 'sync never rewrites history'
Assert-NotMatches $sync 'docker\s+(build|compose\s+up)' 'sync never builds or deploys'

# The host orchestrator owns only locking, action dispatch, and one-at-a-time trigger consumption.
foreach ($marker in @(
    'release-trigger', 'flock -n', 'prepare-release.sh', 'apply-release.sh',
    'prepare-rollback.sh', 'apply-rollback.sh', 'operation_kind', 'ACTION',
    'LEGACY_SINGLE_PHASE_UNSUPPORTED'
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
    Assert-Matches $prepareSurface ([regex]::Escape($marker)) "prepare executor surface is missing $marker"
}
Assert-NotMatches $prepare 'compose[^\r\n]*\b(?:up|down|rm|restart|stop|kill)\b' 'prepare must not mutate container lifecycle'
Assert-NotMatches $prepare 'release_production_state_write' 'prepare must not write production release state'
foreach ($marker in @('ledger_list_rollback_release_ids 3', 'verifying_snapshot', 'release_create_complete_backup', 'prepared_at', 'expires_at')) {
    Assert-Matches $prepareRollback ([regex]::Escape($marker)) "rollback prepare is missing $marker"
}
Assert-NotMatches $prepareRollback 'api\.github\.com|wait-for-actions|verify-release-images|compose[^\r\n]*\b(?:up|down|rm|restart|stop|kill)\b' 'rollback prepare must not query remote release gates or mutate containers'
foreach ($marker in @('release_checkout_exact_commit', 'switching_extensions', 'switching_main', '--pull never', 'run_complete_health', 'ledger_commit_rollback', 'ledger_restore_failed_rollback')) {
    Assert-Matches $applyRollback ([regex]::Escape($marker)) "rollback apply is missing $marker"
}
Assert-NotMatches $applyRollback 'docker\s+pull|pg_dump|pg_restore|api\.github\.com|wait-for-actions|verify-release-images' 'rollback apply must be local-only and must not back up or restore databases'
Assert-Matches $prepareSurface 'docker inspect sub2api sub2api-postgres sub2api-redis risk-control-postgres extensions-self' 'prepare must back up metadata for the exact production container names'
foreach ($marker in @(
    'worktree add --detach', '$BACKUP_DIR/target', 'release_stage_target_env',
    'release_render_explicit_compose', 'release_create_complete_backup',
    'base_custom_high_water', 'proposed_custom_sequence', 'advances_custom_version',
    'target_official_version', 'target_custom_version', 'REUSE_VERIFIED_EVIDENCE'
)) {
    Assert-Matches $prepare ([regex]::Escape($marker)) "ledger-aware prepare is missing $marker"
}
Assert-Matches $common 'target_artifact_manifest_sha256' 'shared manifest validation must bind the immutable target artifact manifest'
Assert-Matches $common 'ledger_validate_backup_contract' 'the shared complete backup helper must use the ledger backup contract'
Assert-NotMatches $prepare 'compose[^\r\n]*-f\s+"\$COMPOSE_BASE"' 'prepare must render the staged target Compose pair, not the production pair'

# Apply is local-only, immutable, and extensions-first.
foreach ($marker in @(
    'release_manifest_valid', 'ledger_validate_state', 'ledger_validate_release',
    'origin/$BRANCH', 'drifted', '--pull never', 'switching_extensions',
    'switching_main', 'health_checking', 'ledger_commit_release', 'rolling_back',
    'release_restore_source_snapshot', 'ledger_restore_failed_apply',
    'ledger_settle_pre_mutation_failure', 'ledger_recover_pre_mutation_terminal',
    'validate_update_identity_contract',
    'release_running_container_matches_image', 'rollback:{attempted:true'
)) {
    Assert-Matches $apply ([regex]::Escape($marker)) "apply executor is missing $marker"
}
foreach ($marker in @('config --quiet', 'config --format json', '.name ==', 'healthcheck', 'nginx', 'container-metadata', 'rollback-tags.txt', 'SUB2API_IMAGE=')) {
    Assert-Matches $prepareSurface ([regex]::Escape($marker)) "prepare executor surface is missing $marker"
}
Assert-NotMatches $applySurface 'git\s+(?:fetch|merge|reset|pull)|docker\s+pull|api\.github\.com|wait-for-actions|verify-release-images' 'apply helpers must not access remote gates, move refs, or pull images'
Assert-NotMatches $apply 'pg_dump|pg_restore' 'apply must not invoke database backup or restore commands'
Assert-NotMatches $apply 'up[^\r\n]*risk-control-postgres|(?:rm|down)[^\r\n]*risk-control-postgres' 'apply must not lifecycle-manage risk-control-postgres'
Assert-Before $apply 'switching_extensions' 'switching_main' 'apply deploys extensions before the main application'
Assert-Before $apply 'release_source_snapshot' 'release_checkout_exact_commit' 'apply must snapshot and validate the production source before exact checkout'
Assert-Matches $common 'status --porcelain --untracked-files=all' 'exact source checkout must reject a dirty production worktree'
Assert-Matches $common 'switch --detach' 'exact source checkout must detach at the immutable prepared commit'
Assert-NotMatches $common 'reset --hard|merge --ff-only' 'source switching and restoration must not reset or merge the production worktree'
Assert-Matches $apply 'docker compose --project-name deploy -f "\$COMPOSE_BASE" -f "\$COMPOSE_CUSTOM" --env-file "\$ENV_FILE"' 'apply must use the explicit production Compose pair and environment'
Assert-Matches $apply 'HEALTH_WAIT_TIMEOUT_SECONDS' 'apply health waits must be bounded'
Assert-Matches $apply 'wait_container_healthy extensions-self' 'apply must wait for extensions health before switching the main application'
Assert-Matches $apply 'wait_container_healthy sub2api' 'apply must wait for main application health before reporting success'
Assert-NotMatches $apply 'docker inspect --format ''\{\{\.State\.Health\.Status\}\}'' (?:extensions-self|sub2api) \| grep -q healthy' 'apply must not treat an initial starting health state as failure'
Assert-Matches $apply 'SUB2API_INTERNAL_HEALTH_URL:-http://127\.0\.0\.1:8081/health' 'apply must probe the published production port'
Assert-Matches $apply 'SUB2API_ADMIN_HEALTH_URL:-http://127\.0\.0\.1:8081/admin' 'apply must probe the native admin page on the published production port'
Assert-Matches $apply 'docker exec extensions-self[^\r\n]*http://extensions-self:8090/healthz' 'apply must probe extensions-self from its container network'
Assert-Matches $apply 'RISK_CONTROL_INTERNAL_SECRET' 'apply must load the prepared signing secret for data-quality health'
Assert-Matches $apply '/api/v1/admin/account-monitor/data-quality' 'apply must execute the signed data-quality health gate'
Assert-Matches $apply 'abort_apply' 'apply must route explicit post-mutation failures through rollback'
Assert-Matches $apply 'run_complete_health "\$TARGET_DIR/rendered-compose\.json"[^\r\n]*\|\| abort_apply[\s\S]*live_target_identity_matches[^\r\n]*\|\| abort_apply[\s\S]*if ! ledger_commit_release' 'normal apply must publish the ledger only after complete health and exact runtime identity'
Assert-Matches $apply 'operation_status[^\r\n]*health_checking[\s\S]*live_target_identity_matches; then[\s\S]*run_complete_health "\$TARGET_DIR/rendered-compose\.json"[\s\S]*ledger_commit_release' 'interrupted apply recovery must recheck complete health before ledger publication'
Assert-Matches $ledger 'ledger_create_release' 'ledger publication must create an immutable release record'
Assert-Matches $ledger 'active_operation_id=null' 'ledger publication must settle ownership atomically'
Assert-Matches $ledger '/proc/\$\$/fd/\$inherited_fd' 'ledger mutations must reuse the inherited orchestrator lock descriptor'
Assert-Matches $ledger 'published_commit=\$commit' 'exact recovery must restore complete publication audit metadata'
Assert-Matches $ledger 'if \[\[ "\$operation_status" == success[\s\S]*published_commit == \$record\.custom_commit[\s\S]*if \[\[ "\$state_release_id"' 'exact recovery must validate terminal publication metadata before moving the ledger pointer'
Assert-NotMatches $apply 'curl[^\r\n]*\|\| fail_apply' 'post-mutation HTTP failures must not bypass rollback'
Assert-NotMatches $apply 'release_production_state_write' 'apply must publish compatibility state only through the ledger transaction'

Assert-Matches $publisher 'deprecated' 'publisher is a fail-closed compatibility shim'
Assert-Matches $publisher 'exit 64' 'publisher rejects direct invocation'

foreach ($marker in @(
    'org.opencontainers.image.revision', 'org.opencontainers.image.version',
    'org.opencontainers.image.source', 'linux/amd64', '.RepoDigests', 'docker pull',
    'DOCKER_CONFIG'
)) {
    Assert-Matches $imageVerifier ([regex]::Escape($marker)) "image verifier is missing $marker"
}
$canonicalStatuses = [regex]::Matches(
    $updateJob,
    '(?m)^\s*ReleaseStatus\w+\s+=\s+"([a-z][a-z0-9_]*)"'
) | ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique
if ($canonicalStatuses.Count -eq 0) {
    throw 'ASSERTION FAILED: backend canonical release status constants are missing'
}
foreach ($status in $canonicalStatuses) {
    Assert-Matches $state ([regex]::Escape($status)) "durable state helper is missing $status"
}
Assert-Matches $actionsWaiter 'EXPECTED_CHECKS' 'Actions waiter requires the complete validation suite'
Assert-Matches $actionsWaiter 'TIMEOUT_SECONDS' 'Actions waiter has a bounded long-running wait'

foreach ($marker in @('ledger_state_path', 'ledger_release_path', 'ledger_operation_path', 'ledger_validate_state', 'ledger_validate_release', 'ledger_atomic_write', 'ledger_create_release')) {
    Assert-Matches $ledger ([regex]::Escape($marker)) "release ledger helper is missing $marker"
}
Assert-Matches $ledger 'ln\s+"\$temporary"\s+"\$path"' 'immutable records must use atomic hard-link creation'
Assert-Matches $ledger 'flock\s+-x' 'ledger mutations must acquire an exclusive release-ledger lock'
Assert-Matches $ledger '/var/lock/sub2api-release\.lock' 'ledger mutations must share the production release lock outside the ledger root'
Assert-Matches $state 'release-ledger[/\\]operations|RELEASE_OPERATIONS_DIR' 'new operations must live under the release ledger'
Assert-Matches $state 'LEGACY_SINGLE_PHASE_UNSUPPORTED' 'legacy release-jobs must fail closed with an explicit error'
Assert-Matches $state 'sync\s+-f' 'operation writes must fsync before and after rename'
Assert-Matches $state 'rm\s+-f\s+"\$temporary"' 'operation writes must remove a failed temporary file'
Assert-Matches $updateJob '\.Sync\(\)' 'Go operation and current-pointer writes must be crash durable'
Assert-NotMatches $ledger 'cat[^\r\n]*\.env|source[^\r\n]*\.env' 'ledger helper must not read environment contents'
foreach ($marker in @('aa2d24106cab0a03785330d8e0ff4e02b0474a0e', 'v0.1.163', 'v1.0.0', 'after_release_record', 'after_projection')) {
    Assert-Matches $ledgerMigration ([regex]::Escape($marker)) "ledger migration is missing $marker"
}
Assert-NotMatches $ledgerMigration 'compose[^\r\n]*\b(?:up|down|restart|stop|kill)\b|docker\s+pull|pg_restore|git\s+(?:reset|merge|rebase)' 'ledger migration must not mutate production runtime or source'

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
Assert-Matches $serviceUnit 'Restart=on-failure' 'release service restarts after an interrupted executor'
Assert-Matches $serviceUnit 'RestartSec=5s' 'release service uses a bounded restart delay'
Assert-Matches $serviceUnit 'Environment=SUB2API_DATA_DIR=/var/lib/docker/volumes/deploy_sub2api_data/_data' 'release service uses the persistent data directory'
Assert-Matches $serviceUnit 'ExecStart=/opt/sub2api-custom/sync-and-publish\.sh' 'release service calls only the unified orchestrator'
Assert-NotMatches $serviceUnit 'sync-upstream\.sh|publish-custom\.sh' 'systemd service must not bypass the orchestrator'

$opsSources = Get-ChildItem -LiteralPath (Join-Path $repoRoot 'deploy\ops') -File -Recurse |
    Where-Object { $_.Extension -in @('.sh', '.service', '.path') } |
    ForEach-Object { Get-Content -Raw -LiteralPath $_.FullName }
$allOpsText = $opsSources -join "`n"
Assert-NotMatches $allOpsText '(?i)--scheduled|scheduled-|SCHEDULED_RUN|prepare_scheduled_status|auto-update\.sh' 'scheduled release behavior remains in deploy/ops'

Write-Output 'script-contract=PASS'
