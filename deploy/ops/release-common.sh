#!/usr/bin/env bash
set -Eeuo pipefail

RELEASE_COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RELEASE_EXECUTOR_UID="$(id -u)"
SUB2API_DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
RELEASE_LEDGER_ROOT="${SUB2API_RELEASE_LEDGER_ROOT:-$SUB2API_DATA_DIR/release-ledger}"
RELEASE_JOBS_DIR="${SUB2API_RELEASE_OPERATIONS_DIR:-$RELEASE_LEDGER_ROOT/operations}"
PREPARED_ROOT="${SUB2API_PREPARED_ROOT:-/var/lib/sub2api-release/prepared}"
RELEASE_BACKUP_ROOT="${SUB2API_RELEASE_BACKUP_ROOT:-/var/lib/sub2api-release/backups}"
PRODUCTION_RELEASE_STATE_FILE="${SUB2API_RELEASE_STATE_FILE:-$SUB2API_DATA_DIR/release-state.json}"
REPO="${SUB2API_REPO:-/root/sub2api}"
BRANCH="${SUB2API_BRANCH:-custom-release}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"

release_manifest_path() {
  local job_id="$1"
  printf '%s/%s/manifest.json\n' "$PREPARED_ROOT" "$job_id"
}

release_manifest_sha_path() {
  local job_id="$1"
  printf '%s/%s/manifest.sha256\n' "$PREPARED_ROOT" "$job_id"
}

