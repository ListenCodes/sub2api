#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
  printf 'release pipeline test failed: %s\n' "$1" >&2
  exit 1
}

assert_eq() {
  local expected="$1"
  local actual="$2"
  local message="$3"
  [[ "$actual" == "$expected" ]] || fail "$message (expected=$expected actual=$actual)"
}

export SUB2API_DATA_DIR="$TMP_DIR/data"
source "$ROOT_DIR/deploy/ops/release-state.sh"

release_job_init update-fixture
release_job_update update-fixture resolving_target 'Waiting for Actions' '{"action":"prepare","target_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}'
job_file="$SUB2API_DATA_DIR/release-ledger/operations/update-fixture.json"
assert_eq resolving_target "$(jq -r '.status' "$job_file")" 'job state was not persisted'
assert_eq aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "$(jq -r '.target_commit' "$job_file")" 'job metadata was not merged'
assert_eq update-fixture "$(cat "$SUB2API_DATA_DIR/release-current-job-id")" 'current job id was not persisted'
if release_job_update update-fixture unknown_state invalid '{}' >/dev/null 2>&1; then
  fail 'unknown release job state was accepted'
fi

trigger_output="$(SUB2API_DATA_DIR="$SUB2API_DATA_DIR" /bin/sh "$ROOT_DIR/deploy/ops/sync-trigger.sh")"
assert_eq 'prepare update-fixture' "$(cat "$SUB2API_DATA_DIR/release-trigger")" 'trigger did not carry phase and durable job id'
[[ "$trigger_output" == *'release triggered: update-fixture'* ]] || fail 'trigger did not return immediately with the job id'
release_job_init update-explicit
release_job_update update-explicit resolving_target queued '{"action":"apply"}'
printf 'update-fixture\n' > "$SUB2API_DATA_DIR/release-current-job-id"
SUB2API_DATA_DIR="$SUB2API_DATA_DIR" /bin/sh "$ROOT_DIR/deploy/ops/sync-trigger.sh" apply update-explicit >/dev/null
assert_eq 'apply update-explicit' "$(cat "$SUB2API_DATA_DIR/release-trigger")" 'trigger ignored the explicit phase and durable job id'
release_job_init rollback-expire-trigger
release_job_update rollback-expire-trigger prepared prepared \
  '{"action":"prepare","operation_kind":"rollback"}'
SUB2API_DATA_DIR="$SUB2API_DATA_DIR" /bin/sh "$ROOT_DIR/deploy/ops/sync-trigger.sh" expire rollback-expire-trigger >/dev/null
assert_eq 'expire rollback-expire-trigger' "$(cat "$SUB2API_DATA_DIR/release-trigger")" \
  'trigger did not persist the explicit expiration settlement action'
printf 'update-a.b\n' > "$SUB2API_DATA_DIR/release-current-job-id"
mkdir -p "$SUB2API_DATA_DIR/release-ledger/operations"
touch "$SUB2API_DATA_DIR/release-ledger/operations/update-a.b.json"
if SUB2API_DATA_DIR="$SUB2API_DATA_DIR" /bin/sh "$ROOT_DIR/deploy/ops/sync-trigger.sh" >/dev/null 2>&1; then
  fail 'trigger accepted a job id containing a forbidden character'
fi
printf 'update-fixture\n' > "$SUB2API_DATA_DIR/release-current-job-id"

DISPATCH_DIR="$TMP_DIR/dispatch"
mkdir -p "$DISPATCH_DIR/bin"
for executor in prepare-release apply-release prepare-rollback apply-rollback; do
  printf '#!/usr/bin/env sh\nprintf "%%s %%s\\n" "%s" "$2" >> "$DISPATCH_CALLS"\n' "$executor" > "$DISPATCH_DIR/bin/$executor.sh"
  chmod +x "$DISPATCH_DIR/bin/$executor.sh"
