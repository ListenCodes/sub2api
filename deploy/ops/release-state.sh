#!/usr/bin/env bash

SUB2API_DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
RELEASE_LEDGER_ROOT="${SUB2API_RELEASE_LEDGER_ROOT:-$SUB2API_DATA_DIR/release-ledger}"
RELEASE_OPERATIONS_DIR="${SUB2API_RELEASE_OPERATIONS_DIR:-$RELEASE_LEDGER_ROOT/operations}"
LEGACY_RELEASE_JOBS_DIR="${SUB2API_LEGACY_RELEASE_JOBS_DIR:-$SUB2API_DATA_DIR/release-jobs}"
RELEASE_JOBS_DIR="$RELEASE_OPERATIONS_DIR"
CURRENT_RELEASE_JOB_FILE="${SUB2API_CURRENT_RELEASE_JOB_FILE:-$SUB2API_DATA_DIR/release-current-job-id}"
PRODUCTION_RELEASE_STATE_FILE="${SUB2API_RELEASE_STATE_FILE:-$SUB2API_DATA_DIR/release-state.json}"

release_valid_job_id() {
  [[ "${1:-}" =~ ^(update|rollback)-[A-Za-z0-9-]{1,121}$ ]]
}

release_valid_status() {
  case "${1:-}" in
    resolving_target|resolving_snapshot|verifying_snapshot|verifying_images|downloading_images|rendering_compose|backing_up|validating_backup|prepared|apply_queued|validating_manifest|switching_extensions|switching_main|health_checking|rolling_back|success|failed|conflict|expired|drifted|failed_rolled_back|rollback_failed)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

release_terminal_status() {
  case "${1:-}" in
    success|failed|conflict|expired|drifted|failed_rolled_back|rollback_failed) return 0 ;;
    *) return 1 ;;
  esac
}

release_atomic_write() {
  local path="$1"
  local content="$2"
  local directory temporary
  directory="$(dirname "$path")"
  mkdir -p "$directory" || return 1
  temporary="$(mktemp "$directory/.release-state.XXXXXX")" || return 1
  printf '%s\n' "$content" > "$temporary" || { rm -f "$temporary"; return 1; }
  chmod 0644 "$temporary" || { rm -f "$temporary"; return 1; }
  (sync -f "$temporary" 2>/dev/null || sync) || { rm -f "$temporary"; return 1; }
  if ! mv -f "$temporary" "$path"; then
    rm -f "$temporary"
    return 1
  fi
  sync -f "$directory" 2>/dev/null || sync
}

release_job_path() {
  local job_id="$1"
  release_valid_job_id "$job_id" || return 1
  printf '%s/%s.json\n' "$RELEASE_JOBS_DIR" "$job_id"
}

release_legacy_job_path() {
  local job_id="$1"
  release_valid_job_id "$job_id" || return 1
  printf '%s/%s.json\n' "$LEGACY_RELEASE_JOBS_DIR" "$job_id"
}

release_current_job_id() {
  local job_id
  job_id="$(tr -d '[:space:]' < "$CURRENT_RELEASE_JOB_FILE" 2>/dev/null || true)"
  release_valid_job_id "$job_id" || return 1
  printf '%s\n' "$job_id"
}

release_job_init() {
  local job_id="$1"
  local now job_path payload operation_kind
  release_valid_job_id "$job_id" || return 1
  operation_kind="${job_id%%-*}"
  now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  job_path="$(release_job_path "$job_id")"
  payload="$(jq -n \
    --arg job_id "$job_id" \
    --arg operation_kind "$operation_kind" \
    --arg now "$now" \
    '{
      job_id:$job_id,
      operation_kind:$operation_kind,
      action:"",
      status:"resolving_target",
      message:"release job queued",
      ts:$now,
      updated_at:$now,
      started_at:$now,
      finished_at:null,
      integration_branch:"",
      base_commit:"",
      target_commit:"",
      target_custom_commit:"",
      update_kind:"none",
      production_commit:"",
      stable_release_tag:"",
      stable_release_commit:"",
      release_tag:"",
      release_commit:"",
      release_published_at:"",
      workflow_url:"",
      main_digest:"",
      extensions_digest:"",
      conflict_files:[],
      conflict_base:"",
      conflict_upstream:"",
      conflict_release:"",
      conflict_log:"",
      resolution_hint:"",
      artifact_path:"",
      need_restart:false,
      published:false,
      published_commit:"",
      production_changed:false,
      error_code:"",
      failed_check:"",
      check_url:"",
      conclusion:"",
      prepared_manifest:"",
      prepared_manifest_sha256:"",
      prepared_at:"",
      expires_at:"",
      backup_dir:"",
      rollback:{attempted:false,succeeded:false,message:""}
    }')"
  release_atomic_write "$job_path" "$payload"
  release_atomic_write "$CURRENT_RELEASE_JOB_FILE" "$job_id"
}

release_job_update() {
  local job_id="$1"
  local status="$2"
  local message="$3"
  local metadata="${4-}"
  local job_path now finished payload
  [[ -n "$metadata" ]] || metadata='{}'
  release_valid_status "$status" || return 1
  jq -e 'type == "object"' <<< "$metadata" >/dev/null || return 1
  job_path="$(release_job_path "$job_id")" || return 1
  [[ -r "$job_path" ]] || return 1
  now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  finished=null
  if release_terminal_status "$status"; then
    finished="\"$now\""
  fi
  payload="$(jq \
    --arg status "$status" \
    --arg message "$message" \
    --arg now "$now" \
    --argjson finished_at "$finished" \
    --argjson metadata "$metadata" \
    '. * $metadata
      | .status=$status
      | .message=$message
      | .ts=$now
      | .updated_at=$now
      | .finished_at=$finished_at' \
    "$job_path")"
  release_atomic_write "$job_path" "$payload"
}

release_job_read() {
  local job_id="$1"
  local job_path legacy_path
  job_path="$(release_job_path "$job_id")" || return 1
  if [[ ! -r "$job_path" ]]; then
    legacy_path="$(release_legacy_job_path "$job_id")" || return 1
    if [[ -e "$legacy_path" ]]; then
      printf 'LEGACY_SINGLE_PHASE_UNSUPPORTED: legacy release job cannot be resumed\n' >&2
    fi
    return 1
  fi
  jq -e . "$job_path"
}

release_production_state_write() {
  local payload="$1"
  jq -e '
    type == "object"
    and (.production_commit | type == "string" and length == 40)
    and (.stable_release_tag | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
    and (.stable_release_commit | type == "string" and length == 40)
    and (.main_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
    and (.extensions_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
    and (.published_at | type == "string" and length > 0)
    and (.backup_dir | type == "string" and length > 0)
  ' <<< "$payload" >/dev/null || return 1
  release_atomic_write "$PRODUCTION_RELEASE_STATE_FILE" "$payload"
}
