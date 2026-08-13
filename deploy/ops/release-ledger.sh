#!/usr/bin/env bash

SUB2API_DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
RELEASE_LEDGER_ROOT="${SUB2API_RELEASE_LEDGER_ROOT:-$SUB2API_DATA_DIR/release-ledger}"
RELEASE_BACKUP_ROOT="${SUB2API_RELEASE_BACKUP_ROOT:-/var/lib/sub2api-release/backups}"
LEGACY_RELEASE_BACKUP_ROOT="${SUB2API_LEGACY_RELEASE_BACKUP_ROOT:-$SUB2API_DATA_DIR/release-backups}"
PRODUCTION_RELEASE_STATE_FILE="${SUB2API_RELEASE_STATE_FILE:-$SUB2API_DATA_DIR/release-state.json}"
RELEASE_LEDGER_LOCK_FILE="${SUB2API_RELEASE_LEDGER_LOCK_FILE:-${SUB2API_SYNC_PUBLISH_LOCK:-/var/lock/sub2api-release.lock}}"
RELEASE_REPO="${SUB2API_REPO:-/root/sub2api}"
RELEASE_MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
RELEASE_EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"

ledger_state_path() { printf '%s/state.json\n' "$RELEASE_LEDGER_ROOT"; }
ledger_release_path() { printf '%s/releases/%s.json\n' "$RELEASE_LEDGER_ROOT" "$1"; }
ledger_operation_path() { printf '%s/operations/%s.json\n' "$RELEASE_LEDGER_ROOT" "$1"; }

ledger_validate_state() {
  jq -e '
    type == "object"
    and .schema_version == 1
    and (.current_release_id | type == "string" and test("^release-[A-Za-z0-9-]+$"))
    and (.custom_version_high_water | type == "number" and floor == . and . >= 0)
    and ((.active_operation_id == null) or (.active_operation_id | type == "string" and test("^(update|rollback)-[A-Za-z0-9-]+$")))
    and (.updated_at | fromdateiso8601 > 0)
  ' "$1" >/dev/null
}