done
printf '#!/usr/bin/env sh\nexit 0\n' > "$DISPATCH_DIR/bin/flock"
chmod +x "$DISPATCH_DIR/bin/flock"
run_dispatch() {
  local kind="$1" action="$2" status="$3" claim_mode="${4:-trigger}" job_id expected
  job_id="$kind-$action"
  SUB2API_DATA_DIR="$DISPATCH_DIR/data" RELEASE_LEDGER_ROOT="$DISPATCH_DIR/data/release-ledger" RELEASE_OPERATIONS_DIR="$DISPATCH_DIR/data/release-ledger/operations" RELEASE_JOBS_DIR="$DISPATCH_DIR/data/release-ledger/operations" CURRENT_RELEASE_JOB_FILE="$DISPATCH_DIR/data/release-current-job-id"
  mkdir -p "$RELEASE_OPERATIONS_DIR"
  release_job_init "$job_id"
  release_job_update "$job_id" "$status" queued "$(jq -n --arg action "$action" '{action:$action}')"
  jq -n --arg release release-dispatch-fixture \
    '{schema_version:1,current_release_id:$release,custom_version_high_water:4,active_operation_id:null,updated_at:"2026-07-23T08:00:00Z"}' \
    > "$RELEASE_LEDGER_ROOT/state.json"
  rm -f "$DISPATCH_DIR/data/release-trigger" "$DISPATCH_DIR/data/release-trigger.processing."*
  if [[ "$claim_mode" == stale ]]; then
    printf '%s %s\n' "$action" "$job_id" > "$DISPATCH_DIR/data/release-trigger.processing.99999"
  else
    printf '%s %s\n' "$action" "$job_id" > "$DISPATCH_DIR/data/release-trigger"
  fi
  : > "$DISPATCH_DIR/calls"
  PATH="$DISPATCH_DIR/bin:$PATH" DISPATCH_CALLS="$DISPATCH_DIR/calls" SUB2API_DATA_DIR="$DISPATCH_DIR/data" \
    SUB2API_PREPARE_SCRIPT="$DISPATCH_DIR/bin/prepare-release.sh" SUB2API_APPLY_SCRIPT="$DISPATCH_DIR/bin/apply-release.sh" \
    SUB2API_PREPARE_ROLLBACK_SCRIPT="$DISPATCH_DIR/bin/prepare-rollback.sh" SUB2API_APPLY_ROLLBACK_SCRIPT="$DISPATCH_DIR/bin/apply-rollback.sh" \
    SUB2API_SYNC_PUBLISH_LOCK="$DISPATCH_DIR/release.lock" SUB2API_SYNC_PUBLISH_LOG="$DISPATCH_DIR/release.log" \
    "$ROOT_DIR/deploy/ops/sync-and-publish.sh"
  if [[ "$kind" == update ]]; then expected="$action-release"; else expected="$action-rollback"; fi
  assert_eq "$expected $job_id" "$(cat "$DISPATCH_DIR/calls")" "$kind $action dispatched to wrong executor"
  assert_eq "$job_id" "$(jq -r '.active_operation_id // empty' "$RELEASE_LEDGER_ROOT/state.json")" \
    "$kind $action executor ran before the ledger operation was claimed"
}
run_dispatch update prepare resolving_target
run_dispatch update apply apply_queued
run_dispatch rollback prepare resolving_target
run_dispatch rollback apply apply_queued
run_dispatch update prepare resolving_target stale

missing_job_id='update-missing-executor'
release_job_init "$missing_job_id"
release_job_update "$missing_job_id" resolving_target queued '{"action":"prepare"}'
jq -n --arg release release-dispatch-fixture \
  '{schema_version:1,current_release_id:$release,custom_version_high_water:4,active_operation_id:null,updated_at:"2026-07-23T08:00:00Z"}' \
  > "$RELEASE_LEDGER_ROOT/state.json"
printf 'prepare %s\n' "$missing_job_id" > "$DISPATCH_DIR/data/release-trigger"
if PATH="$DISPATCH_DIR/bin:$PATH" DISPATCH_CALLS="$DISPATCH_DIR/calls" SUB2API_DATA_DIR="$DISPATCH_DIR/data" \
  SUB2API_PREPARE_SCRIPT="$DISPATCH_DIR/bin/missing-prepare-release.sh" \
  SUB2API_SYNC_PUBLISH_LOCK="$DISPATCH_DIR/release.lock" SUB2API_SYNC_PUBLISH_LOG="$DISPATCH_DIR/release.log" \
  "$ROOT_DIR/deploy/ops/sync-and-publish.sh" >/dev/null 2>&1; then
  fail 'dispatcher accepted a missing executor'
fi
assert_eq '' "$(jq -r '.active_operation_id // empty' "$RELEASE_LEDGER_ROOT/state.json")" \
  'missing executor retained the active ledger operation'

expired_job_id='rollback-expired'
release_job_init "$expired_job_id"
expired_at="$(date -u -d '1 minute ago' '+%Y-%m-%dT%H:%M:%SZ')"
release_job_update "$expired_job_id" prepared prepared \
  "$(jq -n --arg expires_at "$expired_at" '{action:"prepare",operation_kind:"rollback",expires_at:$expires_at,published:false,production_changed:false}')"
jq -n --arg release release-dispatch-fixture --arg operation "$expired_job_id" \
  '{schema_version:1,current_release_id:$release,custom_version_high_water:4,active_operation_id:$operation,updated_at:"2026-07-23T08:00:00Z"}' \
  > "$RELEASE_LEDGER_ROOT/state.json"
