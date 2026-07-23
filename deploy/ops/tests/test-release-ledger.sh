#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

BASELINE_COMMIT=aa2d24106cab0a03785330d8e0ff4e02b0474a0e
STABLE_COMMIT=d0bdd7e771636a8d315f542cafd39484f39bd60c
MAIN_DIGEST="sha256:$(printf '1%.0s' {1..64})"
EXTENSIONS_DIGEST="sha256:$(printf '2%.0s' {1..64})"

fail() {
  printf 'release ledger test failed: %s\n' "$1" >&2
  exit 1
}

assert_eq() {
  [[ "$1" == "$2" ]] || fail "$3 (expected=$1 actual=$2)"
}

seed_fixture() {
  local name="$1" root="$TMP_DIR/$1"
  mkdir -p "$root/data" "$root/repo/deploy" "$root/backups/bootstrap/target" "$root/bin"
  printf 'services:\n  sub2api:\n    image: ${SUB2API_IMAGE}\n' > "$root/repo/deploy/docker-compose.yml"
  printf 'services:\n  extensions-self:\n    image: ${EXTENSIONS_SELF_IMAGE}\n' > "$root/repo/deploy/docker-compose.custom.yml"
  printf 'SUB2API_IMAGE=ghcr.io/listencodes/sub2api-custom@%s\nEXTENSIONS_SELF_IMAGE=ghcr.io/listencodes/sub2api-extensions@%s\n' "$MAIN_DIGEST" "$EXTENSIONS_DIGEST" > "$root/repo/deploy/.env"
  cp "$root/repo/deploy/docker-compose.yml" "$root/backups/bootstrap/target/"
  cp "$root/repo/deploy/docker-compose.custom.yml" "$root/backups/bootstrap/target/"
  cp "$root/repo/deploy/.env" "$root/backups/bootstrap/target/.env"
  printf '{"services":{"sub2api":{},"extensions-self":{}}}\n' > "$root/backups/bootstrap/target/rendered-compose.json"
  (cd "$root/backups/bootstrap/target" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
  cp "$root/repo/deploy/docker-compose.yml" "$root/backups/bootstrap/docker-compose.yml"
  cp "$root/repo/deploy/docker-compose.custom.yml" "$root/backups/bootstrap/docker-compose.custom.yml"
  cp "$root/repo/deploy/.env" "$root/backups/bootstrap/.env"
  for evidence in release-state.json container-metadata.json image-metadata.txt rollback-tags.txt \
    sub2api_db.dump sub2api_db.list risk_control_db.dump risk_control_db.list \
    docker-containers.txt docker-images.txt; do
    printf 'fixture %s\n' "$evidence" > "$root/backups/bootstrap/$evidence"
  done
  printf '/etc/nginx/sites-available/sub.ailisten.top\n' > "$root/backups/bootstrap/nginx-vhost.path"
  printf '/etc/nginx/ssl/ailisten.top.crt\n' > "$root/backups/bootstrap/origin-cert.path"
  printf '/etc/nginx/ssl/ailisten.top.key\n' > "$root/backups/bootstrap/origin-key.path"
  printf 'fixture nginx\n' > "$root/backups/bootstrap/sub.ailisten.top"
  printf 'fixture cert\n' > "$root/backups/bootstrap/ailisten.top.crt"
  printf 'fixture key\n' > "$root/backups/bootstrap/ailisten.top.key"
  (cd "$root/backups/bootstrap" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
  jq -n \
    --arg production_commit "$BASELINE_COMMIT" \
    --arg stable_release_tag v0.1.163 \
    --arg stable_release_commit "$STABLE_COMMIT" \
    --arg main_digest "$MAIN_DIGEST" \
    --arg extensions_digest "$EXTENSIONS_DIGEST" \
    --arg published_at '2026-07-23T00:00:00Z' \
    --arg backup_dir "$root/backups/bootstrap" \
    '{production_commit:$production_commit,stable_release_tag:$stable_release_tag,stable_release_commit:$stable_release_commit,main_digest:$main_digest,extensions_digest:$extensions_digest,published_at:$published_at,backup_dir:$backup_dir}' \
    > "$root/data/release-state.json"
  cat > "$root/bin/git" <<EOF
#!/usr/bin/env bash
[[ "\$*" == *'rev-parse HEAD'* ]] && { printf '%s\n' '$BASELINE_COMMIT'; exit 0; }
[[ "\$*" == *'status --porcelain --untracked-files=all'* ]] && { [[ ! -e '$root/repo/.status-error' ]] || exit 2; [[ ! -e '$root/repo/.dirty' ]] || printf ' M dirty\n'; exit 0; }
exit 1
EOF
  cat > "$root/bin/docker" <<EOF
#!/usr/bin/env bash
if [[ "\$1 \$2" == 'inspect --format' ]]; then
  case "\${4:-}" in
    sub2api) printf '%s\n' 'ghcr.io/listencodes/sub2api-custom@$MAIN_DIGEST' ;;
    extensions-self) printf '%s\n' 'ghcr.io/listencodes/sub2api-extensions@$EXTENSIONS_DIGEST' ;;
    *) exit 1 ;;
  esac
  exit 0
