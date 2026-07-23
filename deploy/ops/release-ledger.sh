#!/usr/bin/env bash

SUB2API_DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
RELEASE_LEDGER_ROOT="${SUB2API_RELEASE_LEDGER_ROOT:-$SUB2API_DATA_DIR/release-ledger}"
RELEASE_BACKUP_ROOT="${SUB2API_RELEASE_BACKUP_ROOT:-$SUB2API_DATA_DIR/release-backups}"
PRODUCTION_RELEASE_STATE_FILE="${SUB2API_RELEASE_STATE_FILE:-$SUB2API_DATA_DIR/release-state.json}"
RELEASE_LEDGER_LOCK_FILE="${SUB2API_RELEASE_LEDGER_LOCK_FILE:-${SUB2API_SYNC_PUBLISH_LOCK:-/var/lock/sub2api-release.lock}}"

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
  jq -e --arg artifact_root "$RELEASE_BACKUP_ROOT/" '
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
    and (.backup_dir | type == "string" and startswith($artifact_root))
    and (.published_at | fromdateiso8601 > 0)
    and (.source_kind | IN("official", "custom", "combined", "bootstrap"))
    and (.operation_id | type == "string" and length > 0)
  ' "$path" >/dev/null || return 1
  backup_dir="$(jq -er '.backup_dir' "$path")" || return 1
  ledger_canonical_backup_path "$backup_dir" >/dev/null
}

ledger_canonical_backup_path() {
  local candidate="$1" root_real candidate_real
  root_real="$(cd "$RELEASE_BACKUP_ROOT" 2>/dev/null && pwd -P)" || return 1
  candidate_real="$(cd "$candidate" 2>/dev/null && pwd -P)" || return 1
  case "$candidate_real/" in
    "$root_real/"*) printf '%s\n' "$candidate_real" ;;
    *) return 1 ;;
  esac
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
  local function_name="$1" result=0
  shift
  mkdir -p "$(dirname "$RELEASE_LEDGER_LOCK_FILE")"
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
  [[ -z "$current" || "$current" == "$operation_id" ]] || return 1
  [[ "$current" != "$operation_id" ]] || return 0
  now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  content="$(jq --arg operation "$operation_id" --arg now "$now" '.active_operation_id=$operation | .updated_at=$now' <<< "$state")"
  ledger_atomic_write "$state_path" "$content"
}

ledger_set_active_operation() {
  ledger_with_lock _ledger_set_active_operation_unlocked "$1"
}

_ledger_commit_release_unlocked() {
  local record_content="$1" advance_high_water="${2:-0}"
  local release_id sequence operation_id state_path state current_record current_sequence projection now
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
  if [[ "$advance_high_water" == 1 ]]; then
    [[ "$sequence" -eq $(( $(jq -r '.custom_version_high_water' <<< "$state") + 1 )) ]] || return 1
  else
    [[ "$sequence" -eq "$current_sequence" ]] || return 1
  fi
  ledger_create_release "$record_content" || return 1
  projection="$(ledger_projection_for_release "$record_content")" || return 1
  ledger_write_projection "$projection" || return 1
  now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  state="$(jq --arg release "$release_id" --arg now "$now" --argjson sequence "$sequence" --argjson advance "$advance_high_water" '
    .current_release_id=$release
    | .active_operation_id=null
    | .updated_at=$now
    | if $advance == 1 then .custom_version_high_water=$sequence else . end
  ' <<< "$state")"
  ledger_atomic_write "$state_path" "$state"
}

ledger_commit_release() {
  ledger_with_lock _ledger_commit_release_unlocked "$@"
}

_ledger_commit_rollback_unlocked() {
  local target_release_id="$1" operation_id="$2" state_path state target_record projection now high_water
  [[ "$target_release_id" =~ ^release-[A-Za-z0-9-]+$ ]] || return 1
  [[ "$operation_id" =~ ^rollback-[A-Za-z0-9-]+$ ]] || return 1
  state_path="$(ledger_state_path)"
  ledger_validate_state "$state_path" || return 1
  state="$(cat "$state_path")"
  [[ "$(jq -r '.active_operation_id // empty' <<< "$state")" == "$operation_id" ]] || return 1
  target_record="$(ledger_release_path "$target_release_id")"
  ledger_validate_release "$target_record" || return 1
  high_water="$(jq -r '.custom_version_high_water' <<< "$state")"
  [[ "$(jq -r '.custom_version_sequence' "$target_record")" -le "$high_water" ]] || return 1
  projection="$(ledger_projection_for_release "$(cat "$target_record")")" || return 1
  ledger_write_projection "$projection" || return 1
  now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  state="$(jq --arg release "$target_release_id" --arg now "$now" '
    .current_release_id=$release | .active_operation_id=null | .updated_at=$now
  ' <<< "$state")"
  ledger_atomic_write "$state_path" "$state"
}

ledger_commit_rollback() {
  ledger_with_lock _ledger_commit_rollback_unlocked "$@"
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
  advances_custom_version="$(jq -r '.advances_custom_version' <<< "$operation")" || return 2
  operation_status="$(jq -er '.status' <<< "$operation")" || return 2
  base_record="$(ledger_release_path "$base_release_id")"
  ledger_validate_release "$base_record" || return 2
  base_sequence="$(jq -er '.custom_version_sequence' "$base_record")" || return 2

  if [[ "$operation_kind" == update ]]; then
    [[ "$(jq -r '.operation_id' <<< "$expected_record")" == "$expected_operation_id" ]] || return 2
    update_kind="$(jq -er '.update_kind' <<< "$operation")" || return 2
    proposed_sequence="$(jq -er '.proposed_custom_sequence' <<< "$operation")" || return 2
    [[ "$update_kind" == "$source_kind" ]] || return 2
    if [[ "$source_kind" == official ]]; then
      [[ "$advances_custom_version" == false ]] || return 2
      [[ "$target_sequence" -eq "$base_sequence" && "$proposed_sequence" -eq "$target_sequence" ]] || return 2
      [[ "$expected_high_water" -eq "$base_high_water" ]] || return 2
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
    operation="$(jq --arg now "$now" '.status="success" | .message="release recovered after exact ledger publication" | .ts=$now | .updated_at=$now | .finished_at=$now' <<< "$operation")" || return 2
    ledger_atomic_write "$operation_path" "$operation" || return 2
  fi
}

ledger_recover_or_refuse() {
  ledger_with_lock _ledger_recover_or_refuse_unlocked "$@"
}