printf 'expire %s\n' "$expired_job_id" > "$DISPATCH_DIR/data/release-trigger"
: > "$DISPATCH_DIR/calls"
PATH="$DISPATCH_DIR/bin:$PATH" DISPATCH_CALLS="$DISPATCH_DIR/calls" SUB2API_DATA_DIR="$DISPATCH_DIR/data" \
  SUB2API_SYNC_PUBLISH_LOCK="$DISPATCH_DIR/release.lock" SUB2API_SYNC_PUBLISH_LOG="$DISPATCH_DIR/release.log" \
  "$ROOT_DIR/deploy/ops/sync-and-publish.sh"
assert_eq expired "$(jq -r '.status' "$RELEASE_OPERATIONS_DIR/$expired_job_id.json")" \
  'expired preparation was not settled by the locked host dispatcher'
assert_eq '' "$(jq -r '.active_operation_id // empty' "$RELEASE_LEDGER_ROOT/state.json")" \
  'expired preparation retained the active ledger operation'
assert_eq release-dispatch-fixture "$(jq -r '.current_release_id' "$RELEASE_LEDGER_ROOT/state.json")" \
  'expiration settlement changed the current release'
assert_eq 4 "$(jq -r '.custom_version_high_water' "$RELEASE_LEDGER_ROOT/state.json")" \
  'expiration settlement changed the custom version high-water mark'
assert_eq '' "$(cat "$DISPATCH_DIR/calls")" 'expiration settlement invoked a release executor'

stale_job_id='rollback-stale-terminal'
release_job_init "$stale_job_id"
release_job_update "$stale_job_id" expired expired \
  '{"action":"prepare","operation_kind":"rollback","published":false,"production_changed":false}'
next_job_id='update-after-stale-terminal'
release_job_init "$next_job_id"
release_job_update "$next_job_id" resolving_target queued \
  '{"action":"prepare","operation_kind":"update"}'
jq -n --arg release release-dispatch-fixture --arg operation "$stale_job_id" \
  '{schema_version:1,current_release_id:$release,custom_version_high_water:4,active_operation_id:$operation,updated_at:"2026-07-23T08:00:00Z"}' \
  > "$RELEASE_LEDGER_ROOT/state.json"
printf 'prepare %s\n' "$next_job_id" > "$DISPATCH_DIR/data/release-trigger"
: > "$DISPATCH_DIR/calls"
PATH="$DISPATCH_DIR/bin:$PATH" DISPATCH_CALLS="$DISPATCH_DIR/calls" SUB2API_DATA_DIR="$DISPATCH_DIR/data" \
  SUB2API_PREPARE_SCRIPT="$DISPATCH_DIR/bin/prepare-release.sh" \
  SUB2API_SYNC_PUBLISH_LOCK="$DISPATCH_DIR/release.lock" SUB2API_SYNC_PUBLISH_LOG="$DISPATCH_DIR/release.log" \
  "$ROOT_DIR/deploy/ops/sync-and-publish.sh"
assert_eq "$next_job_id" "$(jq -r '.active_operation_id // empty' "$RELEASE_LEDGER_ROOT/state.json")" \
  'terminal stale ledger owner was not recovered before the next operation'
assert_eq "prepare-release $next_job_id" "$(cat "$DISPATCH_DIR/calls")" \
  'next operation did not run after terminal stale owner recovery'

noop_job_id='update-stale-noop-success'
release_job_init "$noop_job_id"
release_job_update "$noop_job_id" success complete \
  '{"action":"prepare","operation_kind":"update","published":false,"production_changed":false}'
after_noop_job_id='update-after-stale-noop'
release_job_init "$after_noop_job_id"
release_job_update "$after_noop_job_id" resolving_target queued \
  '{"action":"prepare","operation_kind":"update"}'
jq -n --arg release release-dispatch-fixture --arg operation "$noop_job_id" \
  '{schema_version:1,current_release_id:$release,custom_version_high_water:4,active_operation_id:$operation,updated_at:"2026-07-23T08:00:00Z"}' \
  > "$RELEASE_LEDGER_ROOT/state.json"
printf 'prepare %s\n' "$after_noop_job_id" > "$DISPATCH_DIR/data/release-trigger"
: > "$DISPATCH_DIR/calls"
PATH="$DISPATCH_DIR/bin:$PATH" DISPATCH_CALLS="$DISPATCH_DIR/calls" SUB2API_DATA_DIR="$DISPATCH_DIR/data" \
  SUB2API_PREPARE_SCRIPT="$DISPATCH_DIR/bin/prepare-release.sh" \
  SUB2API_SYNC_PUBLISH_LOCK="$DISPATCH_DIR/release.lock" SUB2API_SYNC_PUBLISH_LOG="$DISPATCH_DIR/release.log" \
  "$ROOT_DIR/deploy/ops/sync-and-publish.sh"
