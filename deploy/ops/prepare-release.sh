#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
COMMON_HELPER="${SUB2API_RELEASE_COMMON_HELPER:-$SCRIPT_DIR/release-common.sh}"
LEDGER_HELPER="${SUB2API_RELEASE_LEDGER_HELPER:-$SCRIPT_DIR/release-ledger.sh}"
SYNC_SCRIPT="${SUB2API_SYNC_SCRIPT:-$SCRIPT_DIR/sync-upstream.sh}"
WAIT_ACTIONS_SCRIPT="${SUB2API_WAIT_ACTIONS_SCRIPT:-$SCRIPT_DIR/wait-for-actions.sh}"
VERIFY_IMAGES_SCRIPT="${SUB2API_VERIFY_IMAGES_SCRIPT:-$SCRIPT_DIR/verify-release-images.sh}"
SCOPE_SCRIPT="${SUB2API_SCOPE_SCRIPT:-$SCRIPT_DIR/classify-release-scope.sh}"
PROMOTE_SCRIPT="${SUB2API_PROMOTE_SCRIPT:-$SCRIPT_DIR/promote-release.sh}"
REPO="${SUB2API_REPO:-/root/sub2api}"
OPS_INSTALL_ROOT="${SUB2API_OPS_INSTALL_ROOT:-/opt/sub2api-custom}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
NGINX_VHOST="${SUB2API_NGINX_VHOST:-/etc/nginx/sites-available/sub.ailisten.top}"
ORIGIN_CERT="${SUB2API_ORIGIN_CERT:-/etc/nginx/ssl/ailisten.top.crt}"
ORIGIN_KEY="${SUB2API_ORIGIN_KEY:-/etc/nginx/ssl/ailisten.top.key}"
BACKUP_ROOT="${SUB2API_BACKUP_ROOT:-/var/lib/sub2api-release/backups}"
export SUB2API_RELEASE_BACKUP_ROOT="${SUB2API_RELEASE_BACKUP_ROOT:-$BACKUP_ROOT}"
MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"
LOG="${SUB2API_SYNC_PUBLISH_LOG:-/var/log/sub2api-release.log}"

source "$STATE_HELPER"
source "$COMMON_HELPER"
source "$LEDGER_HELPER"

JOB_ID="${SUB2API_JOB_ID:-${2:-}}"
[[ "${1:-}" == --job-id && -n "$JOB_ID" ]] || { printf 'usage: prepare-release.sh --job-id <job-id>\n' >&2; exit 2; }
release_valid_job_id "$JOB_ID" || { printf 'invalid job id\n' >&2; exit 2; }
JOB_FILE="$(release_job_path "$JOB_ID")"
[[ -r "$JOB_FILE" ]] || { printf 'release job file is missing\n' >&2; exit 1; }
[[ "$(jq -r '.action // empty' "$JOB_FILE")" == prepare ]] || { release_job_fail "$JOB_ID" LEGACY_SINGLE_PHASE_UNSUPPORTED 'Legacy or non-prepare release job rejected'; exit 1; }

fail_prepare() {
  local message="$1" code="${2:-PREPARE_FAILED}" evidence="${3:-}" metadata
  [[ -n "$evidence" ]] || evidence='{}'
  metadata="$(jq -cn --arg code "$code" --argjson evidence "$evidence" \
    '$evidence + {error_code:$code,published:false,production_changed:false}')"
  ledger_settle_pre_mutation_failure "$JOB_ID" failed "$message" "$metadata" || true
  printf '%s\n' "$message" >&2
  exit 1
}
trap 'fail_prepare "unexpected prepare failure at line $LINENO" UNEXPECTED_PREPARE_ERROR' ERR

mkdir -p "$DATA_DIR" "$(dirname "$LOG")"
release_ensure_backup_root || fail_prepare 'root-owned release backup directory is unsafe' BACKUP_PATH_UNSAFE
touch "$LOG"
CURRENT_RENDERED_JSON=''
TARGET_WORKTREE=''
MANIFEST_SOURCE=''
target_worktree_added=false
cleanup_prepare() {
  [[ -z "$CURRENT_RENDERED_JSON" ]] || rm -f "$CURRENT_RENDERED_JSON"
  [[ -z "$MANIFEST_SOURCE" ]] || rm -f "$MANIFEST_SOURCE"
  if [[ "$target_worktree_added" == true && -n "$TARGET_WORKTREE" ]]; then
    git -C "$REPO" worktree remove --force "$TARGET_WORKTREE" >> "$LOG" 2>&1 || true
  fi
}
trap cleanup_prepare EXIT