fi
if [[ "\$1 \$2" == 'compose --project-name' || "\$1" == compose ]]; then
  printf '%s\n' '{"services":{"sub2api":{},"extensions-self":{}}}'
  exit 0
fi
exit 1
EOF
  cat > "$root/bin/flock" <<'EOF'
#!/usr/bin/env bash
[[ -z "${FAKE_FLOCK_LOG:-}" ]] || printf 'flock %s\n' "$*" >> "$FAKE_FLOCK_LOG"
exit 0
EOF
  chmod +x "$root/bin/git" "$root/bin/docker" "$root/bin/flock"
  printf '%s\n' "$root"
}

run_migration() {
  local root="$1"
  PATH="$root/bin:$PATH" \
    SUB2API_DATA_DIR="$root/data" \
    SUB2API_REPO="$root/repo" \
    SUB2API_ENV_FILE="$root/repo/deploy/.env" \
    SUB2API_COMPOSE_BASE="$root/repo/deploy/docker-compose.yml" \
    SUB2API_COMPOSE_CUSTOM="$root/repo/deploy/docker-compose.custom.yml" \
    SUB2API_RELEASE_BACKUP_ROOT="$root/backups" \
    SUB2API_RELEASE_LEDGER_LOCK_FILE="$root/data/release-ledger.lock" \
    "$ROOT_DIR/deploy/ops/migrate-release-ledger.sh" \
      --expected-production-commit "$BASELINE_COMMIT" \
      --official-version v0.1.163 \
      --custom-version v1.0.0
}

root="$(seed_fixture success)"
run_migration "$root"
state="$root/data/release-ledger/state.json"
[[ -r "$state" ]] || fail 'migration did not create state.json'
assert_eq 0 "$(jq -r '.custom_version_high_water' "$state")" 'bootstrap high-water mismatch'
release_id="$(jq -r '.current_release_id' "$state")"
record="$root/data/release-ledger/releases/$release_id.json"
[[ -r "$record" ]] || fail 'bootstrap release record is missing'
assert_eq bootstrap "$(jq -r '.source_kind' "$record")" 'bootstrap source kind mismatch'
assert_eq v0.1.163 "$(jq -r '.official_version' "$record")" 'official version mismatch'
assert_eq v1.0.0 "$(jq -r '.custom_version' "$record")" 'custom version mismatch'
assert_eq "$release_id" "$(jq -r '.release_id' "$root/data/release-state.json")" 'compatibility projection missing release id'
before_hash="$(sha256sum "$state" "$record" "$root/data/release-state.json")"
run_migration "$root"
assert_eq "$before_hash" "$(sha256sum "$state" "$record" "$root/data/release-state.json")" 'idempotent migration changed ledger files'