assert_eq "$after_noop_job_id" "$(jq -r '.active_operation_id // empty' "$RELEASE_LEDGER_ROOT/state.json")" \
  'stale no-op success owner was not recovered before the next operation'
assert_eq "prepare-release $after_noop_job_id" "$(cat "$DISPATCH_DIR/calls")" \
  'next operation did not run after stale no-op success recovery'

unresolved_job_id='rollback-unresolved-production'
release_job_init "$unresolved_job_id"
release_job_update "$unresolved_job_id" rollback_failed unresolved \
  '{"action":"apply","operation_kind":"rollback","published":false,"production_changed":true}'
jq -n --arg release release-dispatch-fixture --arg operation "$unresolved_job_id" \
  '{schema_version:1,current_release_id:$release,custom_version_high_water:4,active_operation_id:$operation,updated_at:"2026-07-23T08:00:00Z"}' \
  > "$RELEASE_LEDGER_ROOT/state.json"
printf 'apply %s\n' "$unresolved_job_id" > "$DISPATCH_DIR/data/release-trigger"
set +e
PATH="$DISPATCH_DIR/bin:$PATH" DISPATCH_CALLS="$DISPATCH_DIR/calls" SUB2API_DATA_DIR="$DISPATCH_DIR/data" \
  SUB2API_APPLY_ROLLBACK_SCRIPT="$DISPATCH_DIR/bin/apply-rollback.sh" \
  SUB2API_SYNC_PUBLISH_LOCK="$DISPATCH_DIR/release.lock" SUB2API_SYNC_PUBLISH_LOG="$DISPATCH_DIR/release.log" \
  "$ROOT_DIR/deploy/ops/sync-and-publish.sh" >/dev/null 2>&1
unresolved_code=$?
set -e
[[ "$unresolved_code" -ne 0 ]] || fail 'unresolved changed-production terminal trigger was accepted'
assert_eq "$unresolved_job_id" "$(jq -r '.active_operation_id // empty' "$RELEASE_LEDGER_ROOT/state.json")" \
  'unresolved changed-production terminal cleared the active ledger owner'

if [[ "${DISPATCH_ONLY:-0}" == 1 ]]; then
  printf 'release-dispatch=PASS\n'
  exit 0
fi

bash "$ROOT_DIR/deploy/ops/tests/test-sync-upstream-behind.sh"
bash "$ROOT_DIR/deploy/ops/tests/test-release-ledger.sh"
bash "$ROOT_DIR/deploy/ops/tests/test-release-common-compose.sh"
bash "$ROOT_DIR/deploy/ops/tests/test-prepare-release-ledger.sh"

cat > "$TMP_DIR/checks-success.json" <<'JSON'
{
  "check_runs": [
    {"name":"backend","status":"completed","conclusion":"success","html_url":"https://github.example/backend"},
    {"name":"golangci","status":"completed","conclusion":"success","html_url":"https://github.example/golangci"},
    {"name":"frontend","status":"completed","conclusion":"success","html_url":"https://github.example/frontend"},
    {"name":"extensions","status":"completed","conclusion":"success","html_url":"https://github.example/extensions"},
    {"name":"deployment","status":"completed","conclusion":"success","html_url":"https://github.example/deployment"},
    {"name":"metadata","status":"completed","conclusion":"success","html_url":"https://github.example/metadata"},
    {"name":"images","status":"completed","conclusion":"success","html_url":"https://github.example/images"}
  ]
}
JSON
cat > "$TMP_DIR/checks-failed.json" <<'JSON'
{
  "check_runs": [
    {"name":"backend","status":"completed","conclusion":"failure","html_url":"https://github.example/backend"},
    {"name":"golangci","status":"completed","conclusion":"success","html_url":"https://github.example/golangci"},
    {"name":"frontend","status":"completed","conclusion":"success","html_url":"https://github.example/frontend"},
    {"name":"extensions","status":"completed","conclusion":"success","html_url":"https://github.example/extensions"},
    {"name":"deployment","status":"completed","conclusion":"success","html_url":"https://github.example/deployment"},
    {"name":"metadata","status":"completed","conclusion":"success","html_url":"https://github.example/metadata"},
    {"name":"images","status":"completed","conclusion":"skipped","html_url":"https://github.example/images"}
  ]
}
JSON