validate_snapshot_against_base_record() {
  local base_compose="$1" custom_compose="$2" env_file="$3" rendered_json="$4"
  [[ -r "$base_compose" && -r "$custom_compose" && -r "$env_file" ]] || return 1
  [[ "$(sha256sum "$base_compose" | awk '{print $1}')" == "$(jq -r '.base_compose_sha256' <<< "$BASE_RECORD")" ]] || return 1
  [[ "$(sha256sum "$custom_compose" | awk '{print $1}')" == "$(jq -r '.custom_compose_sha256' <<< "$BASE_RECORD")" ]] || return 1
  [[ "$(sha256sum "$env_file" | awk '{print $1}')" == "$(jq -r '.env_sha256' <<< "$BASE_RECORD")" ]] || return 1
  release_env_matches_digest_pair "$env_file" "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
    || return 1
  release_render_explicit_compose "$base_compose" "$custom_compose" "$env_file" "$rendered_json" "$LOG" || return 1
  release_validate_rendered_compose "$rendered_json" \
    "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" || return 1
  [[ "$(sha256sum "$rendered_json" | awk '{print $1}')" == "$(jq -r '.rendered_compose_sha256' <<< "$BASE_RECORD")" ]]
}

capture_data_quality_baseline() {
  local rendered="$1" enabled secret timestamp nonce signature quality missing
  BASELINE_MISSING_GROUP_REQUESTS=0
  BASELINE_DATA_AS_OF=''
  [[ -r "$rendered" ]] || return 1
  enabled="$(jq -r '.services["extensions-self"].environment.ACCOUNT_MONITOR_ENABLED // "false" | ascii_downcase' "$rendered")"
  [[ "$enabled" == false ]] && return 0
  [[ "$enabled" == true ]] || return 1
  secret="$(jq -r '.services["extensions-self"].environment.RISK_CONTROL_INTERNAL_SECRET // empty' "$rendered")"
  [[ -n "$secret" ]] || return 1
  timestamp="$(date +%s)"; nonce="prepare-$JOB_ID-$timestamp"
  signature="$(MONITOR_SECRET="$secret" MONITOR_TIMESTAMP="$timestamp" MONITOR_NONCE="$nonce" python3 -c '
import hashlib, hmac, os
message = (os.environ["MONITOR_TIMESTAMP"] + "\n" + os.environ["MONITOR_NONCE"] + "\n").encode()
print(hmac.new(os.environ["MONITOR_SECRET"].encode(), message, hashlib.sha256).hexdigest())
')" || return 1
  quality="$(docker exec extensions-self wget -qO- -T 10 \
    --header="X-Risk-Timestamp: $timestamp" --header="X-Risk-Nonce: $nonce" \
    --header="X-Risk-Signature: $signature" --header='X-Risk-Actor-ID: 1' \
    http://extensions-self:8090/api/v1/admin/account-monitor/data-quality)" || return 1
  jq -e '.source_connected == true and (.missing_group_requests | type == "number" and floor == . and . >= 0) and (.data_as_of | type == "string" and length > 0)' <<< "$quality" >/dev/null || return 1
  missing="$(jq -r '.missing_group_requests' <<< "$quality")"
  BASELINE_MISSING_GROUP_REQUESTS="$missing"
  BASELINE_DATA_AS_OF="$(jq -r '.data_as_of' <<< "$quality")"
}

LEDGER_STATE_PATH="$(ledger_state_path)"
ledger_validate_state "$LEDGER_STATE_PATH" || fail_prepare 'release ledger state is invalid' LEDGER_INCONSISTENT
BASE_RELEASE_ID="$(jq -r '.current_release_id' "$LEDGER_STATE_PATH")"
BASE_CUSTOM_HIGH_WATER="$(jq -r '.custom_version_high_water' "$LEDGER_STATE_PATH")"
[[ "$(jq -r '.active_operation_id // empty' "$LEDGER_STATE_PATH")" == "$JOB_ID" ]] \
  || fail_prepare 'release ledger does not own this operation' LEDGER_OPERATION_DRIFT