default_job_path="$(SUB2API_DATA_DIR="$root/data" SUB2API_RELEASE_JOBS_DIR="$root/data/release-jobs" bash -c 'source "$1"; release_job_path update-path-check' _ "$ROOT_DIR/deploy/ops/release-state.sh")"
assert_eq "$root/data/release-ledger/operations/update-path-check.json" "$default_job_path" 'new operation path is outside the ledger'
mkdir -p "$root/data/release-jobs"
printf '{"job_id":"update-legacy","status":"prepared"}\n' > "$root/data/release-jobs/update-legacy.json"
if legacy_output="$(SUB2API_DATA_DIR="$root/data" bash -c 'source "$1"; release_job_read update-legacy' _ "$ROOT_DIR/deploy/ops/release-state.sh" 2>&1)"; then
  fail 'legacy release-jobs record was read as a new operation'
fi
[[ "$legacy_output" == *LEGACY_SINGLE_PHASE_UNSUPPORTED* ]] || fail 'legacy release-jobs refusal omitted its error code'

mkdir -p "$root/outside"
escape_record="$(jq --arg backup "$root/backups/../outside" '.backup_dir=$backup' "$record")"
printf '%s\n' "$escape_record" > "$root/escape-record.json"
if SUB2API_DATA_DIR="$root/data" SUB2API_RELEASE_BACKUP_ROOT="$root/backups" \
  bash -c 'source "$1"; ledger_validate_release "$2"' _ \
    "$ROOT_DIR/deploy/ops/release-ledger.sh" "$root/escape-record.json"; then
  fail 'release record backup path escaped its canonical root'
fi

recovery_id=release-recovery-20260724T000000Z-ccccccccc
recovery_operation=update-recovery-ledger-test
recovery_record="$(jq --arg release_id "$recovery_id" --arg operation_id "$recovery_operation" \
  --arg custom_version v1.0.1 --arg custom_commit "$(printf 'd%.0s' {1..40})" \
  '.release_id=$release_id | .operation_id=$operation_id | .custom_version=$custom_version
   | .custom_version_sequence=1 | .custom_commit=$custom_commit | .source_kind="custom"' "$record")"
printf '%s\n' "$recovery_record" > "$root/data/release-ledger/releases/$recovery_id.json"
mkdir -p "$root/data/release-ledger/operations"
jq -n --arg job_id "$recovery_operation" --arg base "$release_id" --arg target "$recovery_id" \
  --arg official v0.1.163 --arg custom v1.0.1 --arg target_commit "$(printf 'd%.0s' {1..40})" \
  --arg stable_commit "$STABLE_COMMIT" --arg main_digest "$MAIN_DIGEST" --arg extensions_digest "$EXTENSIONS_DIGEST" \
  '{job_id:$job_id,operation_kind:"update",action:"apply",status:"health_checking",base_release_id:$base,target_release_id:$target,
    base_custom_high_water:0,update_kind:"custom",proposed_custom_sequence:1,advances_custom_version:true,
    target_official_version:$official,target_custom_version:$custom,target_commit:$target_commit,
    stable_release_tag:$official,stable_release_commit:$stable_commit,main_digest:$main_digest,extensions_digest:$extensions_digest}' \
  > "$root/data/release-ledger/operations/$recovery_operation.json"
jq --arg operation "$recovery_operation" '.active_operation_id=$operation' "$state" > "$state.tmp"
mv "$state.tmp" "$state"
recovery_projection="$(SUB2API_DATA_DIR="$root/data" SUB2API_RELEASE_BACKUP_ROOT="$root/backups" \
  bash -c 'source "$1"; ledger_projection_for_release "$2"' _ "$ROOT_DIR/deploy/ops/release-ledger.sh" "$recovery_record")"
printf '%s\n' "$recovery_projection" > "$root/data/release-state.json"
PATH="$root/bin:$PATH" SUB2API_DATA_DIR="$root/data" SUB2API_RELEASE_BACKUP_ROOT="$root/backups" \
  SUB2API_RELEASE_LEDGER_LOCK_FILE="$root/data/release-ledger.lock" \
  bash -c 'source "$1"; ledger_recover_or_refuse "$2" "$3" 1 "$4"' _ \
    "$ROOT_DIR/deploy/ops/release-ledger.sh" "$recovery_record" "$recovery_projection" "$recovery_operation"