ledger_validate_release() {
  local path="$1" backup_dir
  [[ -f "$path" && ! -L "$path" ]] || return 1
  jq -e '
    type == "object"
    and .schema_version == 1
    and (.release_id | type == "string" and test("^release-[A-Za-z0-9-]+$"))
    and (.official_version | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
    and (.official_commit | type == "string" and test("^[0-9a-f]{40}$"))
    and (.custom_version_sequence | type == "number" and floor == . and . >= 0)
    and .custom_version == ("v1.0." + (.custom_version_sequence | tostring))
    and (.custom_commit | type == "string" and test("^[0-9a-f]{40}$"))
    and (.main_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
    and (.extensions_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
    and (.base_compose_sha256 | type == "string" and test("^[0-9a-f]{64}$"))
    and (.custom_compose_sha256 | type == "string" and test("^[0-9a-f]{64}$"))
    and (.rendered_compose_sha256 | type == "string" and test("^[0-9a-f]{64}$"))
    and (.env_sha256 | type == "string" and test("^[0-9a-f]{64}$"))
    and (.backup_manifest_sha256 | type == "string" and test("^[0-9a-f]{64}$"))
    and (.backup_dir | type == "string" and length > 0)
    and (.published_at | fromdateiso8601 > 0)
    and (.source_kind | IN("official", "custom", "combined", "bootstrap", "identity-config"))
    and (if .source_kind == "identity-config" then
      (.identity_transition | IN("stage1-v2", "stage1-ip", "stage1-device", "stage2-admin", "stage3-shadow-window", "stage3-rules", "stage4-geo"))
    else (.identity_transition // "") == "" end)
    and (.operation_id | type == "string" and length > 0)
  ' "$path" >/dev/null || return 1
  backup_dir="$(jq -er '.backup_dir' "$path")" || return 1
  backup_dir="$(ledger_canonical_backup_path "$backup_dir")" || return 1
  if [[ "$(jq -r '.source_kind' "$path")" == identity-config ]]; then
    ledger_backup_path_is_protected "$backup_dir" || return 1
  fi
}

ledger_canonical_backup_path() {
  local candidate="$1" root root_real candidate_real
  candidate_real="$(cd "$candidate" 2>/dev/null && pwd -P)" || return 1
  for root in "$RELEASE_BACKUP_ROOT" "$LEGACY_RELEASE_BACKUP_ROOT"; do
    root_real="$(cd "$root" 2>/dev/null && pwd -P)" || continue
    case "$candidate_real/" in
      "$root_real/"*) printf '%s\n' "$candidate_real"; return 0 ;;
    esac
  done
  return 1
}

ledger_backup_path_is_protected() {
  local candidate="$1" root_real candidate_real
  root_real="$(cd "$RELEASE_BACKUP_ROOT" 2>/dev/null && pwd -P)" || return 1
  candidate_real="$(cd "$candidate" 2>/dev/null && pwd -P)" || return 1
  case "$candidate_real/" in "$root_real/"*) return 0 ;; *) return 1 ;; esac
}

ledger_validate_manifest_exact() {
  local backup_dir="$1" manifest_name="$2"
  local canonical manifest_path line relative full_path parent_real actual
  local -A declared=()
  canonical="$(ledger_canonical_backup_path "$backup_dir")" || return 1
  manifest_path="$canonical/$manifest_name"
  [[ -s "$manifest_path" && ! -L "$manifest_path" ]] || return 1

  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" != \\* ]] || return 1
    [[ "$line" =~ ^[0-9a-f]{64}[[:space:]][[:space:]\*](.+)$ ]] || return 1
    relative="${BASH_REMATCH[1]}"
    while [[ "$relative" == ./* ]]; do
      relative="${relative#./}"
    done
    [[ -n "$relative" && "$relative" != /* && ! "$relative" =~ ^[A-Za-z]:[\\/] ]] || return 1
    [[ "$relative" != *\\* && "$relative" != *//* ]] || return 1
    [[ ! "$relative" =~ (^|/)\.\.?(/|$) ]] || return 1
    [[ "$(basename "$relative")" != SHA256SUMS ]] || return 1
    [[ -z "${declared[$relative]+present}" ]] || return 1
    full_path="$canonical/$relative"
    [[ -f "$full_path" && ! -L "$full_path" ]] || return 1
    parent_real="$(cd "$(dirname "$full_path")" 2>/dev/null && pwd -P)" || return 1
    case "$parent_real/" in
      "$canonical/"*) ;;
      *) return 1 ;;
    esac
    declared["$relative"]=1
  done < "$manifest_path"

  while IFS= read -r actual; do
    [[ -n "${declared[$actual]+present}" ]] || return 1
    unset 'declared[$actual]'
  done < <(find "$canonical" -type f ! -name SHA256SUMS -printf '%P\n' | LC_ALL=C sort)
  [[ "${#declared[@]}" -eq 0 ]] || return 1
  (cd "$canonical" && sha256sum -c "$manifest_name" >/dev/null) || return 1
}

ledger_validate_backup_contract() {
  local backup_dir="$1" canonical path_file referenced basename_value required
  canonical="$(ledger_canonical_backup_path "$backup_dir")" || return 1
  [[ -z "$(find "$canonical" -type l -print -quit)" ]] || return 1
  for required in \
    SHA256SUMS .env docker-compose.yml docker-compose.custom.yml release-state.json \
    container-metadata.json image-metadata.txt rollback-tags.txt \
    sub2api_db.dump sub2api_db.list risk_control_db.dump risk_control_db.list \
    docker-containers.txt docker-images.txt nginx-vhost.path origin-cert.path origin-key.path \
    target/SHA256SUMS target/.env target/docker-compose.yml \
    target/docker-compose.custom.yml target/rendered-compose.json; do
    [[ -s "$canonical/$required" && ! -L "$canonical/$required" ]] || return 1
  done
  for path_file in nginx-vhost.path origin-cert.path origin-key.path; do
    referenced="$(head -n 1 "$canonical/$path_file")"
    [[ -n "$referenced" ]] || return 1
    basename_value="$(basename "$referenced")"
    [[ "$basename_value" != . && "$basename_value" != .. ]] || return 1
    [[ -f "$canonical/$basename_value" && ! -L "$canonical/$basename_value" ]] || return 1
  done
  ledger_validate_manifest_exact "$canonical/target" SHA256SUMS || return 1
  ledger_validate_manifest_exact "$canonical" SHA256SUMS || return 1
}

ledger_validate_release_artifacts() {
  local record_path="$1" record backup_dir
  ledger_validate_release "$record_path" || return 1
  record="$(cat "$record_path")"
  backup_dir="$(jq -r '.backup_dir' <<< "$record")"
  ledger_validate_backup_contract "$backup_dir" || return 1
  [[ "$(jq -r '.backup_manifest_sha256' <<< "$record")" == "$(sha256sum "$backup_dir/SHA256SUMS" | awk '{print $1}')" ]] || return 1
  [[ "$(jq -r '.base_compose_sha256' <<< "$record")" == "$(sha256sum "$backup_dir/target/docker-compose.yml" | awk '{print $1}')" ]] || return 1
  [[ "$(jq -r '.custom_compose_sha256' <<< "$record")" == "$(sha256sum "$backup_dir/target/docker-compose.custom.yml" | awk '{print $1}')" ]] || return 1
  [[ "$(jq -r '.rendered_compose_sha256' <<< "$record")" == "$(sha256sum "$backup_dir/target/rendered-compose.json" | awk '{print $1}')" ]] || return 1
  [[ "$(jq -r '.env_sha256' <<< "$record")" == "$(sha256sum "$backup_dir/target/.env" | awk '{print $1}')" ]]
}

ledger_release_runtime_available() {
  local record_path="$1" record commit main_digest extensions_digest reference
  record="$(cat "$record_path")" || return 1
  commit="$(jq -er '.custom_commit' <<< "$record")" || return 1
  main_digest="$(jq -er '.main_digest' <<< "$record")" || return 1
  extensions_digest="$(jq -er '.extensions_digest' <<< "$record")" || return 1
  git -C "$RELEASE_REPO" cat-file -e "$commit^{commit}" >/dev/null 2>&1 || return 1
  for reference in "$RELEASE_MAIN_REPOSITORY@$main_digest" "$RELEASE_EXTENSIONS_REPOSITORY@$extensions_digest"; do
    docker image inspect "$reference" >/dev/null 2>&1 \
      || docker manifest inspect "$reference" >/dev/null 2>&1 \
      || return 1
  done
}

ledger_list_rollback_release_ids() {
  local limit="${1:-3}" state_path current path record release_id published count=0
  local -a candidates=()
  [[ "$limit" =~ ^[1-9][0-9]*$ ]] || return 1
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  current="$(jq -r '.current_release_id' "$state_path")"
  shopt -s nullglob
  for path in "$RELEASE_LEDGER_ROOT"/releases/*.json; do
    ledger_validate_release_artifacts "$path" || continue
    record="$(cat "$path")"
    release_id="$(jq -r '.release_id' <<< "$record")"
    [[ "$release_id" != "$current" ]] || continue
    published="$(jq -r '.published_at' <<< "$record")"
    candidates+=("$published $release_id")
  done
  shopt -u nullglob
  while IFS=' ' read -r published release_id; do
    [[ -n "$release_id" ]] || continue
    ledger_release_runtime_available "$(ledger_release_path "$release_id")" || continue
    printf '%s\n' "$release_id"
    count=$((count + 1))
    [[ "$count" -lt "$limit" ]] || break
  done < <(printf '%s\n' "${candidates[@]}" | LC_ALL=C sort -r)
}

ledger_atomic_write() {
  local path="$1" content="$2" directory temporary
  directory="$(dirname "$path")"
  mkdir -p "$directory"
  temporary="$(mktemp "$directory/.ledger.XXXXXX")"
  printf '%s\n' "$content" > "$temporary" || { rm -f "$temporary"; return 1; }
  chmod 0644 "$temporary" || { rm -f "$temporary"; return 1; }
  (sync -f "$temporary" 2>/dev/null || sync) || { rm -f "$temporary"; return 1; }
  if ! mv -f "$temporary" "$path"; then
    rm -f "$temporary"
    return 1
  fi
  sync -f "$directory" 2>/dev/null || sync
}

ledger_create_release() {
  local content="$1" release_id path directory temporary
  release_id="$(jq -er '.release_id' <<< "$content")" || return 1
  [[ "$release_id" =~ ^release-[A-Za-z0-9-]+$ ]] || return 1
  path="$(ledger_release_path "$release_id")"
  directory="$(dirname "$path")"
  mkdir -p "$directory"
  temporary="$(mktemp "$directory/.release.XXXXXX")"
  printf '%s\n' "$content" > "$temporary" || { rm -f "$temporary"; return 1; }
  chmod 0644 "$temporary" || { rm -f "$temporary"; return 1; }
  ledger_validate_release "$temporary" || { rm -f "$temporary"; return 1; }
  (sync -f "$temporary" 2>/dev/null || sync) || { rm -f "$temporary"; return 1; }
  if [[ -e "$path" ]]; then
    ledger_validate_release "$path" || { rm -f "$temporary"; return 1; }
    cmp -s "$temporary" "$path" || { rm -f "$temporary"; return 1; }
  else
    ln "$temporary" "$path" || { rm -f "$temporary"; return 1; }
    (sync -f "$directory" 2>/dev/null || sync) || { rm -f "$temporary"; return 1; }
  fi
  rm -f "$temporary"
}

ledger_validate_projection() {
  local content="$1"
  jq -e '
    type == "object"
    and (.production_commit | test("^[0-9a-f]{40}$"))
    and (.stable_release_tag | test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
    and (.stable_release_commit | test("^[0-9a-f]{40}$"))
    and (.main_digest | test("^sha256:[0-9a-f]{64}$"))
    and (.extensions_digest | test("^sha256:[0-9a-f]{64}$"))
    and (.release_id | test("^release-[A-Za-z0-9-]+$"))
    and (.official_version | test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
    and (.custom_version | test("^v1\\.0\\.[0-9]+$"))
    and (.custom_version_sequence | type == "number" and floor == . and . >= 0)
  ' <<< "$content" >/dev/null
}

ledger_validate_current_projection_consistency() {
  local state_path="$1" projection_path="$2" current_path="$3" state projection current_record path sequence high_water
  ledger_validate_state "$state_path" || return 1
  [[ -f "$projection_path" && ! -L "$projection_path" ]] || return 1
  state="$(cat "$state_path")" || return 1
  projection="$(cat "$projection_path")" || return 1
  ledger_validate_projection "$projection" || return 1
  [[ -f "$current_path" && ! -L "$current_path" ]] || return 1
  current_record="$(cat "$current_path")" || return 1
  jq -e --argjson state "$state" --argjson record "$current_record" '
    .release_id == $state.current_release_id
    and .release_id == $record.release_id
    and .production_commit == $record.custom_commit
    and .stable_release_tag == $record.official_version
    and .stable_release_commit == $record.official_commit
    and .main_digest == $record.main_digest
    and .extensions_digest == $record.extensions_digest
    and .official_version == $record.official_version
    and .custom_version == $record.custom_version
    and .custom_version_sequence == $record.custom_version_sequence
    and .published_at == $record.published_at
    and .backup_dir == $record.backup_dir
  ' <<< "$projection" >/dev/null || return 1
  high_water="$(jq -er '.custom_version_high_water' <<< "$state")" || return 1
  shopt -s nullglob
  for path in "$RELEASE_LEDGER_ROOT"/releases/*.json; do
    [[ -f "$path" && ! -L "$path" ]] || { shopt -u nullglob; return 1; }
    sequence="$(jq -er '
      .custom_version_sequence as $sequence
      | select($sequence | type == "number" and floor == . and . >= 0)
      | select(.custom_version == ("v1.0." + ($sequence | tostring)))
      | $sequence
    ' "$path")" || { shopt -u nullglob; return 1; }
    [[ "$high_water" -ge "$sequence" ]] || { shopt -u nullglob; return 1; }
  done
  shopt -u nullglob
}

ledger_validate_current_projection() {
  local state_path current_release current_path
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  current_release="$(jq -er '.current_release_id' "$state_path")" || return 1
  current_path="$(ledger_release_path "$current_release")"
  ledger_validate_release "$current_path" || return 1
  ledger_validate_current_projection_consistency "$state_path" "$PRODUCTION_RELEASE_STATE_FILE" "$current_path"
}

ledger_write_projection() {
  local content="$1"
  ledger_validate_projection "$content" || return 1
  ledger_atomic_write "$PRODUCTION_RELEASE_STATE_FILE" "$content"
}

ledger_projection_for_release() {
  local record_content="$1"
  [[ -r "$PRODUCTION_RELEASE_STATE_FILE" ]] || return 1
  jq --argjson record "$record_content" '. + {
    production_commit:$record.custom_commit,
    stable_release_tag:$record.official_version,
    stable_release_commit:$record.official_commit,
    main_digest:$record.main_digest,
    extensions_digest:$record.extensions_digest,
    published_at:$record.published_at,
    backup_dir:$record.backup_dir,
    release_id:$record.release_id,
    official_version:$record.official_version,
    custom_version:$record.custom_version,
    custom_version_sequence:$record.custom_version_sequence
  }' "$PRODUCTION_RELEASE_STATE_FILE"
}

ledger_with_lock() {
  local function_name="$1" result=0 inherited_fd="${SUB2API_RELEASE_LOCK_FD:-9}" inherited_path lock_path
  shift
  mkdir -p "$(dirname "$RELEASE_LEDGER_LOCK_FILE")"
  touch "$RELEASE_LEDGER_LOCK_FILE" || return 1
  if [[ "$inherited_fd" =~ ^[0-9]+$ && -e "/proc/$$/fd/$inherited_fd" ]]; then
    inherited_path="$(readlink -f "/proc/$$/fd/$inherited_fd" 2>/dev/null || true)"
    lock_path="$(readlink -f "$RELEASE_LEDGER_LOCK_FILE" 2>/dev/null || true)"
    if [[ -n "$inherited_path" && "$inherited_path" == "$lock_path" ]]; then
      flock -n "$inherited_fd" || return 1
      "$function_name" "$@" || result=$?
      return "$result"
    fi
  fi
  exec 8>"$RELEASE_LEDGER_LOCK_FILE"
  if ! flock -x 8; then
    exec 8>&-
    return 1
  fi
  "$function_name" "$@" || result=$?
  flock -u 8 || result=1
  exec 8>&-
  return "$result"
}

_ledger_set_active_operation_unlocked() {
  local operation_id="$1" state_path state current content now
  [[ "$operation_id" =~ ^(update|rollback)-[A-Za-z0-9-]+$ ]] || return 1
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  state="$(cat "$state_path")"
  current="$(jq -r '.active_operation_id // empty' <<< "$state")"
  if [[ -n "$current" && "$current" != "$operation_id" ]] \
    && _ledger_recover_pre_mutation_terminal_unlocked "$current"; then
    state="$(cat "$state_path")"
    current="$(jq -r '.active_operation_id // empty' <<< "$state")"
  fi
  [[ -z "$current" || "$current" == "$operation_id" ]] || return 1
  [[ "$current" != "$operation_id" ]] || return 0
  now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  content="$(jq --arg operation "$operation_id" --arg now "$now" '.active_operation_id=$operation | .updated_at=$now' <<< "$state")"
  ledger_atomic_write "$state_path" "$content"
}

ledger_set_active_operation() {
  ledger_with_lock _ledger_set_active_operation_unlocked "$1"
}

_ledger_clear_active_operation_unlocked() {
  local operation_id="$1" state_path state current now
  [[ "$operation_id" =~ ^(update|rollback)-[A-Za-z0-9-]+$ ]] || return 1
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  state="$(cat "$state_path")"
  current="$(jq -r '.active_operation_id // empty' <<< "$state")"
  [[ "$current" == "$operation_id" ]] || { [[ -z "$current" ]] && return 0; return 1; }
  now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  state="$(jq --arg now "$now" '.active_operation_id=null | .updated_at=$now' <<< "$state")" || return 1
  ledger_atomic_write "$state_path" "$state"
}

ledger_clear_active_operation() {
  ledger_with_lock _ledger_clear_active_operation_unlocked "$1"
}

_ledger_settle_pre_mutation_failure_unlocked() {
  local operation_id="$1" status="$2" message="$3" metadata="$4"
  local state_path state current operation_path operation current_status
  [[ "$operation_id" =~ ^(update|rollback)-[A-Za-z0-9-]+$ ]] || return 1
  release_terminal_status "$status" || return 1
  [[ "$status" != success && "$status" != rollback_failed ]] || return 1
  jq -e 'type == "object" and .published == false and .production_changed == false' <<< "$metadata" >/dev/null || return 1
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  state="$(cat "$state_path")"
  current="$(jq -r '.active_operation_id // empty' <<< "$state")"
  [[ -z "$current" || "$current" == "$operation_id" ]] || return 1
  operation_path="$(ledger_operation_path "$operation_id")"
  [[ -f "$operation_path" && ! -L "$operation_path" ]] || return 1
  operation="$(cat "$operation_path")"
  current_status="$(jq -er '.status' <<< "$operation")" || return 1
  if release_terminal_status "$current_status"; then
    [[ "$current_status" == "$status" ]] || return 1
  else
    [[ "$current" == "$operation_id" ]] || return 1
    release_job_update "$operation_id" "$status" "$message" "$metadata" || return 1
    operation="$(cat "$operation_path")"
  fi
  jq -e --arg status "$status" '
    .status == $status and .published == false and .production_changed == false
  ' <<< "$operation" >/dev/null || return 1
  [[ "$current" != "$operation_id" ]] || _ledger_clear_active_operation_unlocked "$operation_id"
}

ledger_settle_pre_mutation_failure() {
  ledger_with_lock _ledger_settle_pre_mutation_failure_unlocked "$@"
}

_ledger_settle_noop_success_unlocked() {
  local operation_id="$1" message="$2" metadata="$3"
  local state_path state current operation_path operation current_status
  [[ "$operation_id" =~ ^(update|rollback)-[A-Za-z0-9-]+$ ]] || return 1
  jq -e 'type == "object" and .published == false and .production_changed == false' <<< "$metadata" >/dev/null || return 1
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  state="$(cat "$state_path")"
  current="$(jq -r '.active_operation_id // empty' <<< "$state")"
  [[ -z "$current" || "$current" == "$operation_id" ]] || return 1
  operation_path="$(ledger_operation_path "$operation_id")"
  [[ -f "$operation_path" && ! -L "$operation_path" ]] || return 1
  operation="$(cat "$operation_path")"
  current_status="$(jq -er '.status' <<< "$operation")" || return 1
  if release_terminal_status "$current_status"; then
    [[ "$current_status" == success ]] || return 1
  else
    [[ "$current" == "$operation_id" ]] || return 1
    release_job_update "$operation_id" success "$message" "$metadata" || return 1
    operation="$(cat "$operation_path")"
  fi
  jq -e '.status == "success" and .published == false and .production_changed == false' <<< "$operation" >/dev/null || return 1
  [[ "$current" != "$operation_id" ]] || _ledger_clear_active_operation_unlocked "$operation_id"
}

ledger_settle_noop_success() {
  ledger_with_lock _ledger_settle_noop_success_unlocked "$@"
}

_ledger_recover_pre_mutation_terminal_unlocked() {
  local operation_id="$1" state_path state current operation_path operation
  [[ "$operation_id" =~ ^(update|rollback)-[A-Za-z0-9-]+$ ]] || return 1
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  state="$(cat "$state_path")"
  current="$(jq -r '.active_operation_id // empty' <<< "$state")"
  [[ -z "$current" || "$current" == "$operation_id" ]] || return 1
  operation_path="$(ledger_operation_path "$operation_id")"
  [[ -f "$operation_path" && ! -L "$operation_path" ]] || return 1
  operation="$(cat "$operation_path")"
  jq -e '
    ((.status | IN("failed", "conflict", "expired", "drifted", "failed_rolled_back")) or .status == "success")
    and .published == false
    and .production_changed == false
  ' <<< "$operation" >/dev/null || return 1
  [[ "$current" != "$operation_id" ]] || _ledger_clear_active_operation_unlocked "$operation_id"
}

ledger_recover_pre_mutation_terminal() {
  ledger_with_lock _ledger_recover_pre_mutation_terminal_unlocked "$1"
}

_ledger_commit_release_unlocked() {
  local record_content="$1" advance_high_water="${2:-0}"
  local release_id sequence operation_id state_path state current_record current_sequence projection now
  local operation_path operation update_kind base_release_id base_high_water proposed_sequence advances source_kind
  local stable_tag target_custom_commit custom_docs_only current_official_version current_official_commit current_custom_version current_custom_commit identity_transition
  [[ "$advance_high_water" == 0 || "$advance_high_water" == 1 ]] || return 1
  release_id="$(jq -er '.release_id' <<< "$record_content")" || return 1
  sequence="$(jq -er '.custom_version_sequence' <<< "$record_content")" || return 1
  operation_id="$(jq -er '.operation_id' <<< "$record_content")" || return 1
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  state="$(cat "$state_path")"
  [[ "$(jq -r '.active_operation_id // empty' <<< "$state")" == "$operation_id" ]] || return 1
  current_record="$(ledger_release_path "$(jq -r '.current_release_id' <<< "$state")")"
  ledger_validate_release "$current_record" || return 1
  current_sequence="$(jq -r '.custom_version_sequence' "$current_record")"
  operation_path="$(ledger_operation_path "$operation_id")"
  [[ -f "$operation_path" && ! -L "$operation_path" ]] || return 1
  operation="$(cat "$operation_path")"
  jq -e --arg operation "$operation_id" --arg release "$release_id" '
    type == "object"
    and .job_id == $operation
    and .operation_kind == "update"
    and .action == "apply"
    and .status == "health_checking"
    and .target_release_id == $release
  ' <<< "$operation" >/dev/null || return 1
  base_release_id="$(jq -er '.base_release_id' <<< "$operation")" || return 1
  base_high_water="$(jq -er '.base_custom_high_water' <<< "$operation")" || return 1
  proposed_sequence="$(jq -er '.proposed_custom_sequence' <<< "$operation")" || return 1
  advances="$(jq -r '.advances_custom_version' <<< "$operation")" || return 1
  [[ "$advances" == true || "$advances" == false ]] || return 1
  update_kind="$(jq -er '.update_kind' <<< "$operation")" || return 1
  source_kind="$(jq -er '.source_kind' <<< "$record_content")" || return 1
  stable_tag="$(jq -er '.stable_release_tag' <<< "$operation")" || return 1
  target_custom_commit="$(jq -er '.target_custom_commit' <<< "$operation")" || return 1
  custom_docs_only="$(jq -r '.custom_docs_only // false' <<< "$operation")" || return 1
	identity_transition="$(jq -r '.identity_transition // empty' <<< "$operation")" || return 1
  [[ "$custom_docs_only" == true || "$custom_docs_only" == false ]] || return 1
  [[ "$target_custom_commit" =~ ^[0-9a-f]{40}$ ]] || return 1
  current_official_version="$(jq -r '.official_version' "$current_record")"
  current_official_commit="$(jq -r '.official_commit' "$current_record")"
  current_custom_version="$(jq -r '.custom_version' "$current_record")"
  current_custom_commit="$(jq -r '.custom_commit' "$current_record")"
  [[ "$base_release_id" == "$(jq -r '.current_release_id' <<< "$state")" ]] || return 1
  [[ "$base_high_water" -eq "$(jq -r '.custom_version_high_water' <<< "$state")" ]] || return 1
  [[ "$proposed_sequence" -eq "$sequence" && "$update_kind" == "$source_kind" ]] || return 1
  jq -e --argjson record "$record_content" '
    .target_official_version == $record.official_version
    and .target_custom_version == $record.custom_version
    and .target_commit == $record.custom_commit
    and .stable_release_commit == $record.official_commit
    and .main_digest == $record.main_digest
    and .extensions_digest == $record.extensions_digest
  ' <<< "$operation" >/dev/null || return 1
  [[ "$stable_tag" == "$(jq -r '.official_version' <<< "$record_content")" ]] || return 1
  case "$source_kind" in
    official)
      [[ "$advances" == false ]] || return 1
      [[ "$(jq -r '.official_version' <<< "$record_content")" != "$current_official_version" ]] || return 1
      [[ "$(jq -r '.official_commit' <<< "$record_content")" != "$current_official_commit" ]] || return 1
      [[ "$(jq -r '.custom_version' <<< "$record_content")" == "$current_custom_version" ]] || return 1
      if [[ "$custom_docs_only" == true ]]; then
        [[ "$target_custom_commit" != "$current_custom_commit" ]] || return 1
      else
        [[ "$target_custom_commit" == "$current_custom_commit" ]] || return 1
      fi
      ;;
    custom)
      [[ "$custom_docs_only" == false ]] || return 1
      [[ "$advances" == true ]] || return 1
      [[ "$(jq -r '.official_version' <<< "$record_content")" == "$current_official_version" ]] || return 1
      [[ "$(jq -r '.official_commit' <<< "$record_content")" == "$current_official_commit" ]] || return 1
      [[ "$target_custom_commit" == "$(jq -r '.custom_commit' <<< "$record_content")" ]] || return 1
      [[ "$target_custom_commit" != "$current_custom_commit" ]] || return 1
      ;;
    combined)
      [[ "$custom_docs_only" == false ]] || return 1
      [[ "$advances" == true ]] || return 1
      [[ "$(jq -r '.official_version' <<< "$record_content")" != "$current_official_version" ]] || return 1
      [[ "$(jq -r '.official_commit' <<< "$record_content")" != "$current_official_commit" ]] || return 1
      [[ "$target_custom_commit" != "$current_custom_commit" ]] || return 1
      ;;
	identity-config)
	  [[ "$custom_docs_only" == false && "$advances" == false ]] || return 1
	  [[ "$(jq -r '.base_release_id' <<< "$operation")" == "$(jq -r '.release_id' "$current_record")" ]] || return 1
	  [[ "$identity_transition" =~ ^stage(1-(v2|ip|device)|2-admin|3-(shadow-window|rules)|4-geo)$ ]] || return 1
	  [[ "$(jq -r '.identity_transition // empty' <<< "$record_content")" == "$identity_transition" ]] || return 1
	  [[ "$(jq -r '.official_version' <<< "$record_content")" == "$current_official_version" ]] || return 1
	  [[ "$(jq -r '.official_commit' <<< "$record_content")" == "$current_official_commit" ]] || return 1
	  [[ "$(jq -r '.custom_version' <<< "$record_content")" == "$current_custom_version" ]] || return 1
	  [[ "$(jq -r '.custom_commit' <<< "$record_content")" == "$current_custom_commit" ]] || return 1
	  [[ "$target_custom_commit" == "$current_custom_commit" ]] || return 1
	  ;;
    *) return 1 ;;
  esac
  if [[ "$advance_high_water" == 1 ]]; then
    [[ "$advances" == true && ("$source_kind" == custom || "$source_kind" == combined) ]] || return 1
    [[ "$sequence" -eq $(( $(jq -r '.custom_version_high_water' <<< "$state") + 1 )) ]] || return 1
  else
	[[ "$advances" == false && ("$source_kind" == official || "$source_kind" == identity-config) ]] || return 1
    [[ "$sequence" -eq "$current_sequence" ]] || return 1
  fi
  ledger_create_release "$record_content" || return 1
  if [[ "${SUB2API_LEDGER_COMMIT_FAILPOINT:-}" == before_projection ]]; then
    SUB2API_LEDGER_COMMIT_FAILPOINT=''
    export SUB2API_LEDGER_COMMIT_FAILPOINT
    return 97
  fi
  projection="$(ledger_projection_for_release "$record_content")" || return 1
  ledger_write_projection "$projection" || return 1
  if [[ "${SUB2API_LEDGER_COMMIT_FAILPOINT:-}" == before_state ]]; then
    SUB2API_LEDGER_COMMIT_FAILPOINT=''
    export SUB2API_LEDGER_COMMIT_FAILPOINT
    return 98
  fi
  now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  state="$(jq --arg release "$release_id" --arg now "$now" --argjson sequence "$sequence" --argjson advance "$advance_high_water" '
    .current_release_id=$release
    | .active_operation_id=null
    | .updated_at=$now
    | if $advance == 1 then .custom_version_high_water=$sequence else . end
  ' <<< "$state")"
  ledger_atomic_write "$state_path" "$state" || return 1
  operation="$(jq --arg now "$now" --arg published "$(jq -r '.published_at' <<< "$record_content")" \
    --arg release "$release_id" --arg backup "$(jq -r '.backup_dir' <<< "$record_content")" '
    .status="success"
    | .message="release published and committed to the production ledger"
    | .ts=$now
    | .updated_at=$now
    | .finished_at=$now
    | .published=true
    | .published_commit=.target_commit
    | .published_at=$published
    | .target_release_id=$release
    | .production_changed=true
    | .artifact_path=$backup
    | .rollback={attempted:false,succeeded:false,message:""}
  ' <<< "$operation")" || return 1
  ledger_atomic_write "$operation_path" "$operation"
}

ledger_commit_release() {
  ledger_with_lock _ledger_commit_release_unlocked "$@"
}

_ledger_restore_failed_apply_unlocked() {
  local base_release_id="$1" base_high_water="$2" operation_id="$3" base_projection="$4"
  local state_path state base_record now
  [[ "$base_release_id" =~ ^release-[A-Za-z0-9-]+$ ]] || return 1
  [[ "$base_high_water" =~ ^[0-9]+$ ]] || return 1
  [[ "$operation_id" =~ ^update-[A-Za-z0-9-]+$ ]] || return 1
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  state="$(cat "$state_path")"
  [[ "$(jq -r '.current_release_id' <<< "$state")" == "$base_release_id" ]] || return 1
  [[ "$(jq -r '.custom_version_high_water' <<< "$state")" -eq "$base_high_water" ]] || return 1
  [[ "$(jq -r '.active_operation_id // empty' <<< "$state")" == "$operation_id" ]] || return 1
  base_record="$(ledger_release_path "$base_release_id")"
  ledger_validate_release "$base_record" || return 1
  ledger_validate_projection "$base_projection" || return 1
  jq -e --argjson record "$(cat "$base_record")" '
    .production_commit == $record.custom_commit
    and .stable_release_commit == $record.official_commit
    and .main_digest == $record.main_digest
    and .extensions_digest == $record.extensions_digest
    and .release_id == $record.release_id
    and .official_version == $record.official_version
    and .custom_version == $record.custom_version
    and .custom_version_sequence == $record.custom_version_sequence
  ' <<< "$base_projection" >/dev/null || return 1
  ledger_write_projection "$base_projection" || return 1
  now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  state="$(jq --arg now "$now" '.active_operation_id=null | .updated_at=$now' <<< "$state")" || return 1
  ledger_atomic_write "$state_path" "$state"
}

ledger_restore_failed_apply() {
  ledger_with_lock _ledger_restore_failed_apply_unlocked "$@"
}

_ledger_commit_rollback_unlocked() {
  local target_release_id="$1" operation_id="$2" state_path state target_record projection now high_water
  local operation_path operation base_release_id base_high_water target_commit backup_dir
  [[ "$target_release_id" =~ ^release-[A-Za-z0-9-]+$ ]] || return 1
  [[ "$operation_id" =~ ^rollback-[A-Za-z0-9-]+$ ]] || return 1
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  state="$(cat "$state_path")"
  [[ "$(jq -r '.active_operation_id // empty' <<< "$state")" == "$operation_id" ]] || return 1
  base_release_id="$(jq -r '.current_release_id' <<< "$state")"
  high_water="$(jq -r '.custom_version_high_water' <<< "$state")"
  operation_path="$(ledger_operation_path "$operation_id")"
  [[ -f "$operation_path" && ! -L "$operation_path" ]] || return 1
  operation="$(cat "$operation_path")"
  jq -e --arg job "$operation_id" --arg base "$base_release_id" --arg target "$target_release_id" --argjson high "$high_water" '
    .job_id == $job and .operation_kind == "rollback" and .action == "apply"
    and .status == "health_checking" and .base_release_id == $base and .target_release_id == $target
    and .base_custom_high_water == $high and (.advances_custom_version // false) == false
  ' <<< "$operation" >/dev/null || return 1
  target_record="$(ledger_release_path "$target_release_id")"
  ledger_validate_release "$target_record" || return 1
  [[ "$(jq -r '.custom_version_sequence' "$target_record")" -le "$high_water" ]] || return 1
  jq -e --argjson record "$(cat "$target_record")" '
    .target_official_version == $record.official_version
    and .target_custom_version == $record.custom_version
    and .target_commit == $record.custom_commit
    and .main_digest == $record.main_digest
    and .extensions_digest == $record.extensions_digest
  ' <<< "$operation" >/dev/null || return 1
  projection="$(ledger_projection_for_release "$(cat "$target_record")")" || return 1
  ledger_write_projection "$projection" || return 1
  if [[ "${SUB2API_LEDGER_COMMIT_FAILPOINT:-}" == before_state ]]; then
    SUB2API_LEDGER_COMMIT_FAILPOINT=''
    export SUB2API_LEDGER_COMMIT_FAILPOINT
    return 98
  fi
  now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  state="$(jq --arg release "$target_release_id" --arg now "$now" '
    .current_release_id=$release | .active_operation_id=null | .updated_at=$now
  ' <<< "$state")"
  ledger_atomic_write "$state_path" "$state" || return 1
  target_commit="$(jq -r '.custom_commit' "$target_record")"
  backup_dir="$(jq -r '.backup_dir // empty' <<< "$operation")"
  operation="$(jq --arg now "$now" --arg target "$target_release_id" --arg commit "$target_commit" --arg artifact "$backup_dir" '
    .status="success" | .message="complete release snapshot rollback committed"
    | .ts=$now | .updated_at=$now | .finished_at=$now
    | .published=false | .published_commit=$commit | .production_changed=true
    | .target_release_id=$target | .artifact_path=$artifact
    | .rollback={attempted:false,succeeded:false,message:""}
  ' <<< "$operation")" || return 1
  ledger_atomic_write "$operation_path" "$operation"
}

ledger_commit_rollback() {
  ledger_with_lock _ledger_commit_rollback_unlocked "$@"
}

_ledger_restore_failed_rollback_unlocked() {
  local base_release_id="$1" base_high_water="$2" operation_id="$3" base_projection="$4"
  local state_path state base_record now
  [[ "$base_release_id" =~ ^release-[A-Za-z0-9-]+$ && "$base_high_water" =~ ^[0-9]+$ ]] || return 1
  [[ "$operation_id" =~ ^rollback-[A-Za-z0-9-]+$ ]] || return 1
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  state="$(cat "$state_path")"
  [[ "$(jq -r '.current_release_id' <<< "$state")" == "$base_release_id" ]] || return 1
  [[ "$(jq -r '.custom_version_high_water' <<< "$state")" -eq "$base_high_water" ]] || return 1
  [[ "$(jq -r '.active_operation_id // empty' <<< "$state")" == "$operation_id" ]] || return 1
  base_record="$(ledger_release_path "$base_release_id")"
  ledger_validate_release "$base_record" || return 1
  ledger_validate_projection "$base_projection" || return 1
  jq -e --argjson record "$(cat "$base_record")" '
    .production_commit == $record.custom_commit and .stable_release_commit == $record.official_commit
    and .main_digest == $record.main_digest and .extensions_digest == $record.extensions_digest
    and .release_id == $record.release_id and .official_version == $record.official_version
    and .custom_version == $record.custom_version and .custom_version_sequence == $record.custom_version_sequence
  ' <<< "$base_projection" >/dev/null || return 1
  ledger_write_projection "$base_projection" || return 1
  now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  state="$(jq --arg now "$now" '.active_operation_id=null | .updated_at=$now' <<< "$state")" || return 1
  ledger_atomic_write "$state_path" "$state"
}

ledger_restore_failed_rollback() {
  ledger_with_lock _ledger_restore_failed_rollback_unlocked "$@"
}

_ledger_recover_or_refuse_unlocked() {
  local expected_record="$1" expected_projection="$2" expected_high_water="$3" expected_operation_id="${4:-}"
  local expected_release_id source_kind target_sequence base_release_id base_record base_sequence base_high_water
  local state_path state state_release_id state_high_water state_operation_id actual_record operation_path operation operation_kind
  local update_kind proposed_sequence advances_custom_version operation_status now
  expected_release_id="$(jq -er '.release_id' <<< "$expected_record")" || return 2
  source_kind="$(jq -er '.source_kind' <<< "$expected_record")" || return 2
  target_sequence="$(jq -er '.custom_version_sequence' <<< "$expected_record")" || return 2
  [[ "$expected_high_water" =~ ^[0-9]+$ ]] || return 2
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 2
  state="$(cat "$state_path")"
  ledger_validate_projection "$expected_projection" || return 2
  jq -e --argjson record "$expected_record" '
    .production_commit == $record.custom_commit
    and .stable_release_tag == $record.official_version
    and .stable_release_commit == $record.official_commit
    and .main_digest == $record.main_digest
    and .extensions_digest == $record.extensions_digest
    and .published_at == $record.published_at
    and .backup_dir == $record.backup_dir
    and .release_id == $record.release_id
    and .official_version == $record.official_version
    and .custom_version == $record.custom_version
    and .custom_version_sequence == $record.custom_version_sequence
  ' <<< "$expected_projection" >/dev/null || return 2
  actual_record="$(ledger_release_path "$expected_release_id")"
  ledger_validate_release "$actual_record" || return 2
  [[ "$(jq -cS . "$actual_record")" == "$(jq -cS . <<< "$expected_record")" ]] || return 2
  ledger_validate_projection "$(cat "$PRODUCTION_RELEASE_STATE_FILE")" || return 2
  [[ "$(jq -cS . "$PRODUCTION_RELEASE_STATE_FILE")" == "$(jq -cS . <<< "$expected_projection")" ]] || return 2

  state_release_id="$(jq -r '.current_release_id' <<< "$state")"
  state_high_water="$(jq -r '.custom_version_high_water' <<< "$state")"
  state_operation_id="$(jq -r '.active_operation_id // empty' <<< "$state")"
  if [[ "$source_kind" == bootstrap && -z "$expected_operation_id" ]]; then
    [[ "$expected_high_water" -eq 0 && "$target_sequence" -eq 0 ]] || return 2
    [[ "$state_release_id" == "$expected_release_id" && "$state_high_water" == "$expected_high_water" && -z "$state_operation_id" ]] || return 2
    return 0
  fi

  [[ "$expected_operation_id" =~ ^(update|rollback)-[A-Za-z0-9-]+$ ]] || return 2
  operation_kind="${expected_operation_id%%-*}"
  operation_path="$(ledger_operation_path "$expected_operation_id")"
  [[ -f "$operation_path" && ! -L "$operation_path" ]] || return 2
  operation="$(cat "$operation_path")"
  jq -e \
    --arg job "$expected_operation_id" \
    --arg kind "$operation_kind" \
    --arg target "$expected_release_id" '
      type == "object"
      and
      .job_id == $job
      and .operation_kind == $kind
      and .action == "apply"
      and .target_release_id == $target
      and (.status | IN("health_checking", "success"))
      and (.base_release_id | type == "string" and test("^release-[A-Za-z0-9-]+$"))
      and (.base_custom_high_water | type == "number" and floor == . and . >= 0)
      and (.advances_custom_version | type == "boolean")
    ' <<< "$operation" >/dev/null || return 2
  jq -e --argjson record "$expected_record" '
    .target_official_version == $record.official_version
    and .target_custom_version == $record.custom_version
    and .target_commit == $record.custom_commit
    and .stable_release_tag == $record.official_version
    and .stable_release_commit == $record.official_commit
    and .main_digest == $record.main_digest
    and .extensions_digest == $record.extensions_digest
  ' <<< "$operation" >/dev/null || return 2
  base_release_id="$(jq -er '.base_release_id' <<< "$operation")" || return 2
  base_high_water="$(jq -er '.base_custom_high_water' <<< "$operation")" || return 2
  advances_custom_version="$(jq -r '.advances_custom_version // false' <<< "$operation")" || return 2
  operation_status="$(jq -er '.status' <<< "$operation")" || return 2
  base_record="$(ledger_release_path "$base_release_id")"
  ledger_validate_release "$base_record" || return 2
  base_sequence="$(jq -er '.custom_version_sequence' "$base_record")" || return 2

  if [[ "$operation_kind" == update ]]; then
    [[ "$(jq -r '.operation_id' <<< "$expected_record")" == "$expected_operation_id" ]] || return 2
    update_kind="$(jq -er '.update_kind' <<< "$operation")" || return 2
    proposed_sequence="$(jq -er '.proposed_custom_sequence' <<< "$operation")" || return 2
    [[ "$update_kind" == "$source_kind" ]] || return 2
	if [[ "$source_kind" == official || "$source_kind" == identity-config ]]; then
      [[ "$advances_custom_version" == false ]] || return 2
      [[ "$target_sequence" -eq "$base_sequence" && "$proposed_sequence" -eq "$target_sequence" ]] || return 2
      [[ "$expected_high_water" -eq "$base_high_water" ]] || return 2
	  if [[ "$source_kind" == identity-config ]]; then
	    [[ "$(jq -r '.identity_transition // empty' <<< "$operation")" == "$(jq -r '.identity_transition // empty' <<< "$expected_record")" ]] || return 2
	  fi
    elif [[ "$source_kind" == custom || "$source_kind" == combined ]]; then
      [[ "$advances_custom_version" == true ]] || return 2
      [[ "$target_sequence" -eq $((base_high_water + 1)) && "$proposed_sequence" -eq "$target_sequence" ]] || return 2
      [[ "$expected_high_water" -eq "$target_sequence" ]] || return 2
    else
      return 2
    fi
  else
    [[ "$advances_custom_version" == false ]] || return 2
    [[ "$expected_high_water" -eq "$base_high_water" && "$target_sequence" -le "$base_high_water" ]] || return 2
  fi

  if [[ "$operation_status" == success && "$operation_kind" == update ]]; then
    jq -e --argjson record "$expected_record" '
      .published == true
      and .published_commit == $record.custom_commit
      and .published_at == $record.published_at
      and .production_changed == true
      and .artifact_path == $record.backup_dir
      and .rollback == {attempted:false,succeeded:false,message:""}
    ' <<< "$operation" >/dev/null || return 2
  elif [[ "$operation_status" == success && "$operation_kind" == rollback ]]; then
    jq -e --argjson record "$expected_record" '
      .published == false
      and .published_commit == $record.custom_commit
      and .production_changed == true
      and .target_release_id == $record.release_id
      and (.artifact_path | type == "string" and length > 0)
      and .rollback == {attempted:false,succeeded:false,message:""}
    ' <<< "$operation" >/dev/null || return 2
  fi

  if [[ "$state_release_id" == "$base_release_id" && "$state_high_water" -eq "$base_high_water" && "$state_operation_id" == "$expected_operation_id" ]]; then
    now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    state="$(jq --arg release "$expected_release_id" --arg now "$now" --argjson high_water "$expected_high_water" '
      .current_release_id=$release
      | .custom_version_high_water=$high_water
      | .active_operation_id=null
      | .updated_at=$now
    ' <<< "$state")"
    ledger_atomic_write "$state_path" "$state" || return 2
  elif [[ "$state_release_id" == "$expected_release_id" && "$state_high_water" -eq "$expected_high_water" && -z "$state_operation_id" ]]; then
    now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  else
    return 2
  fi

  if [[ "$operation_status" != success ]]; then
    if [[ "$operation_kind" == update ]]; then
      operation="$(jq --arg now "$now" --arg published "$(jq -r '.published_at' <<< "$expected_record")" \
        --arg commit "$(jq -r '.custom_commit' <<< "$expected_record")" \
        --arg backup "$(jq -r '.backup_dir' <<< "$expected_record")" '
          .status="success"
          | .message="release recovered after exact ledger publication"
          | .ts=$now
          | .updated_at=$now
          | .finished_at=$now
          | .published=true
          | .published_commit=$commit
          | .published_at=$published
          | .production_changed=true
          | .artifact_path=$backup
          | .rollback={attempted:false,succeeded:false,message:""}
        ' <<< "$operation")" || return 2
    else
      operation="$(jq --arg now "$now" --arg commit "$(jq -r '.custom_commit' <<< "$expected_record")" \
        --arg target "$expected_release_id" --arg artifact "$(jq -r '.backup_dir // empty' <<< "$operation")" '
          .status="success" | .message="rollback recovered after exact ledger publication"
          | .ts=$now | .updated_at=$now | .finished_at=$now
          | .published=false | .published_commit=$commit | .production_changed=true
          | .target_release_id=$target | .artifact_path=$artifact
          | .rollback={attempted:false,succeeded:false,message:""}
        ' <<< "$operation")" || return 2
    fi
    ledger_atomic_write "$operation_path" "$operation" || return 2
  fi
}

ledger_recover_or_refuse() {
  ledger_with_lock _ledger_recover_or_refuse_unlocked "$@"
}