BASE_RECORD_PATH="$(ledger_release_path "$BASE_RELEASE_ID")"
ledger_validate_release "$BASE_RECORD_PATH" || fail_prepare 'current release record is invalid' LEDGER_INCONSISTENT
BASE_RECORD="$(cat "$BASE_RECORD_PATH")"
CURRENT_OFFICIAL_VERSION="$(jq -r '.official_version' <<< "$BASE_RECORD")"
CURRENT_CUSTOM_VERSION="$(jq -r '.custom_version' <<< "$BASE_RECORD")"
CURRENT_CUSTOM_SEQUENCE="$(jq -r '.custom_version_sequence' <<< "$BASE_RECORD")"
PRODUCTION_COMMIT="$(jq -r '.custom_commit' <<< "$BASE_RECORD")"
CURRENT_MAIN_DIGEST="$(jq -r '.main_digest' <<< "$BASE_RECORD")"
CURRENT_EXTENSIONS_DIGEST="$(jq -r '.extensions_digest' <<< "$BASE_RECORD")"
jq -e --argjson record "$BASE_RECORD" '
  .release_id == $record.release_id
  and .production_commit == $record.custom_commit
  and .stable_release_tag == $record.official_version
  and .stable_release_commit == $record.official_commit
  and .main_digest == $record.main_digest
  and .extensions_digest == $record.extensions_digest
  and .official_version == $record.official_version
  and .custom_version == $record.custom_version
  and .custom_version_sequence == $record.custom_version_sequence
' "$PRODUCTION_RELEASE_STATE_FILE" >/dev/null || fail_prepare 'compatibility projection drifted from the ledger' LEDGER_PROJECTION_DRIFT

CURRENT_RENDERED_JSON="$(mktemp "$DATA_DIR/.current-compose-$JOB_ID.XXXXXX")"
validate_snapshot_against_base_record "$COMPOSE_BASE" "$COMPOSE_CUSTOM" "$ENV_FILE" "$CURRENT_RENDERED_JSON" \
  || fail_prepare 'production Compose, environment, digest pair, or rendered contract drifted from the ledger' CURRENT_SNAPSHOT_DRIFT
capture_data_quality_baseline "$CURRENT_RENDERED_JSON" \
  || fail_prepare 'production data-quality baseline is unavailable or invalid' DATA_QUALITY_BASELINE_FAILED
rm -f "$CURRENT_RENDERED_JSON"
CURRENT_RENDERED_JSON=''

release_validate_installed_ops_at_commit "$REPO" "$PRODUCTION_COMMIT" "$OPS_INSTALL_ROOT" \
  || fail_prepare 'installed release scripts do not match the ledger production commit' HOST_OPS_DRIFT

CACHED_OPERATION="$(cat "$JOB_FILE")"
release_job_update "$JOB_ID" resolving_target 'Checking the locked update target' '{}'
SUB2API_PRODUCTION_COMMIT="$PRODUCTION_COMMIT" "$SYNC_SCRIPT" --job-id "$JOB_ID" >> "$LOG" 2>&1 || {
  status="$(jq -r '.status // empty' "$JOB_FILE" 2>/dev/null || true)"
  if [[ "$status" == conflict || "$status" == failed ]]; then
    ledger_recover_pre_mutation_terminal "$JOB_ID" || true
    exit 1
  fi
  fail_prepare 'Stable Release preparation failed' RELEASE_PREPARATION_FAILED
}