assert_eq "$recovery_id" "$(jq -r '.current_release_id' "$state")" 'exact partial commit recovery did not move the current pointer'
assert_eq 1 "$(jq -r '.custom_version_high_water' "$state")" 'exact partial commit recovery did not advance high-water'
assert_eq null "$(jq -r '.active_operation_id' "$state")" 'exact partial commit recovery did not clear the operation'
assert_eq success "$(jq -r '.status' "$root/data/release-ledger/operations/$recovery_operation.json")" 'exact partial commit recovery did not settle the operation'

drift_root="$(seed_fixture existing-ledger-drift)"
run_migration "$drift_root"
drift_state="$drift_root/data/release-ledger/state.json"
drift_record="$drift_root/data/release-ledger/releases/$(jq -r '.current_release_id' "$drift_state").json"
jq --arg digest "sha256:$(printf '8%.0s' {1..64})" '.main_digest=$digest' "$drift_record" > "$drift_record.tmp"
mv "$drift_record.tmp" "$drift_record"
if run_migration "$drift_root" >/dev/null 2>&1; then
  fail 'existing ledger digest drift was accepted as idempotent'
fi

missing_record_root="$(seed_fixture existing-ledger-missing-record)"
run_migration "$missing_record_root"
missing_record_state="$missing_record_root/data/release-ledger/state.json"
missing_record="$missing_record_root/data/release-ledger/releases/$(jq -r '.current_release_id' "$missing_record_state").json"
rm "$missing_record"
if run_migration "$missing_record_root" >/dev/null 2>&1; then
  fail 'existing ledger missing its immutable record was silently repaired'
fi
[[ ! -e "$missing_record" ]] || fail 'existing ledger missing record was recreated'

rollback_root="$(seed_fixture rollback-commit)"
run_migration "$rollback_root"
rollback_state="$rollback_root/data/release-ledger/state.json"
current_id="$(jq -r '.current_release_id' "$rollback_state")"
current_record="$rollback_root/data/release-ledger/releases/$current_id.json"
target_id=release-historical-20260722T000000Z-bbbbbbbbb
target_record="$rollback_root/data/release-ledger/releases/$target_id.json"
jq --arg release_id "$target_id" --arg official v0.1.162 --arg official_commit "$(printf 'b%.0s' {1..40})" \
  --arg custom v1.0.2 --arg custom_commit "$(printf 'c%.0s' {1..40})" \
  '.release_id=$release_id | .official_version=$official | .official_commit=$official_commit
   | .custom_version=$custom | .custom_version_sequence=2 | .custom_commit=$custom_commit' \
  "$current_record" > "$target_record"
jq '.custom_version_high_water=4 | .active_operation_id="rollback-ledger-test"' "$rollback_state" > "$rollback_state.tmp"
mv "$rollback_state.tmp" "$rollback_state"
target_hash_before="$(sha256sum "$target_record")"
release_count_before="$(find "$rollback_root/data/release-ledger/releases" -maxdepth 1 -type f | wc -l | tr -d ' ')"
flock_log="$rollback_root/flock.log"
PATH="$rollback_root/bin:$PATH" FAKE_FLOCK_LOG="$flock_log" \
  SUB2API_DATA_DIR="$rollback_root/data" SUB2API_RELEASE_BACKUP_ROOT="$rollback_root/backups" \
  SUB2API_RELEASE_LEDGER_LOCK_FILE="$rollback_root/data/release-ledger.lock" \
  bash -c 'source "$1"; ledger_commit_rollback "$2" "$3"' _ \
    "$ROOT_DIR/deploy/ops/release-ledger.sh" "$target_id" rollback-ledger-test
assert_eq "$target_hash_before" "$(sha256sum "$target_record")" 'rollback modified its immutable target record'
assert_eq "$release_count_before" "$(find "$rollback_root/data/release-ledger/releases" -maxdepth 1 -type f | wc -l | tr -d ' ')" 'rollback created a release record'
assert_eq "$target_id" "$(jq -r '.current_release_id' "$rollback_state")" 'rollback did not move the current pointer'
assert_eq 4 "$(jq -r '.custom_version_high_water' "$rollback_state")" 'rollback changed the custom high-water'
assert_eq null "$(jq -r '.active_operation_id' "$rollback_state")" 'rollback did not clear the active operation'
assert_eq v0.1.162 "$(jq -r '.official_version' "$rollback_root/data/release-state.json")" 'rollback projection did not restore the official version'
[[ -s "$flock_log" ]] || fail 'rollback commit did not acquire the ledger lock'