checks_output="$(SUB2API_CHECKS_JSON_FILE="$TMP_DIR/checks-success.json" "$ROOT_DIR/deploy/ops/wait-for-actions.sh" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa)"
[[ "$checks_output" == *'workflow_url=https://github.example/images'* ]] || fail 'successful checks did not return workflow evidence'
if SUB2API_CHECKS_JSON_FILE="$TMP_DIR/checks-failed.json" "$ROOT_DIR/deploy/ops/wait-for-actions.sh" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa >/dev/null 2>&1; then
  fail 'failed Actions checks were accepted'
fi

cat > "$TMP_DIR/docker" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
args="$*"
image=''
for argument in "$@"; do
  case "$argument" in
    ghcr.io/*) image="$argument" ;;
  esac
done
[[ -n "$image" ]]
if [[ "$args" == 'buildx imagetools inspect '* || "$args" == 'pull '* ]]; then
  [[ -n "${DOCKER_CONFIG:-}" && -r "$DOCKER_CONFIG/config.json" ]]
  jq -e '.auths == {}' "$DOCKER_CONFIG/config.json" >/dev/null
fi
if [[ "$image" == *sub2api-custom* ]]; then
  digest="sha256:$(printf '1%.0s' {1..64})"
  repository='ghcr.io/listencodes/sub2api-custom'
else
  digest="sha256:$(printf '2%.0s' {1..64})"
  repository='ghcr.io/listencodes/sub2api-extensions'
fi
case "$args" in
  'buildx imagetools inspect '*)
    printf 'Name: %s\nMediaType: application/vnd.oci.image.index.v1+json\nDigest: %s\nPlatform: linux/amd64\n' "$image" "$digest"
    ;;
  'pull '*) ;;
  *'.Config.Labels'*)
    jq -n \
      --arg revision "${FAKE_IMAGE_REVISION:-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}" \
      --arg version '0.1.158' \
      '{"org.opencontainers.image.revision":$revision,"org.opencontainers.image.version":$version,"org.opencontainers.image.source":"https://github.com/ListenCodes/sub2api"}'
    ;;
  *'.Architecture'*) printf 'amd64\n' ;;
  *'.RepoDigests'*) jq -n --arg ref "$repository@$digest" '[$ref]' ;;
  *) printf 'unexpected fake docker command: %s\n' "$args" >&2; exit 2 ;;
esac
SH
chmod +x "$TMP_DIR/docker"

image_output="$(PATH="$TMP_DIR:$PATH" "$ROOT_DIR/deploy/ops/verify-release-images.sh" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 0.1.158)"
[[ "$image_output" == *"main_digest=sha256:$(printf '1%.0s' {1..64})"* ]] || fail 'main image digest was not verified'
[[ "$image_output" == *"extensions_digest=sha256:$(printf '2%.0s' {1..64})"* ]] || fail 'extensions image digest was not verified'
if PATH="$TMP_DIR:$PATH" FAKE_IMAGE_REVISION=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
  "$ROOT_DIR/deploy/ops/verify-release-images.sh" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 0.1.158 >/dev/null 2>&1; then
  fail 'image with the wrong revision label was accepted'
fi

PROMOTE_SEED="$TMP_DIR/promote-seed"
PROMOTE_REMOTE="$TMP_DIR/promote-origin.git"
PROMOTE_REPO="$TMP_DIR/promote-repo"
git init -q -b custom-release "$PROMOTE_SEED"
git -C "$PROMOTE_SEED" config user.name 'Release Fixture'
git -C "$PROMOTE_SEED" config user.email 'release-fixture@example.com'
printf 'base\n' > "$PROMOTE_SEED/release.txt"
git -C "$PROMOTE_SEED" add release.txt
git -C "$PROMOTE_SEED" commit -q -m production
promote_local="$(git -C "$PROMOTE_SEED" rev-parse HEAD)"
printf 'approved base\n' >> "$PROMOTE_SEED/release.txt"
git -C "$PROMOTE_SEED" commit -q -am base
promote_base="$(git -C "$PROMOTE_SEED" rev-parse HEAD)"
printf 'target\n' >> "$PROMOTE_SEED/release.txt"
git -C "$PROMOTE_SEED" commit -q -am target
promote_target="$(git -C "$PROMOTE_SEED" rev-parse HEAD)"
git -C "$PROMOTE_SEED" branch integration/release-v0.1.159-fixture "$promote_target"
git -C "$PROMOTE_SEED" switch -q --detach "$promote_target"
git -C "$PROMOTE_SEED" branch -f custom-release "$promote_base"
git clone -q --bare "$PROMOTE_SEED" "$PROMOTE_REMOTE"
git --git-dir="$PROMOTE_REMOTE" symbolic-ref HEAD refs/heads/custom-release
git clone -q "$PROMOTE_REMOTE" "$PROMOTE_REPO"
git -C "$PROMOTE_REPO" config user.name 'Release Fixture'
git -C "$PROMOTE_REPO" config user.email 'release-fixture@example.com'
git -C "$PROMOTE_SEED" push -q "$PROMOTE_REMOTE" "integration/release-v0.1.159-fixture:integration/release-v0.1.159-fixture"
git -C "$PROMOTE_REPO" switch -q --detach "$promote_local"
git -C "$PROMOTE_REPO" branch -f custom-release "$promote_local"
git -C "$PROMOTE_REPO" switch -q custom-release

SUB2API_REPO="$PROMOTE_REPO" SUB2API_PROMOTE_LOG="$TMP_DIR/promote.log" \
  "$ROOT_DIR/deploy/ops/promote-release.sh" \
  "$promote_base" "$promote_target" integration/release-v0.1.159-fixture >/dev/null
assert_eq "$promote_local" "$(git -C "$PROMOTE_REPO" rev-parse HEAD)" \
  'promotion moved local production source before the publisher backup'
assert_eq "$promote_target" "$(git --git-dir="$PROMOTE_REMOTE" rev-parse refs/heads/custom-release)" \
  'promotion did not advance the remote approved branch'

FAKE_BIN="$TMP_DIR/fake-bin"
mkdir -p "$FAKE_BIN"
cat > "$FAKE_BIN/sync.sh" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
source "$SUB2API_RELEASE_STATE_HELPER"
[[ "${1:-}" == --job-id && -n "${2:-}" ]]
job_id="$2"
case "$PIPELINE_SCENARIO" in
  no-change)
    metadata="$(jq -n --arg base "$TARGET_COMMIT" --arg target "$TARGET_COMMIT" '{base_commit:$base,target_commit:$target,release_tag:"v0.1.158",release_commit:"26abd19a2812edba02bbef93c3e2a620141cc257",release_published_at:"2026-07-16T12:37:06Z",integration_branch:""}')"
    ;;
  custom|docs-only|publisher-failure)
    metadata="$(jq -n --arg base "$TARGET_COMMIT" --arg target "$TARGET_COMMIT" '{base_commit:$base,target_commit:$target,release_tag:"v0.1.158",release_commit:"26abd19a2812edba02bbef93c3e2a620141cc257",release_published_at:"2026-07-16T12:37:06Z",integration_branch:""}')"
    ;;
  release|base-race)
    metadata="$(jq -n --arg base "$BASE_COMMIT" --arg target "$TARGET_COMMIT" '{base_commit:$base,target_commit:$target,release_tag:"v0.1.159",release_commit:"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",release_published_at:"2026-07-17T00:00:00Z",integration_branch:"integration/release-v0.1.159-fixture"}')"
    ;;
  *) exit 2 ;;
esac
  release_job_update "$job_id" resolving_target 'Waiting for Actions' "$metadata"
SH
cat > "$FAKE_BIN/wait.sh" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'wait %s\n' "$1" >> "$PIPELINE_CALLS"
printf 'workflow_url=https://github.example/actions/%s\n' "$1"
SH
cat > "$FAKE_BIN/scope.sh" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "$PIPELINE_SCENARIO" == docs-only ]]; then
  printf 'docs_only=true\n'
else
  printf 'docs_only=false\n'
fi
SH
cat > "$FAKE_BIN/verify.sh" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'verify %s %s\n' "$1" "$2" >> "$PIPELINE_CALLS"
printf 'main_digest=sha256:%064d\nextensions_digest=sha256:%064d\n' 1 2
SH
cat > "$FAKE_BIN/promote.sh" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'promote %s %s %s\n' "$1" "$2" "$3" >> "$PIPELINE_CALLS"
[[ "$PIPELINE_SCENARIO" != base-race ]]
SH
cat > "$FAKE_BIN/publish.sh" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'publish %s\n' "$*" >> "$PIPELINE_CALLS"
source "$SUB2API_RELEASE_STATE_HELPER"
if [[ "$PIPELINE_SCENARIO" == publisher-failure ]]; then
  release_job_update "$SUB2API_JOB_ID" failed 'Fixture deployment failed; previous pair restored' \
    '{"error_code":"MAIN_HEALTH_FAILED","artifact_path":"/fixture/backup","rollback":{"attempted":true,"succeeded":true,"message":"restored"}}'
  exit 1
fi
release_job_update "$SUB2API_JOB_ID" success 'Published fixture release' "$(jq -n --arg commit "$TARGET_COMMIT" '{published:true,published_commit:$commit,production_changed:true}')"
SH
cat > "$FAKE_BIN/prepare.sh" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
source "$SUB2API_RELEASE_STATE_HELPER"
[[ "${1:-}" == --job-id && -n "${2:-}" ]]
job_id="$2"
fail_prepare() {
  release_job_update "$job_id" failed "$1" '{"error_code":"FIXTURE_PREPARE_FAILED","production_changed":false}'
  exit 1
}
case "$PIPELINE_SCENARIO" in
  no-change)
    release_job_update "$job_id" success 'Already current' '{"published":false,"production_changed":false}'
    ;;
  docs-only)
    release_job_update "$job_id" success 'Documentation-only' '{"docs_only":true,"published":false,"production_changed":false}'
    ;;
  custom|release|base-race|publisher-failure)
    "$SUB2API_WAIT_ACTIONS_SCRIPT" "$TARGET_COMMIT" >> "$PIPELINE_LOG" 2>&1 || fail_prepare 'Actions failed'
    "$SUB2API_VERIFY_IMAGES_SCRIPT" "$TARGET_COMMIT" 0.1.158 >> "$PIPELINE_LOG" 2>&1 || fail_prepare 'Images failed'
    if [[ "$PIPELINE_SCENARIO" == release || "$PIPELINE_SCENARIO" == base-race ]]; then
      "$SUB2API_PROMOTE_SCRIPT" "$BASE_COMMIT" "$TARGET_COMMIT" integration/fixture >> "$PIPELINE_LOG" 2>&1 || fail_prepare 'Promotion failed'
    fi
    "$SUB2API_PUBLISH_SCRIPT" --commit "$TARGET_COMMIT" --main-digest sha256:$(printf '1%.0s' {1..64}) --extensions-digest sha256:$(printf '2%.0s' {1..64}) || exit $?
    ;;
  *) fail_prepare 'Unknown fixture scenario' ;;
esac
SH
cat > "$FAKE_BIN/flock" <<'SH'
#!/usr/bin/env sh
exit 0
SH
chmod +x "$FAKE_BIN"/*.sh

run_scenario() {
  local scenario="$1"
  local production_commit="$2"
  local base_commit="$3"
  local target_commit="$4"
  local scenario_dir="$TMP_DIR/scenario-$scenario"
  local job_id="update-$scenario"
  local run_exit
  mkdir -p "$scenario_dir/data"
  : > "$scenario_dir/calls"
  SUB2API_DATA_DIR="$scenario_dir/data"
  RELEASE_JOBS_DIR="$SUB2API_DATA_DIR/release-ledger/operations"
  CURRENT_RELEASE_JOB_FILE="$SUB2API_DATA_DIR/release-current-job-id"
  PRODUCTION_RELEASE_STATE_FILE="$SUB2API_DATA_DIR/release-state.json"
  mkdir -p "$SUB2API_DATA_DIR/release-ledger"
  jq -n '{schema_version:1,current_release_id:"release-fixture",custom_version_high_water:0,active_operation_id:null,updated_at:"2026-07-16T12:00:00Z"}' \
    > "$SUB2API_DATA_DIR/release-ledger/state.json"
  release_job_init "$job_id"
  release_job_update "$job_id" resolving_target 'Fixture prepare queued' '{"action":"prepare"}'
  release_production_state_write "$(jq -n \
    --arg production_commit "$production_commit" \
    '{production_commit:$production_commit,stable_release_tag:"v0.1.158",stable_release_commit:"26abd19a2812edba02bbef93c3e2a620141cc257",main_digest:("sha256:"+("0"*64)),extensions_digest:("sha256:"+("1"*64)),published_at:"2026-07-16T12:00:00Z",backup_dir:"/root/backups/sub2api/fixture"}')"

  set +e
  PIPELINE_SCENARIO="$scenario" \
  PATH="$FAKE_BIN:$PATH" \
  PIPELINE_CALLS="$scenario_dir/calls" \
  BASE_COMMIT="$base_commit" \
  TARGET_COMMIT="$target_commit" \
  SUB2API_DATA_DIR="$scenario_dir/data" \
  SUB2API_JOB_ID="$job_id" \
  SUB2API_RELEASE_STATE_HELPER="$ROOT_DIR/deploy/ops/release-state.sh" \
  SUB2API_SYNC_SCRIPT="$FAKE_BIN/sync.sh" \
  SUB2API_PREPARE_SCRIPT="$FAKE_BIN/prepare.sh" \
  SUB2API_PUBLISH_SCRIPT="$FAKE_BIN/publish.sh" \
  SUB2API_WAIT_ACTIONS_SCRIPT="$FAKE_BIN/wait.sh" \
  SUB2API_VERIFY_IMAGES_SCRIPT="$FAKE_BIN/verify.sh" \
  SUB2API_PROMOTE_SCRIPT="$FAKE_BIN/promote.sh" \
  PIPELINE_LOG="$scenario_dir/release.log" \
  SUB2API_SCOPE_SCRIPT="$FAKE_BIN/scope.sh" \
  SUB2API_WAIT_ACTIONS_SCRIPT="$FAKE_BIN/wait.sh" \
  SUB2API_VERIFY_IMAGES_SCRIPT="$FAKE_BIN/verify.sh" \
  SUB2API_PROMOTE_SCRIPT="$FAKE_BIN/promote.sh" \
  SUB2API_PUBLISH_SCRIPT="$FAKE_BIN/publish.sh" \
  SUB2API_SYNC_PUBLISH_LOCK="$scenario_dir/release.lock" \
  SUB2API_SYNC_PUBLISH_LOG="$scenario_dir/release.log" \
  "$ROOT_DIR/deploy/ops/sync-and-publish.sh"
  run_exit=$?
  set -e

  SCENARIO_DIR="$scenario_dir"
  return "$run_exit"
}

sha_a=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
sha_b=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
sha_c=cccccccccccccccccccccccccccccccccccccccc

run_scenario no-change "$sha_a" "$sha_a" "$sha_a"
assert_eq success "$(jq -r '.status' "$SCENARIO_DIR/data/release-ledger/operations/update-no-change.json")" 'no-change job did not finish successfully'
assert_eq '' "$(cat "$SCENARIO_DIR/calls")" 'no-change job called downstream publication tools'

run_scenario custom "$sha_b" "$sha_a" "$sha_a"
grep -q '^wait ' "$SCENARIO_DIR/calls" || fail 'custom commit did not wait for Actions'
grep -q '^verify ' "$SCENARIO_DIR/calls" || fail 'custom commit did not verify images'
grep -q '^publish ' "$SCENARIO_DIR/calls" || fail 'custom commit was not published'
if grep -q '^promote ' "$SCENARIO_DIR/calls"; then fail 'custom commit path unexpectedly promoted a Release branch'; fi

run_scenario docs-only "$sha_b" "$sha_b" "$sha_c"
assert_eq success "$(jq -r '.status' "$SCENARIO_DIR/data/release-ledger/operations/update-docs-only.json")" 'docs-only job did not finish successfully'
assert_eq true "$(jq -r '.docs_only' "$SCENARIO_DIR/data/release-ledger/operations/update-docs-only.json")" 'docs-only scope was not persisted'
assert_eq false "$(jq -r '.published' "$SCENARIO_DIR/data/release-ledger/operations/update-docs-only.json")" 'docs-only job was marked published'
assert_eq false "$(jq -r '.production_changed' "$SCENARIO_DIR/data/release-ledger/operations/update-docs-only.json")" 'docs-only job changed production'
assert_eq '' "$(cat "$SCENARIO_DIR/calls")" 'docs-only job called Actions, image verification, or publication tools'

run_scenario release "$sha_b" "$sha_b" "$sha_c"
release_calls="$(cat "$SCENARIO_DIR/calls")"
[[ "$release_calls" == *$'wait '*$'\nverify '*$'\npromote '*$'\npublish '* ]] || fail 'Release scenario used the wrong gate order'

if run_scenario base-race "$sha_b" "$sha_b" "$sha_c" >/dev/null 2>&1; then
  fail 'base race scenario unexpectedly succeeded'
fi
assert_eq failed "$(jq -r '.status' "$SCENARIO_DIR/data/release-ledger/operations/update-base-race.json")" 'base race did not persist failure'
if grep -q '^publish ' "$SCENARIO_DIR/calls"; then fail 'base race called the publisher'; fi

if run_scenario publisher-failure "$sha_b" "$sha_a" "$sha_a" >/dev/null 2>&1; then
  fail 'publisher failure scenario unexpectedly succeeded'
fi
publisher_failure_job="$SCENARIO_DIR/data/release-ledger/operations/update-publisher-failure.json"
assert_eq MAIN_HEALTH_FAILED "$(jq -r '.error_code' "$publisher_failure_job")" 'orchestrator overwrote the publisher error code'
assert_eq true "$(jq -r '.rollback.succeeded' "$publisher_failure_job")" 'orchestrator lost publisher rollback evidence'
assert_eq '/fixture/backup' "$(jq -r '.artifact_path' "$publisher_failure_job")" 'orchestrator lost publisher backup evidence'

if "$ROOT_DIR/deploy/ops/publish-custom.sh" >/dev/null 2>&1; then
  fail 'deprecated publisher unexpectedly remained an executable release entry point'
fi

printf 'release pipeline fixture tests: PASS\n'