TARGET_COMMIT="$(jq -r '.target_commit // empty' "$JOB_FILE")"
BASE_COMMIT="$(jq -r '.base_commit // empty' "$JOB_FILE")"
RELEASE_TAG="$(jq -r '.release_tag // empty' "$JOB_FILE")"
RELEASE_COMMIT="$(jq -r '.release_commit // empty' "$JOB_FILE")"
INTEGRATION_BRANCH="$(jq -r '.integration_branch // empty' "$JOB_FILE")"
UPDATE_KIND="$(jq -r '.update_kind // empty' "$JOB_FILE")"
LOCKED_TARGET_CUSTOM_COMMIT="$(jq -r '.target_custom_commit // empty' "$JOB_FILE")"
CUSTOM_DOCS_ONLY="$(jq -r '.custom_docs_only // false' "$JOB_FILE")"
[[ "$CUSTOM_DOCS_ONLY" == true || "$CUSTOM_DOCS_ONLY" == false ]] || fail_prepare 'custom documentation scope is invalid' UPDATE_CLASSIFICATION_DRIFT
[[ "$TARGET_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail_prepare 'target commit is invalid' INVALID_TARGET_COMMIT
[[ "$BASE_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail_prepare 'resolved custom branch commit is invalid' INVALID_CUSTOM_TARGET
[[ "$LOCKED_TARGET_CUSTOM_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail_prepare 'locked custom branch commit is invalid' INVALID_CUSTOM_TARGET
[[ "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail_prepare 'stable tag is invalid' INVALID_RELEASE_TAG
[[ "$RELEASE_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail_prepare 'stable commit is invalid' INVALID_RELEASE_COMMIT

if [[ "$LOCKED_TARGET_CUSTOM_COMMIT" != "$BASE_COMMIT" ]]; then
  if jq -e \
    --arg base "$BASE_COMMIT" --arg target "$TARGET_COMMIT" --arg locked_custom "$LOCKED_TARGET_CUSTOM_COMMIT" \
    --arg update_kind "$UPDATE_KIND" --arg stable_tag "$RELEASE_TAG" --arg stable_commit "$RELEASE_COMMIT" \
    --argjson custom_docs_only "$CUSTOM_DOCS_ONLY" '
      .images_verified == true
      and .target_commit == $base
      and $target == $base
      and .target_custom_commit == $locked_custom
      and .update_kind == $update_kind
      and (.custom_docs_only // false) == $custom_docs_only
      and .stable_release_tag == $stable_tag
      and .stable_release_commit == $stable_commit
      and (.workflow_url | type == "string" and length > 0)
      and (.main_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
      and (.extensions_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
    ' <<< "$CACHED_OPERATION" >/dev/null; then
    :
  else
    fail_prepare 'custom branch moved after update detection' TARGET_CUSTOM_COMMIT_DRIFT
  fi
fi

[[ "$UPDATE_KIND" == official || "$UPDATE_KIND" == custom || "$UPDATE_KIND" == combined || "$UPDATE_KIND" == docs-only ]] \
  || [[ "$UPDATE_KIND" == none ]] || fail_prepare 'release update kind is invalid' INVALID_UPDATE_KIND
OFFICIAL_CHANGED=false
if [[ "$RELEASE_TAG" != "$CURRENT_OFFICIAL_VERSION" || "$RELEASE_COMMIT" != "$(jq -r '.official_commit' <<< "$BASE_RECORD")" ]]; then
  OFFICIAL_CHANGED=true
fi
CUSTOM_CHANGED=false
[[ "$LOCKED_TARGET_CUSTOM_COMMIT" == "$PRODUCTION_COMMIT" ]] || CUSTOM_CHANGED=true
[[ "$CUSTOM_DOCS_ONLY" != true || ("$OFFICIAL_CHANGED" == true && "$CUSTOM_CHANGED" == true && "$UPDATE_KIND" == official) ]] \
  || fail_prepare 'custom documentation scope contradicts the resolved target' UPDATE_CLASSIFICATION_DRIFT
if [[ "$UPDATE_KIND" == none ]]; then
  [[ "$TARGET_COMMIT" == "$PRODUCTION_COMMIT" && "$OFFICIAL_CHANGED" == false && "$CUSTOM_CHANGED" == false ]] \
    || fail_prepare 'none classification contradicts the resolved target' UPDATE_CLASSIFICATION_DRIFT
  ledger_settle_noop_success "$JOB_ID" 'No release changes detected; production unchanged' \
    '{"published":false,"production_changed":false}' || fail_prepare 'failed to settle no-change operation' LEDGER_SETTLEMENT_FAILED
  exit 0
fi
scope_output="$($SCOPE_SCRIPT "$PRODUCTION_COMMIT" "$TARGET_COMMIT")" || fail_prepare 'release scope classification failed' SCOPE_CLASSIFICATION_FAILED
[[ "$scope_output" == docs_only=true || "$scope_output" == docs_only=false ]] \
  || fail_prepare 'release scope classifier returned invalid evidence' SCOPE_CLASSIFICATION_FAILED
if [[ "$UPDATE_KIND" == docs-only ]]; then
  [[ "$scope_output" == docs_only=true && "$OFFICIAL_CHANGED" == false && "$CUSTOM_CHANGED" == true ]] \
    || fail_prepare 'documentation-only classification contradicts the resolved target' UPDATE_CLASSIFICATION_DRIFT
  ledger_settle_noop_success "$JOB_ID" "Documentation-only commit $TARGET_COMMIT; production unchanged" \
    '{"docs_only":true,"published":false,"production_changed":false}' || fail_prepare 'failed to settle documentation-only operation' LEDGER_SETTLEMENT_FAILED
  exit 0
fi
[[ "$scope_output" == docs_only=false ]] \
  || fail_prepare 'runtime classification contradicts documentation-only scope' UPDATE_CLASSIFICATION_DRIFT
[[ "$UPDATE_KIND" == official || "$UPDATE_KIND" == custom || "$UPDATE_KIND" == combined ]] \
  || fail_prepare 'runtime release classification is inconsistent' INVALID_UPDATE_KIND
case "$UPDATE_KIND" in
  official) [[ "$OFFICIAL_CHANGED" == true && ("$CUSTOM_CHANGED" == false || "$CUSTOM_DOCS_ONLY" == true) ]] || fail_prepare 'official classification does not match resolved identities' UPDATE_CLASSIFICATION_DRIFT ;;
  custom) [[ "$OFFICIAL_CHANGED" == false && "$CUSTOM_CHANGED" == true ]] || fail_prepare 'custom classification does not match resolved identities' UPDATE_CLASSIFICATION_DRIFT ;;
  combined) [[ "$OFFICIAL_CHANGED" == true && "$CUSTOM_CHANGED" == true ]] || fail_prepare 'combined classification does not match resolved identities' UPDATE_CLASSIFICATION_DRIFT ;;
esac

if [[ "$UPDATE_KIND" == official ]]; then
  PROPOSED_CUSTOM_SEQUENCE="$CURRENT_CUSTOM_SEQUENCE"
  ADVANCES_CUSTOM_VERSION=false
else
  PROPOSED_CUSTOM_SEQUENCE=$((BASE_CUSTOM_HIGH_WATER + 1))
  ADVANCES_CUSTOM_VERSION=true
fi
TARGET_OFFICIAL_VERSION="$RELEASE_TAG"
TARGET_CUSTOM_VERSION="v1.0.$PROPOSED_CUSTOM_SEQUENCE"

REUSE_VERIFIED_EVIDENCE=false
if jq -e \
  --arg base_release "$BASE_RELEASE_ID" --argjson base_high_water "$BASE_CUSTOM_HIGH_WATER" \
  --arg update_kind "$UPDATE_KIND" --arg target_commit "$TARGET_COMMIT" \
  --arg target_custom_commit "$LOCKED_TARGET_CUSTOM_COMMIT" \
  --argjson custom_docs_only "$CUSTOM_DOCS_ONLY" \
  --arg stable_tag "$RELEASE_TAG" --arg stable_commit "$RELEASE_COMMIT" \
  --arg target_official "$TARGET_OFFICIAL_VERSION" --arg target_custom "$TARGET_CUSTOM_VERSION" \
  --argjson proposed "$PROPOSED_CUSTOM_SEQUENCE" --argjson advances "$ADVANCES_CUSTOM_VERSION" '
    .images_verified == true
    and .base_release_id == $base_release
    and .base_custom_high_water == $base_high_water
    and .update_kind == $update_kind
    and .target_commit == $target_commit
    and .target_custom_commit == $target_custom_commit
    and (.custom_docs_only // false) == $custom_docs_only
    and .stable_release_tag == $stable_tag
    and .stable_release_commit == $stable_commit
    and .target_official_version == $target_official
    and .target_custom_version == $target_custom
    and .proposed_custom_sequence == $proposed
    and .advances_custom_version == $advances
    and (.workflow_url | type == "string" and length > 0)
    and (.main_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
    and (.extensions_digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
  ' <<< "$CACHED_OPERATION" >/dev/null; then
  MAIN_DIGEST="$(jq -r '.main_digest' <<< "$CACHED_OPERATION")"
  EXTENSIONS_DIGEST="$(jq -r '.extensions_digest' <<< "$CACHED_OPERATION")"
  WORKFLOW_URL="$(jq -r '.workflow_url' <<< "$CACHED_OPERATION")"
  if release_verify_local_image_identity "$MAIN_REPOSITORY" "$MAIN_DIGEST" "$TARGET_COMMIT" "${RELEASE_TAG#v}" \
    && release_verify_local_image_identity "$EXTENSIONS_REPOSITORY" "$EXTENSIONS_DIGEST" "$TARGET_COMMIT" "${RELEASE_TAG#v}"; then
    REUSE_VERIFIED_EVIDENCE=true
  fi
fi

if [[ "$REUSE_VERIFIED_EVIDENCE" == true ]]; then
  release_job_update "$JOB_ID" verifying_images 'Reusing exact verified Actions and OCI evidence' \
    "$(jq -n --arg url "$WORKFLOW_URL" --arg main "$MAIN_DIGEST" --arg ext "$EXTENSIONS_DIGEST" \
      '{workflow_url:$url,main_digest:$main,extensions_digest:$ext,images_verified:true}')"
else
  release_job_update "$JOB_ID" resolving_target "Waiting for Actions on $TARGET_COMMIT" '{}'
  if ! actions_output="$($WAIT_ACTIONS_SCRIPT "$TARGET_COMMIT")"; then
    if jq -e '.ok == false and (.message | type == "string" and length > 0) and (.error_code | type == "string" and length > 0)' \
      <<< "$actions_output" >/dev/null 2>&1; then
      actions_message="$(jq -r '.message' <<< "$actions_output")"
      actions_code="$(jq -r '.error_code' <<< "$actions_output")"
      actions_evidence="$(jq -c '{failed_check,check_url,conclusion,workflow_url}' <<< "$actions_output")"
      fail_prepare "$actions_message" "$actions_code" "$actions_evidence"
    fi
    fail_prepare 'required GitHub Actions checks failed without valid evidence' ACTIONS_EVIDENCE_INVALID
  fi
  jq -e '.ok == true and (.workflow_url | type == "string" and length > 0)' <<< "$actions_output" >/dev/null \
    || fail_prepare 'Actions waiter returned invalid evidence' ACTIONS_EVIDENCE_INVALID
  WORKFLOW_URL="$(jq -r '.workflow_url' <<< "$actions_output")"
  release_job_update "$JOB_ID" verifying_images "Verifying paired GHCR images for $TARGET_COMMIT" "$(jq -n --arg url "$WORKFLOW_URL" '{workflow_url:$url}')"
  images_output="$($VERIFY_IMAGES_SCRIPT "$TARGET_COMMIT" "${RELEASE_TAG#v}")" || fail_prepare 'paired GHCR image verification failed' IMAGES_FAILED
  MAIN_DIGEST="$(awk -F= '$1=="main_digest" {print $2}' <<< "$images_output")"
  EXTENSIONS_DIGEST="$(awk -F= '$1=="extensions_digest" {print $2}' <<< "$images_output")"
  [[ "$MAIN_DIGEST" =~ ^sha256:[0-9a-f]{64}$ && "$EXTENSIONS_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] \
    || fail_prepare 'image verifier returned invalid digest evidence' IMAGE_EVIDENCE_INVALID
fi

if [[ -n "$INTEGRATION_BRANCH" ]]; then
  release_job_update "$JOB_ID" resolving_target "Promoting $INTEGRATION_BRANCH after immutable verification" '{}'
  "$PROMOTE_SCRIPT" "$BASE_COMMIT" "$TARGET_COMMIT" "$INTEGRATION_BRANCH" >> "$LOG" 2>&1 \
    || fail_prepare 'approved custom-release promotion failed' PROMOTION_FAILED
fi

if [[ "$REUSE_VERIFIED_EVIDENCE" != true ]]; then
  release_job_update "$JOB_ID" downloading_images 'Pulling verified immutable images' "$(jq -n --arg main "$MAIN_DIGEST" --arg ext "$EXTENSIONS_DIGEST" '{main_digest:$main,extensions_digest:$ext}')"
  docker pull "$MAIN_REPOSITORY@$MAIN_DIGEST" >> "$LOG" 2>&1 || fail_prepare 'main image pull failed' TARGET_PULL_FAILED
  docker pull "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" >> "$LOG" 2>&1 || fail_prepare 'extensions image pull failed' TARGET_PULL_FAILED
fi

STAMP="$(date -u '+%Y%m%dT%H%M%S%NZ')"
TARGET_RELEASE_ID="release-candidate-$STAMP-${TARGET_COMMIT:0:9}"
BACKUP_DIR="$(mktemp -d "$BACKUP_ROOT/$JOB_ID-$STAMP.XXXXXX")"
TARGET_WORKTREE="$DATA_DIR/.target-$JOB_ID-${TARGET_COMMIT:0:9}"
[[ ! -e "$TARGET_WORKTREE" ]] || fail_prepare 'target staging path already exists' TARGET_STAGE_CONFLICT
git -C "$REPO" worktree add --detach "$TARGET_WORKTREE" "$TARGET_COMMIT" >> "$LOG" 2>&1 \
  || fail_prepare 'target commit staging failed' TARGET_STAGE_FAILED
target_worktree_added=true
[[ -r "$TARGET_WORKTREE/deploy/docker-compose.yml" && -r "$TARGET_WORKTREE/deploy/docker-compose.custom.yml" ]] \
  || fail_prepare 'target Compose pair is missing' TARGET_COMPOSE_MISSING
mkdir -p "$BACKUP_DIR/target"
cp -p "$TARGET_WORKTREE/deploy/docker-compose.yml" "$BACKUP_DIR/target/docker-compose.yml"
cp -p "$TARGET_WORKTREE/deploy/docker-compose.custom.yml" "$BACKUP_DIR/target/docker-compose.custom.yml"
release_stage_target_env "$ENV_FILE" "$BACKUP_DIR/target/.env" \
  "$MAIN_REPOSITORY@$MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  || fail_prepare 'target environment staging failed' TARGET_ENV_FAILED

release_job_update "$JOB_ID" rendering_compose 'Rendering and validating the target Compose pair' '{}'
release_render_explicit_compose "$BACKUP_DIR/target/docker-compose.yml" "$BACKUP_DIR/target/docker-compose.custom.yml" \
  "$BACKUP_DIR/target/.env" "$BACKUP_DIR/target/rendered-compose.json" "$LOG" \
  || fail_prepare 'target Compose rendering failed' COMPOSE_INVALID
release_validate_rendered_compose "$BACKUP_DIR/target/rendered-compose.json" \
  "$MAIN_REPOSITORY@$MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  || fail_prepare 'target Compose contract failed' COMPOSE_CONTRACT_INVALID

release_job_update "$JOB_ID" backing_up 'Backing up the complete production pair' "$(jq -n --arg dir "$BACKUP_DIR" '{backup_dir:$dir}')"
# Complete backup includes the risk database dump (docker exec risk-control-postgres pg_dump)
# through release_create_complete_backup; rollback itself never restores databases.
release_create_complete_backup "$BACKUP_DIR" "$JOB_ID" "$LOG" \
  || fail_prepare 'complete production backup validation failed' BACKUP_CONTRACT_FAILED

CURRENT_RENDERED_JSON="$(mktemp "$DATA_DIR/.backed-up-compose-$JOB_ID.XXXXXX")"
validate_snapshot_against_base_record "$BACKUP_DIR/docker-compose.yml" "$BACKUP_DIR/docker-compose.custom.yml" \
  "$BACKUP_DIR/.env" "$CURRENT_RENDERED_JSON" \
  || fail_prepare 'backed-up production snapshot drifted from the ledger' CURRENT_SNAPSHOT_DRIFT
rm -f "$CURRENT_RENDERED_JSON"
CURRENT_RENDERED_JSON=''

CURRENT_BASE_COMPOSE_SHA256="$(sha256sum "$BACKUP_DIR/docker-compose.yml" | awk '{print $1}')"
CURRENT_CUSTOM_COMPOSE_SHA256="$(sha256sum "$BACKUP_DIR/docker-compose.custom.yml" | awk '{print $1}')"
TARGET_BASE_COMPOSE_SHA256="$(sha256sum "$BACKUP_DIR/target/docker-compose.yml" | awk '{print $1}')"
TARGET_CUSTOM_COMPOSE_SHA256="$(sha256sum "$BACKUP_DIR/target/docker-compose.custom.yml" | awk '{print $1}')"
TARGET_RENDERED_COMPOSE_SHA256="$(sha256sum "$BACKUP_DIR/target/rendered-compose.json" | awk '{print $1}')"
TARGET_ENV_SHA256="$(sha256sum "$BACKUP_DIR/target/.env" | awk '{print $1}')"
TARGET_ARTIFACT_MANIFEST_SHA256="$(sha256sum "$BACKUP_DIR/target/SHA256SUMS" | awk '{print $1}')"
BACKUP_MANIFEST_SHA256="$(sha256sum "$BACKUP_DIR/SHA256SUMS" | awk '{print $1}')"
prepared_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
expires_at="$(date -u -d '+60 minutes' '+%Y-%m-%dT%H:%M:%SZ')"
MANIFEST_DIR="$(release_prepare_manifest_dir "$JOB_ID")" \
  || fail_prepare 'root-owned prepared manifest directory is unsafe' PREPARED_PATH_UNSAFE
MANIFEST_SOURCE="$(mktemp "$MANIFEST_DIR/.release-manifest-source.XXXXXX")" \
  || fail_prepare 'release manifest temporary file could not be created' PREPARED_MANIFEST_INVALID
chmod 0600 "$MANIFEST_SOURCE" \
  || fail_prepare 'release manifest temporary file permissions could not be secured' PREPARED_MANIFEST_INVALID
jq -n \
  --arg operation_kind update --arg update_kind "$UPDATE_KIND" \
  --argjson custom_docs_only "$CUSTOM_DOCS_ONLY" \
  --arg base_release_id "$BASE_RELEASE_ID" --argjson base_high_water "$BASE_CUSTOM_HIGH_WATER" \
  --arg target_release_id "$TARGET_RELEASE_ID" --arg current_official "$CURRENT_OFFICIAL_VERSION" --arg current_custom "$CURRENT_CUSTOM_VERSION" \
  --arg target_official "$TARGET_OFFICIAL_VERSION" --arg target_custom "$TARGET_CUSTOM_VERSION" \
  --argjson proposed_sequence "$PROPOSED_CUSTOM_SEQUENCE" --argjson advances "$ADVANCES_CUSTOM_VERSION" \
  --arg source_commit "$PRODUCTION_COMMIT" --arg target_commit "$TARGET_COMMIT" \
  --arg target_custom_commit "$LOCKED_TARGET_CUSTOM_COMMIT" \
  --arg stable_tag "$RELEASE_TAG" --arg stable_commit "$RELEASE_COMMIT" \
  --arg main_digest "$MAIN_DIGEST" --arg extensions_digest "$EXTENSIONS_DIGEST" \
  --arg current_main_digest "$CURRENT_MAIN_DIGEST" --arg current_extensions_digest "$CURRENT_EXTENSIONS_DIGEST" \
  --arg current_base_sha "$CURRENT_BASE_COMPOSE_SHA256" --arg current_custom_sha "$CURRENT_CUSTOM_COMPOSE_SHA256" \
  --arg target_base_sha "$TARGET_BASE_COMPOSE_SHA256" \
  --arg target_custom_sha "$TARGET_CUSTOM_COMPOSE_SHA256" --arg target_rendered_sha "$TARGET_RENDERED_COMPOSE_SHA256" \
  --arg target_env_sha "$TARGET_ENV_SHA256" --arg target_manifest_sha "$TARGET_ARTIFACT_MANIFEST_SHA256" \
  --arg backup_dir "$BACKUP_DIR" --arg backup_manifest_sha "$BACKUP_MANIFEST_SHA256" \
  --arg baseline_data_as_of "$BASELINE_DATA_AS_OF" --argjson baseline_missing "$BASELINE_MISSING_GROUP_REQUESTS" \
  --arg prepared_at "$prepared_at" --arg expires_at "$expires_at" --arg workflow_url "$WORKFLOW_URL" \
  '{schema_version:1,operation_kind:$operation_kind,update_kind:$update_kind,custom_docs_only:$custom_docs_only,base_release_id:$base_release_id,
    base_custom_high_water:$base_high_water,target_release_id:$target_release_id,
    current_official_version:$current_official,current_custom_version:$current_custom,
    target_official_version:$target_official,target_custom_version:$target_custom,
    proposed_custom_sequence:$proposed_sequence,advances_custom_version:$advances,
    source_commit:$source_commit,target_commit:$target_commit,target_custom_commit:$target_custom_commit,
    stable_release_tag:$stable_tag,stable_release_commit:$stable_commit,
    main_digest:$main_digest,extensions_digest:$extensions_digest,current_main_digest:$current_main_digest,
    current_extensions_digest:$current_extensions_digest,current_base_compose_sha256:$current_base_sha,
    current_custom_compose_sha256:$current_custom_sha,
    target_base_compose_sha256:$target_base_sha,target_custom_compose_sha256:$target_custom_sha,
    target_rendered_compose_sha256:$target_rendered_sha,target_env_sha256:$target_env_sha,
    target_artifact_manifest_sha256:$target_manifest_sha,backup_dir:$backup_dir,backup_manifest_sha256:$backup_manifest_sha,
    baseline_missing_group_requests:$baseline_missing,baseline_data_as_of:$baseline_data_as_of,
    prepared_at:$prepared_at,expires_at:$expires_at,workflow_url:$workflow_url,images_verified:true,
    compose_contract:"deploy-explicit-pair-v1",backup_contract:"complete-paired-snapshot-v1"}' \
  > "$MANIFEST_SOURCE"
release_install_manifest_files "$MANIFEST_DIR" "$MANIFEST_SOURCE" \
  || fail_prepare 'release manifest files could not be installed safely' PREPARED_PATH_UNSAFE
rm -f "$MANIFEST_SOURCE"
MANIFEST_SOURCE=''
manifest_sha="$(awk '{print $1}' "$MANIFEST_DIR/manifest.sha256")"
operation_metadata="$(jq --arg manifest "$MANIFEST_DIR/manifest.json" --arg sha "$manifest_sha" \
  '. + {prepared_manifest:$manifest,prepared_manifest_sha256:$sha,published:false,production_changed:false}' \
  "$MANIFEST_DIR/manifest.json")"
release_job_update "$JOB_ID" validating_backup 'Backup and immutable manifest validated' "$operation_metadata"
release_job_update "$JOB_ID" prepared 'Update prepared; administrator confirmation is required' "$operation_metadata"