official_recovery_id=release-official-20260725T000000Z-eeeeeeeee
official_operation=update-official-recovery-test
official_record="$(jq --arg release_id "$official_recovery_id" --arg operation_id "$official_operation" \
  --arg official v0.1.164 --arg official_commit "$(printf 'e%.0s' {1..40})" \
  '.release_id=$release_id | .operation_id=$operation_id | .official_version=$official
   | .official_commit=$official_commit | .source_kind="official"' "$target_record")"
printf '%s\n' "$official_record" > "$rollback_root/data/release-ledger/releases/$official_recovery_id.json"
mkdir -p "$rollback_root/data/release-ledger/operations"
jq -n --arg job_id "$official_operation" --arg base "$target_id" --arg target "$official_recovery_id" \
  --arg official v0.1.164 --arg custom v1.0.2 --arg target_commit "$(printf 'c%.0s' {1..40})" \
  --arg stable_commit "$(printf 'e%.0s' {1..40})" --arg main_digest "$MAIN_DIGEST" --arg extensions_digest "$EXTENSIONS_DIGEST" \
  '{job_id:$job_id,operation_kind:"update",action:"apply",status:"health_checking",base_release_id:$base,target_release_id:$target,
    base_custom_high_water:4,update_kind:"official",proposed_custom_sequence:2,advances_custom_version:false,
    target_official_version:$official,target_custom_version:$custom,target_commit:$target_commit,
    stable_release_tag:$official,stable_release_commit:$stable_commit,main_digest:$main_digest,extensions_digest:$extensions_digest}' \
  > "$rollback_root/data/release-ledger/operations/$official_operation.json"
jq --arg operation "$official_operation" '.active_operation_id=$operation' "$rollback_state" > "$rollback_state.tmp"
mv "$rollback_state.tmp" "$rollback_state"
official_projection="$(SUB2API_DATA_DIR="$rollback_root/data" SUB2API_RELEASE_BACKUP_ROOT="$rollback_root/backups" \
  bash -c 'source "$1"; ledger_projection_for_release "$2"' _ "$ROOT_DIR/deploy/ops/release-ledger.sh" "$official_record")"
printf '%s\n' "$official_projection" > "$rollback_root/data/release-state.json"
PATH="$rollback_root/bin:$PATH" SUB2API_DATA_DIR="$rollback_root/data" SUB2API_RELEASE_BACKUP_ROOT="$rollback_root/backups" \
  SUB2API_RELEASE_LEDGER_LOCK_FILE="$rollback_root/data/release-ledger.lock" \
  bash -c 'source "$1"; ledger_recover_or_refuse "$2" "$3" 4 "$4"' _ \
    "$ROOT_DIR/deploy/ops/release-ledger.sh" "$official_record" "$official_projection" "$official_operation"
assert_eq "$official_recovery_id" "$(jq -r '.current_release_id' "$rollback_state")" 'official-only recovery did not move the current pointer'
assert_eq 4 "$(jq -r '.custom_version_high_water' "$rollback_state")" 'official-only recovery changed high-water'
assert_eq v1.0.2 "$(jq -r '.custom_version' "$rollback_root/data/release-state.json")" 'official-only recovery changed the displayed custom version'
assert_eq success "$(jq -r '.status' "$rollback_root/data/release-ledger/operations/$official_operation.json")" 'official-only recovery did not settle the operation'
official_operation_path="$rollback_root/data/release-ledger/operations/$official_operation.json"
cp "$official_operation_path" "$official_operation_path.valid"
for field in target_official_version target_custom_version target_commit stable_release_tag stable_release_commit main_digest extensions_digest; do
  jq --arg field "$field" '.[$field]="tampered"' "$official_operation_path.valid" > "$official_operation_path"
  if PATH="$rollback_root/bin:$PATH" SUB2API_DATA_DIR="$rollback_root/data" SUB2API_RELEASE_BACKUP_ROOT="$rollback_root/backups" \
    SUB2API_RELEASE_LEDGER_LOCK_FILE="$rollback_root/data/release-ledger.lock" \
    bash -c 'source "$1"; ledger_recover_or_refuse "$2" "$3" 4 "$4"' _ \
      "$ROOT_DIR/deploy/ops/release-ledger.sh" "$official_record" "$official_projection" "$official_operation"; then
    fail "recovery accepted tampered operation field $field"
  fi