release_path_chain_has_no_symlink() {
  local path="$1" current="/" component
  local -a components=()
  [[ "$path" == /* ]] || return 1
  IFS='/' read -r -a components <<< "$path"
  for component in "${components[@]}"; do
    [[ -n "$component" ]] || continue
    current="${current%/}/$component"
    [[ ! -L "$current" ]] || return 1
  done
}

release_path_ancestors_not_writable_by_non_owner() {
  local path="$1" current="/" component mode owner permissions
  local -a components=()
  [[ "$path" == /* ]] || return 1
  IFS='/' read -r -a components <<< "$path"
  for component in "${components[@]}"; do
    [[ -n "$component" ]] || continue
    current="${current%/}/$component"
    [[ ! -e "$current" ]] || {
      mode="$(stat -c '%a' "$current")" || return 1
      owner="$(stat -c '%u' "$current")" || return 1
      [[ "$owner" == 0 || "$owner" == "$RELEASE_EXECUTOR_UID" ]] || return 1
      permissions=$((8#$mode))
      if (( (permissions & 0022) != 0 )); then
        (( owner == 0 && (permissions & 01000) != 0 )) || return 1
      fi
    }
  done
}

release_ensure_prepared_root() {
  local root_real
  release_path_chain_has_no_symlink "$PREPARED_ROOT" || return 1
  release_path_ancestors_not_writable_by_non_owner "$(dirname "$PREPARED_ROOT")" || return 1
  if [[ ! -e "$PREPARED_ROOT" ]]; then
    mkdir -p -m 0700 -- "$PREPARED_ROOT" || return 1
  fi
  release_path_chain_has_no_symlink "$PREPARED_ROOT" || return 1
  release_path_ancestors_not_writable_by_non_owner "$PREPARED_ROOT" || return 1
  [[ -d "$PREPARED_ROOT" && ! -L "$PREPARED_ROOT" ]] || return 1
  [[ "$(stat -c '%u' "$PREPARED_ROOT")" == "$RELEASE_EXECUTOR_UID" ]] || return 1
  [[ "$(stat -c '%a' "$PREPARED_ROOT")" == 700 ]] || return 1
  root_real="$(cd "$PREPARED_ROOT" && pwd -P)" || return 1
  [[ "$root_real" == "$PREPARED_ROOT" ]]
}

release_ensure_backup_root() {
  local root_real
  release_path_chain_has_no_symlink "$RELEASE_BACKUP_ROOT" || return 1
  release_path_ancestors_not_writable_by_non_owner "$(dirname "$RELEASE_BACKUP_ROOT")" || return 1
  if [[ ! -e "$RELEASE_BACKUP_ROOT" ]]; then
    mkdir -p -m 0700 -- "$RELEASE_BACKUP_ROOT" || return 1
  fi
  release_path_chain_has_no_symlink "$RELEASE_BACKUP_ROOT" || return 1
  release_path_ancestors_not_writable_by_non_owner "$RELEASE_BACKUP_ROOT" || return 1
  [[ -d "$RELEASE_BACKUP_ROOT" && ! -L "$RELEASE_BACKUP_ROOT" ]] || return 1
  [[ "$(stat -c '%u' "$RELEASE_BACKUP_ROOT")" == "$RELEASE_EXECUTOR_UID" ]] || return 1
  chmod 0700 "$RELEASE_BACKUP_ROOT" || return 1
  root_real="$(cd "$RELEASE_BACKUP_ROOT" && pwd -P)" || return 1
  [[ "$root_real" == "$RELEASE_BACKUP_ROOT" ]]
}

release_prepare_manifest_dir() {
  local job_id="$1" manifest_dir root_real manifest_real
  release_valid_job_id "$job_id" || return 1
  release_ensure_prepared_root || return 1
  manifest_dir="$PREPARED_ROOT/$job_id"
  if [[ ! -e "$manifest_dir" && ! -L "$manifest_dir" ]]; then
    mkdir -m 0700 -- "$manifest_dir" || return 1
  fi
  release_path_chain_has_no_symlink "$manifest_dir" || return 1
  [[ -d "$manifest_dir" && ! -L "$manifest_dir" ]] || return 1
  [[ "$(stat -c '%u' "$manifest_dir")" == "$RELEASE_EXECUTOR_UID" ]] || return 1
  [[ "$(stat -c '%a' "$manifest_dir")" == 700 ]] || return 1
  root_real="$(cd "$PREPARED_ROOT" && pwd -P)" || return 1
  manifest_real="$(cd "$manifest_dir" && pwd -P)" || return 1
  case "$manifest_real/" in "$root_real/"*) ;; *) return 1 ;; esac
  printf '%s\n' "$manifest_real"
}

release_manifest_targets_safe() {
  local manifest_dir="$1" target
  for target in "$manifest_dir/manifest.json" "$manifest_dir/manifest.sha256"; do
    [[ ! -L "$target" ]] || return 1
    if [[ -e "$target" ]]; then
      [[ -f "$target" ]] || return 1
      [[ "$(stat -c '%u' "$target")" == "$RELEASE_EXECUTOR_UID" ]] || return 1
      [[ "$(stat -c '%a' "$target")" == 600 ]] || return 1
    fi
  done
}

release_install_manifest_files() {
  local manifest_dir="$1" manifest_source="$2" manifest_tmp sha_tmp digest
  local manifest_name="manifest.json" sha_name="manifest.sha256"
  release_manifest_targets_safe "$manifest_dir" || return 1
  [[ -f "$manifest_source" && ! -L "$manifest_source" ]] || return 1
  [[ "$(stat -c '%u' "$manifest_source")" == "$RELEASE_EXECUTOR_UID" ]] || return 1
  [[ "$(stat -c '%a' "$manifest_source")" == 600 ]] || return 1
  manifest_tmp=".manifest.json.$$.${RANDOM:-0}"
  sha_tmp=".manifest.sha256.$$.${RANDOM:-0}"
  (
    cd "$manifest_dir" || exit 1
    set -o noclobber
    : > "$manifest_tmp" || exit 1
    : > "$sha_tmp" || { rm -f -- "$manifest_tmp"; exit 1; }
  ) || return 1
  chmod 0600 "$manifest_dir/$manifest_tmp" "$manifest_dir/$sha_tmp" \
    || { rm -f "$manifest_dir/$manifest_tmp" "$manifest_dir/$sha_tmp"; return 1; }
  cp -- "$manifest_source" "$manifest_dir/$manifest_tmp" \
    || { rm -f "$manifest_dir/$manifest_tmp" "$manifest_dir/$sha_tmp"; return 1; }
  digest="$(sha256sum "$manifest_dir/$manifest_tmp" | awk '{print $1}')" \
    || { rm -f "$manifest_dir/$manifest_tmp" "$manifest_dir/$sha_tmp"; return 1; }
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]] \
    || { rm -f "$manifest_dir/$manifest_tmp" "$manifest_dir/$sha_tmp"; return 1; }
  printf '%s  %s\n' "$digest" "$manifest_name" > "$manifest_dir/$sha_tmp" \
    || { rm -f "$manifest_dir/$manifest_tmp" "$manifest_dir/$sha_tmp"; return 1; }
  (
    cd "$manifest_dir" || exit 1
    mv -f -- "$manifest_tmp" "$manifest_name" \
      && mv -f -- "$sha_tmp" "$sha_name"
  ) \
    || { rm -f "$manifest_dir/$manifest_tmp" "$manifest_dir/$sha_tmp"; return 1; }
  release_manifest_targets_safe "$manifest_dir"
}

release_manifest_valid() {
  local job_id="$1" manifest manifest_dir expected actual expires backup_dir backup_root_real backup_dir_real operation_kind
  release_ensure_prepared_root || return 1
  manifest_dir="$PREPARED_ROOT/$job_id"
  release_path_chain_has_no_symlink "$manifest_dir" || return 1
  [[ -d "$manifest_dir" && ! -L "$manifest_dir" ]] || return 1
  [[ "$(stat -c '%u' "$manifest_dir")" == "$RELEASE_EXECUTOR_UID" ]] || return 1
  [[ "$(stat -c '%a' "$manifest_dir")" == 700 ]] || return 1
  release_manifest_targets_safe "$manifest_dir" || return 1
  manifest="$(release_manifest_path "$job_id")"
  expected="$(cat "$(release_manifest_sha_path "$job_id")" 2>/dev/null | awk '{print $1}' || true)"
  actual="$(sha256sum "$manifest" 2>/dev/null | awk '{print $1}' || true)"
  [[ -n "$expected" && "$expected" == "$actual" ]] || return 1
  operation_kind="$(jq -r '.operation_kind // empty' "$manifest" 2>/dev/null || true)"
  if [[ "$operation_kind" == update ]]; then
    jq -e '
    type == "object"
    and .schema_version == 1
    and .operation_kind == "update"
    and (.update_kind | IN("official", "custom", "combined", "identity-config"))
    and (.base_release_id | type == "string" and test("^release-[A-Za-z0-9-]+$"))
    and (.base_custom_high_water | type == "number" and floor == . and . >= 0)
    and (.target_release_id | type == "string" and test("^release-[A-Za-z0-9-]+$"))
    and (.current_official_version | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
    and (.current_custom_version | type == "string" and test("^v1\\.0\\.[0-9]+$"))
    and (.target_official_version | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
    and (.target_custom_version | type == "string" and test("^v1\\.0\\.[0-9]+$"))
    and (.proposed_custom_sequence | type == "number" and floor == . and . >= 0)
    and (.advances_custom_version | type == "boolean")
    and (.custom_docs_only | type == "boolean")
    and (.source_commit | type == "string" and test("^[0-9a-f]{40}$"))
    and (.target_commit | type == "string" and test("^[0-9a-f]{40}$"))
    and (.target_custom_commit | type == "string" and test("^[0-9a-f]{40}$"))
    and (.stable_release_tag | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
    and (.stable_release_commit | type == "string" and test("^[0-9a-f]{40}$"))
    and (.main_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
    and (.extensions_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
    and (.current_main_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
    and (.current_extensions_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
    and ([.current_base_compose_sha256,.current_custom_compose_sha256,
      .target_base_compose_sha256,.target_custom_compose_sha256,
      .target_rendered_compose_sha256,.target_env_sha256,.target_artifact_manifest_sha256,
      .backup_manifest_sha256] | all(type == "string" and test("^[0-9a-f]{64}$")))
    and (.backup_dir | type == "string" and length > 0)
    and (.prepared_at | fromdateiso8601 > 0)
    and (.expires_at | fromdateiso8601 > 0)
    and (.workflow_url | type == "string" and length > 0)
    and .images_verified == true
    and (if .update_kind == "identity-config" then
      .custom_docs_only == false
      and .advances_custom_version == false
      and .proposed_custom_sequence <= .base_custom_high_water
      and .target_custom_version == ("v1.0." + (.proposed_custom_sequence | tostring))
      and .target_custom_version == .current_custom_version
      and .target_official_version == .current_official_version
      and .target_commit == .source_commit
      and .target_custom_commit == .source_commit
      and .main_digest == .current_main_digest
      and .extensions_digest == .current_extensions_digest
	  and (.current_env_sha256 | type == "string" and test("^[0-9a-f]{64}$"))
	  and .target_env_sha256 != .current_env_sha256
      and (.identity_transition | IN("stage1-v2", "stage1-ip", "stage1-device", "stage2-admin", "stage3-shadow-window", "stage3-rules", "stage4-geo"))
    elif .update_kind == "official" then
      .advances_custom_version == false
      and (if .custom_docs_only then .target_custom_commit != .source_commit else .target_custom_commit == .source_commit end)
      and .target_custom_version == .current_custom_version
      and .target_custom_version == ("v1.0." + (.proposed_custom_sequence | tostring))
    else
      .custom_docs_only == false
      and .advances_custom_version == true
      and .proposed_custom_sequence == (.base_custom_high_water + 1)
      and .target_custom_version == ("v1.0." + (.proposed_custom_sequence | tostring))
    end)
    ' "$manifest" >/dev/null || return 1
  elif [[ "$operation_kind" == rollback ]]; then
    jq -e '
      type == "object"
      and .schema_version == 1
      and .operation_kind == "rollback"
      and (.base_release_id | type == "string" and test("^release-[A-Za-z0-9-]+$"))
      and (.target_release_id | type == "string" and test("^release-[A-Za-z0-9-]+$"))
      and .base_release_id != .target_release_id
      and (.base_custom_high_water | type == "number" and floor == . and . >= 0)
      and (.source_commit | type == "string" and test("^[0-9a-f]{40}$"))
      and (.target_commit | type == "string" and test("^[0-9a-f]{40}$"))
      and (.target_official_version | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
      and (.target_custom_version | type == "string" and test("^v1\\.0\\.[0-9]+$"))
      and (.main_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
      and (.extensions_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
      and (.current_main_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
      and (.current_extensions_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
      and ([.current_base_compose_sha256,.current_custom_compose_sha256,
        .target_base_compose_sha256,.target_custom_compose_sha256,
        .target_rendered_compose_sha256,.target_env_sha256,.target_artifact_manifest_sha256,
        .backup_manifest_sha256] | all(type == "string" and test("^[0-9a-f]{64}$")))
      and (.backup_dir | type == "string" and length > 0)
      and (.prepared_at | fromdateiso8601 > 0)
      and (.expires_at | fromdateiso8601 > 0)
      and .images_verified == true
      and .compose_contract == "deploy-explicit-pair-v1"
      and .backup_contract == "complete-paired-snapshot-v1"
    ' "$manifest" >/dev/null || return 1
  else
    return 1
  fi
  expires="$(jq -r '.expires_at // empty' "$manifest")"
  [[ -n "$expires" ]] || return 1
  backup_dir="$(jq -r '.backup_dir' "$manifest")"
  backup_root_real="$(cd "$RELEASE_BACKUP_ROOT" 2>/dev/null && pwd -P)" || return 1
  backup_dir_real="$(cd "$backup_dir" 2>/dev/null && pwd -P)" || return 1
  case "$backup_dir_real/" in "$backup_root_real/"*) ;; *) return 1 ;; esac
  [[ -s "$backup_dir/SHA256SUMS" && -s "$backup_dir/target/SHA256SUMS" ]] || return 1
  [[ "$(jq -r '.backup_manifest_sha256' "$manifest")" == "$(sha256sum "$backup_dir/SHA256SUMS" | awk '{print $1}')" ]] || return 1
  [[ "$(jq -r '.target_artifact_manifest_sha256' "$manifest")" == "$(sha256sum "$backup_dir/target/SHA256SUMS" | awk '{print $1}')" ]] || return 1
  [[ "$(jq -r '.target_base_compose_sha256' "$manifest")" == "$(sha256sum "$backup_dir/target/docker-compose.yml" | awk '{print $1}')" ]] || return 1
  [[ "$(jq -r '.target_custom_compose_sha256' "$manifest")" == "$(sha256sum "$backup_dir/target/docker-compose.custom.yml" | awk '{print $1}')" ]] || return 1
  [[ "$(jq -r '.target_rendered_compose_sha256' "$manifest")" == "$(sha256sum "$backup_dir/target/rendered-compose.json" | awk '{print $1}')" ]] || return 1
  [[ "$(jq -r '.target_env_sha256' "$manifest")" == "$(sha256sum "$backup_dir/target/.env" | awk '{print $1}')" ]] || return 1
  [[ "$(date -u +%s)" -lt "$(date -u -d "$expires" +%s)" ]] || return 2
}

release_job_fail() {
  local job_id="$1" code="$2" message="$3"
  source "${SUB2API_RELEASE_STATE_HELPER:-$RELEASE_COMMON_DIR/release-state.sh}"
  release_job_update "$job_id" failed "$message" "$(jq -n --arg code "$code" '{error_code:$code,production_changed:false}')" || true
}

release_json_hash() {
  jq -cS . "$1" | sha256sum | awk '{print $1}'
}

release_source_snapshot() {
  local status
  SOURCE_HEAD="$(git -C "$REPO" rev-parse HEAD)" || return 1
  SOURCE_REF="$(git -C "$REPO" symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
  status="$(git -C "$REPO" status --porcelain --untracked-files=all)" || return 1
  [[ -z "$status" ]]
}

release_checkout_exact_commit() {
  local target="$1" status
  [[ "$target" =~ ^[0-9a-f]{40}$ ]] || return 1
  status="$(git -C "$REPO" status --porcelain --untracked-files=all)" || return 1
  [[ -z "$status" ]] || return 1
  git -C "$REPO" cat-file -e "$target^{commit}" >/dev/null 2>&1 || return 1
  git -C "$REPO" switch --detach "$target"
}

release_attach_source_branch() {
  local target="$1" branch="${2:-$BRANCH}" status current_head current_ref
  [[ "$target" =~ ^[0-9a-f]{40}$ ]] || return 1
  [[ "$branch" =~ ^[A-Za-z0-9._/-]+$ && "$branch" != -* ]] || return 1
  status="$(git -C "$REPO" status --porcelain --untracked-files=all)" || return 1
  [[ -z "$status" ]] || return 1
  git -C "$REPO" cat-file -e "$target^{commit}" >/dev/null 2>&1 || return 1
  current_head="$(git -C "$REPO" rev-parse HEAD)" || return 1
  current_ref="$(git -C "$REPO" symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
  [[ "$current_head" != "$target" || "$current_ref" != "$branch" ]] || return 0
  git -C "$REPO" switch -C "$branch" "$target" || return 1
  [[ "$(git -C "$REPO" rev-parse HEAD)" == "$target" ]] || return 1
  [[ "$(git -C "$REPO" symbolic-ref --quiet --short HEAD 2>/dev/null || true)" == "$branch" ]]
}

release_restore_source_snapshot() {
  local source_head="$1" source_ref="${2:-}"
  [[ "$source_head" =~ ^[0-9a-f]{40}$ ]] || return 1
  git -C "$REPO" switch --detach "$source_head" || return 1
  if [[ -n "$source_ref" ]]; then
    [[ "$source_ref" =~ ^[A-Za-z0-9._/-]+$ && "$source_ref" != -* ]] || return 1
    git -C "$REPO" branch -f "$source_ref" "$source_head" || return 1
    git -C "$REPO" switch "$source_ref" || return 1
  fi
}

release_install_snapshot_artifacts() {
  local artifact_dir="$1"
  [[ -s "$artifact_dir/docker-compose.yml" && -s "$artifact_dir/docker-compose.custom.yml" && -s "$artifact_dir/.env" ]] || return 1
  cp -p "$artifact_dir/docker-compose.yml" "$COMPOSE_BASE" || return 1
  cp -p "$artifact_dir/docker-compose.custom.yml" "$COMPOSE_CUSTOM" || return 1
  cp -p "$artifact_dir/.env" "$ENV_FILE" || return 1
  chmod 0600 "$ENV_FILE"
}

release_stage_target_env() {
  local source_env="$1" target_env="$2" main_image="$3" extensions_image="$4" temporary
  [[ -r "$source_env" ]] || return 1
  mkdir -p "$(dirname "$target_env")" || return 1
  temporary="$(mktemp "$(dirname "$target_env")/.target-env.XXXXXX")" || return 1
  awk '!/^SUB2API_IMAGE=/ && !/^EXTENSIONS_SELF_IMAGE=/' "$source_env" > "$temporary" \
    || { rm -f "$temporary"; return 1; }
  printf 'SUB2API_IMAGE=%s\nEXTENSIONS_SELF_IMAGE=%s\n' "$main_image" "$extensions_image" >> "$temporary" \
    || { rm -f "$temporary"; return 1; }
  chmod 0600 "$temporary" || { rm -f "$temporary"; return 1; }
  mv -f "$temporary" "$target_env" || { rm -f "$temporary"; return 1; }
}

release_render_explicit_compose() {
  local base_compose="$1" custom_compose="$2" env_file="$3" rendered_json="$4" log_file="$5"
  docker compose --project-name deploy -f "$base_compose" -f "$custom_compose" --env-file "$env_file" config --quiet >> "$log_file" 2>&1 \
    || return 1
  docker compose --project-name deploy -f "$base_compose" -f "$custom_compose" --env-file "$env_file" config --format json > "$rendered_json" \
    || return 1
}

release_validate_rendered_compose() {
  local rendered_json="$1" main_image="$2" extensions_image="$3"
  jq -e --arg main "$main_image" --arg ext "$extensions_image" '
    def mount_targets:
      ([.services.sub2api.volumes[]?.target] | sort);

    .name == "deploy"
    and ([.services | keys[]] | index("sub2api") != null)
    and ([.services | keys[]] | index("extensions-self") != null)
    and ([.services | keys[]] | index("postgres") != null)
    and ([.services | keys[]] | index("redis") != null)
    and ([.services | keys[]] | index("risk-control-postgres") != null)
    and .services.sub2api.image == $main
    and .services["extensions-self"].image == $ext
    and (.services.sub2api.healthcheck != null)
    and (.services["extensions-self"].healthcheck != null)
    and mount_targets == ([
      "/app/data",
      "/app/scripts/sync-upstream.sh"
    ] | sort)
    and ([.services.sub2api.volumes[]? | select(
      .target == "/app/data"
      and .source == "sub2api_data"
      and .type == "volume"
      and ((.read_only // false) == false)
    )] | length == 1)
    and ([.services.sub2api.volumes[]? | select(
      .target == "/app/scripts/sync-upstream.sh"
      and .source == "/opt/sub2api-custom/sync-trigger.sh"
      and .type == "bind"
      and .read_only == true
    )] | length == 1)
    and ((.services.sub2api.networks // {}) | has("sub2api-network"))
    and ((.services["extensions-self"].networks // {}) | has("sub2api-network"))
    and ((.volumes // {}) | has("sub2api_data"))
    and ((.volumes // {}) | has("postgres_data"))
    and ((.volumes // {}) | has("redis_data"))
    and ((.volumes // {}) | has("risk_control_postgres_data"))
  ' "$rendered_json" >/dev/null
}

release_env_matches_digest_pair() {
  local env_file="$1" main_image="$2" extensions_image="$3"
  [[ "$(sed -n 's/^SUB2API_IMAGE=//p' "$env_file" | tail -n 1)" == "$main_image" ]] || return 1
  [[ "$(sed -n 's/^EXTENSIONS_SELF_IMAGE=//p' "$env_file" | tail -n 1)" == "$extensions_image" ]] || return 1
}

release_validate_snapshot_against_record() {
  local base_compose="$1" custom_compose="$2" env_file="$3" rendered_json="$4" record="$5" log_file="$6"
  local main_image extensions_image
  [[ -r "$base_compose" && -r "$custom_compose" && -r "$env_file" ]] || return 1
  [[ "$(sha256sum "$base_compose" | awk '{print $1}')" == "$(jq -r '.base_compose_sha256' <<< "$record")" ]] || return 1
  [[ "$(sha256sum "$custom_compose" | awk '{print $1}')" == "$(jq -r '.custom_compose_sha256' <<< "$record")" ]] || return 1
  [[ "$(sha256sum "$env_file" | awk '{print $1}')" == "$(jq -r '.env_sha256' <<< "$record")" ]] || return 1
  main_image="$MAIN_REPOSITORY@$(jq -r '.main_digest' <<< "$record")"
  extensions_image="$EXTENSIONS_REPOSITORY@$(jq -r '.extensions_digest' <<< "$record")"
  release_env_matches_digest_pair "$env_file" "$main_image" "$extensions_image" || return 1
  release_render_explicit_compose "$base_compose" "$custom_compose" "$env_file" "$rendered_json" "$log_file" || return 1
  release_validate_rendered_compose "$rendered_json" "$main_image" "$extensions_image" || return 1
  [[ "$(sha256sum "$rendered_json" | awk '{print $1}')" == "$(jq -r '.rendered_compose_sha256' <<< "$record")" ]]
}

release_verify_local_image_identity() {
  local repository="$1" digest="$2" commit="$3" version="$4"
  local canonical labels architecture repo_digests
  canonical="$repository@$digest"
  labels="$(docker image inspect "$canonical" --format '{{json .Config.Labels}}')" || return 1
  [[ "$(jq -r '.["org.opencontainers.image.revision"] // empty' <<< "$labels")" == "$commit" ]] || return 1
  [[ "$(jq -r '.["org.opencontainers.image.version"] // empty' <<< "$labels")" == "$version" ]] || return 1
  [[ "$(jq -r '.["org.opencontainers.image.source"] // empty' <<< "$labels")" == 'https://github.com/ListenCodes/sub2api' ]] || return 1
  architecture="$(docker image inspect "$canonical" --format '{{.Architecture}}')" || return 1
  [[ "$architecture" == amd64 ]] || return 1
  repo_digests="$(docker image inspect "$canonical" --format '{{json .RepoDigests}}')" || return 1
  jq -e --arg canonical "$canonical" 'index($canonical) != null' <<< "$repo_digests" >/dev/null
}

release_running_container_matches_image() {
  local container="$1" canonical="$2" configured_image running_image expected_image
  [[ "$container" == sub2api || "$container" == extensions-self ]] || return 1
  configured_image="$(docker inspect --format '{{.Config.Image}}' "$container")" || return 1
  [[ "$configured_image" == "$canonical" ]] || return 1
  running_image="$(docker inspect --format '{{.Image}}' "$container")" || return 1
  expected_image="$(docker image inspect "$canonical" --format '{{.Id}}')" || return 1
  [[ -n "$expected_image" && "$running_image" == "$expected_image" ]]
}

release_create_complete_backup() {
  local backup_dir="$1" job_id="$2" log_file="$3"
  local old_main_digest old_extensions_digest source_path
  [[ -d "$backup_dir/target" && -s "$backup_dir/target/.env" \
    && -s "$backup_dir/target/docker-compose.yml" && -s "$backup_dir/target/docker-compose.custom.yml" \
    && -s "$backup_dir/target/rendered-compose.json" ]] || return 1
  [[ -r "$ENV_FILE" && -r "$COMPOSE_BASE" && -r "$COMPOSE_CUSTOM" && -r "$PRODUCTION_RELEASE_STATE_FILE" ]] || return 1

  cp -p "$ENV_FILE" "$backup_dir/.env" || return 1
  cp -p "$COMPOSE_BASE" "$backup_dir/docker-compose.yml" || return 1
  cp -p "$COMPOSE_CUSTOM" "$backup_dir/docker-compose.custom.yml" || return 1
  cp -p "$PRODUCTION_RELEASE_STATE_FILE" "$backup_dir/release-state.json" || return 1
  for source_path in "$NGINX_VHOST" "$ORIGIN_CERT" "$ORIGIN_KEY"; do
    [[ -r "$source_path" ]] || return 1
    cp -p "$source_path" "$backup_dir/$(basename "$source_path")" || return 1
  done
  printf '%s\n' "$NGINX_VHOST" > "$backup_dir/nginx-vhost.path" || return 1
  printf '%s\n' "$ORIGIN_CERT" > "$backup_dir/origin-cert.path" || return 1
  printf '%s\n' "$ORIGIN_KEY" > "$backup_dir/origin-key.path" || return 1

  docker inspect sub2api sub2api-postgres sub2api-redis risk-control-postgres extensions-self > "$backup_dir/container-metadata.json" 2>/dev/null \
    || return 1
  docker image ls --digests --no-trunc > "$backup_dir/image-metadata.txt" || return 1
  old_main_digest="$(jq -r '.main_digest // empty' "$PRODUCTION_RELEASE_STATE_FILE")"
  old_extensions_digest="$(jq -r '.extensions_digest // empty' "$PRODUCTION_RELEASE_STATE_FILE")"
  [[ "$old_main_digest" =~ ^sha256:[0-9a-f]{64}$ && "$old_extensions_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
  docker image inspect "$MAIN_REPOSITORY@$old_main_digest" > "$backup_dir/old-main-image.json" 2>/dev/null || return 1
  docker image inspect "$EXTENSIONS_REPOSITORY@$old_extensions_digest" > "$backup_dir/old-extensions-image.json" 2>/dev/null || return 1
  docker image tag "$MAIN_REPOSITORY@$old_main_digest" "sub2api:rollback-$job_id" >> "$log_file" 2>&1 || return 1
  docker image tag "$EXTENSIONS_REPOSITORY@$old_extensions_digest" "extensions-self:rollback-$job_id" >> "$log_file" 2>&1 || return 1
  printf '%s\n' "sub2api:rollback-$job_id" "extensions-self:rollback-$job_id" > "$backup_dir/rollback-tags.txt" || return 1

  docker exec sub2api-postgres pg_dump -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" -Fc > "$backup_dir/sub2api_db.dump" || return 1
  docker exec risk-control-postgres pg_dump -U "${RISK_POSTGRES_USER:-risk_control_app}" -d "${RISK_POSTGRES_DB:-risk_control}" -Fc > "$backup_dir/risk_control_db.dump" || return 1
  docker exec -i sub2api-postgres pg_restore --list < "$backup_dir/sub2api_db.dump" > "$backup_dir/sub2api_db.list" || return 1
  docker exec -i risk-control-postgres pg_restore --list < "$backup_dir/risk_control_db.dump" > "$backup_dir/risk_control_db.list" || return 1
  docker ps -a --no-trunc > "$backup_dir/docker-containers.txt" || return 1
  docker images --digests --no-trunc > "$backup_dir/docker-images.txt" || return 1

  (cd "$backup_dir/target" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS) || return 1
  (cd "$backup_dir" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS) || return 1
  ledger_validate_backup_contract "$backup_dir"
}