done
mv "$official_operation_path.valid" "$official_operation_path"

for failpoint in after_release_record after_projection; do
  root="$(seed_fixture "$failpoint")"
  if SUB2API_LEDGER_MIGRATION_FAILPOINT="$failpoint" run_migration "$root" >/dev/null 2>&1; then
    fail "$failpoint did not interrupt migration"
  fi
  run_migration "$root"
  jq -e '.schema_version == 1 and .custom_version_high_water == 0' "$root/data/release-ledger/state.json" >/dev/null \
    || fail "$failpoint recovery did not finish the exact migration"
done

for mutation in wrong_commit wrong_digest wrong_stable wrong_compose wrong_env dirty_source status_failure missing_backup incomplete_backup uncovered_backup absolute_manifest parent_manifest duplicate_manifest active_job dangling_job; do
  root="$(seed_fixture "$mutation")"
  case "$mutation" in
    wrong_commit) sed -i "s/$BASELINE_COMMIT/$(printf 'f%.0s' {1..40})/" "$root/data/release-state.json" ;;
    wrong_digest) sed -i "s/$MAIN_DIGEST/sha256:$(printf '9%.0s' {1..64})/" "$root/data/release-state.json" ;;
    wrong_stable) sed -i 's/v0.1.163/v0.1.162/' "$root/data/release-state.json" ;;
    wrong_compose) printf '\n# drift\n' >> "$root/repo/deploy/docker-compose.yml" ;;
    wrong_env) printf '\nDRIFT=1\n' >> "$root/repo/deploy/.env" ;;
    dirty_source) touch "$root/repo/.dirty" ;;
    status_failure) touch "$root/repo/.status-error" ;;
    missing_backup) rm "$root/backups/bootstrap/SHA256SUMS" ;;
    incomplete_backup) rm "$root/backups/bootstrap/risk_control_db.list" ;;
    uncovered_backup) (cd "$root/backups/bootstrap" && find target -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS) ;;
    absolute_manifest)
      (cd "$root/backups/bootstrap" && find "$PWD" -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > "$root/absolute.SHA256SUMS")
      mv "$root/absolute.SHA256SUMS" "$root/backups/bootstrap/SHA256SUMS"
      ;;
    parent_manifest)
      (cd "$root/backups/bootstrap" && sed 's#\./#../bootstrap/#' SHA256SUMS > "$root/parent.SHA256SUMS")
      mv "$root/parent.SHA256SUMS" "$root/backups/bootstrap/SHA256SUMS"
      ;;
    duplicate_manifest) head -n 1 "$root/backups/bootstrap/SHA256SUMS" >> "$root/backups/bootstrap/SHA256SUMS" ;;
    active_job) mkdir -p "$root/data/release-jobs"; printf 'update-active\n' > "$root/data/release-current-job-id"; printf '{"status":"prepared"}\n' > "$root/data/release-jobs/update-active.json" ;;
    dangling_job) printf 'update-missing\n' > "$root/data/release-current-job-id" ;;
  esac
  if run_migration "$root" >/dev/null 2>&1; then
    fail "$mutation was accepted"
  fi
  [[ ! -e "$root/data/release-ledger/state.json" ]] || fail "$mutation created a partial state.json"
  [[ ! -e "$root/data/release-ledger" ]] || fail "$mutation created a partial ledger directory"
done

printf 'release-ledger=PASS\n'
