#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
COMMON_HELPER="${SUB2API_RELEASE_COMMON_HELPER:-$SCRIPT_DIR/release-common.sh}"
REPO="${SUB2API_REPO:-/root/sub2api}"
BRANCH="${SUB2API_BRANCH:-custom-release}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"
INTERNAL_HEALTH_URL="${SUB2API_INTERNAL_HEALTH_URL:-http://127.0.0.1:8080/health}"
PUBLIC_HEALTH_URL="${SUB2API_PUBLIC_HEALTH_URL:-https://sub.ailisten.top/health}"
ADMIN_HEALTH_URL="${SUB2API_ADMIN_HEALTH_URL:-http://127.0.0.1:8080/admin}"
EXTENSION_ROUTE_URL="${SUB2API_EXTENSION_ROUTE_URL:-http://127.0.0.1:8080/admin/extensions/account-monitor}"
DATA_QUALITY_URL="${SUB2API_DATA_QUALITY_URL:-}"
LOG="${SUB2API_SYNC_PUBLISH_LOG:-/var/log/sub2api-release.log}"
HEALTH_WAIT_TIMEOUT_SECONDS="${SUB2API_HEALTH_WAIT_TIMEOUT_SECONDS:-180}"
HEALTH_WAIT_INTERVAL_SECONDS="${SUB2API_HEALTH_WAIT_INTERVAL_SECONDS:-2}"

source "$STATE_HELPER"
source "$COMMON_HELPER"

JOB_ID="${SUB2API_JOB_ID:-${2:-}}"
[[ "${1:-}" == --job-id && -n "$JOB_ID" ]] || { printf 'usage: apply-release.sh --job-id <job-id>\n' >&2; exit 2; }
release_valid_job_id "$JOB_ID" || { printf 'invalid job id\n' >&2; exit 2; }
JOB_FILE="$(release_job_path "$JOB_ID")"
[[ -r "$JOB_FILE" ]] || { printf 'release job is missing\n' >&2; exit 1; }
[[ "$(jq -r '.action // empty' "$JOB_FILE")" == apply ]] || { release_job_fail "$JOB_ID" LEGACY_SINGLE_PHASE_UNSUPPORTED 'Legacy or non-apply release job rejected'; exit 1; }

fail_apply() {
  local message="$1" code="${2:-APPLY_FAILED}"
  release_job_update "$JOB_ID" failed "$message" "$(jq -n --arg code "$code" '{error_code:$code,production_changed:false,rollback:{attempted:false,succeeded:false,message:""}}')" || true
  printf '%s\n' "$message" >&2
  exit 1
}
trap 'fail_apply "unexpected apply failure at line $LINENO" UNEXPECTED_APPLY_ERROR' ERR

manifest="$(release_manifest_path "$JOB_ID")"
manifest_check=0
release_manifest_valid "$JOB_ID" || manifest_check=$?
if [[ "$manifest_check" -eq 2 ]]; then
  release_job_update "$JOB_ID" expired 'Prepared update expired; prepare again' '{"production_changed":false,"published":false,"error_code":"PREPARED_EXPIRED"}'
  exit 1
elif [[ "$manifest_check" -ne 0 ]]; then
  fail_apply 'Prepared manifest is missing or invalid' PREPARED_MANIFEST_INVALID
fi

TARGET_COMMIT="$(jq -r '.target_commit' "$manifest")"
PRODUCTION_COMMIT="$(jq -r '.production_commit' "$manifest")"
MAIN_DIGEST="$(jq -r '.main_digest' "$manifest")"
EXTENSIONS_DIGEST="$(jq -r '.extensions_digest' "$manifest")"
BACKUP_DIR="$(jq -r '.backup_dir' "$manifest")"
CURRENT_COMMIT="$(jq -r '.production_commit // empty' "$PRODUCTION_RELEASE_STATE_FILE" 2>/dev/null || true)"
[[ "$CURRENT_COMMIT" == "$PRODUCTION_COMMIT" ]] || {
  release_job_update "$JOB_ID" drifted 'Production commit changed since preparation; prepare again' '{"error_code":"PRODUCTION_COMMIT_DRIFT","production_changed":false}'
  exit 1
}
ORIGIN_COMMIT="$(git -C "$REPO" rev-parse "origin/$BRANCH" 2>/dev/null || true)"
[[ "$ORIGIN_COMMIT" == "$TARGET_COMMIT" ]] || {
  release_job_update "$JOB_ID" drifted 'origin/custom-release changed since preparation; prepare again' '{"error_code":"ORIGIN_HEAD_DRIFT","production_changed":false}'
  exit 1
}
[[ -r "$ENV_FILE" && -r "$COMPOSE_BASE" && -r "$COMPOSE_CUSTOM" ]] || fail_apply 'Current Compose pair or environment is missing' COMPOSE_INPUT_MISSING
COMPOSE_HASH="$(sha256sum "$COMPOSE_BASE" "$COMPOSE_CUSTOM" | sha256sum | awk '{print $1}')"
ENV_HASH="$(sha256sum "$ENV_FILE" | awk '{print $1}')"
[[ "$COMPOSE_HASH" == "$(jq -r '.compose_hash' "$manifest")" ]] || {
  release_job_update "$JOB_ID" drifted 'Compose pair changed since preparation; prepare again' '{"error_code":"COMPOSE_DRIFT","production_changed":false}'
  exit 1
}
[[ "$ENV_HASH" == "$(jq -r '.env_hash' "$manifest")" ]] || {
  release_job_update "$JOB_ID" drifted '.env changed since preparation; prepare again' '{"error_code":"ENV_DRIFT","production_changed":false}'
  exit 1
}
[[ -r "$BACKUP_DIR/SHA256SUMS" ]] || fail_apply 'Prepared backup is missing' BACKUP_MISSING
sha256sum -c "$BACKUP_DIR/SHA256SUMS" >> "$LOG" 2>&1 || fail_apply 'Prepared backup checksum no longer matches' BACKUP_DRIFT
for image_ref in "$MAIN_REPOSITORY@$MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST"; do
  image_repo="${image_ref%@*}"
  image_digest="${image_ref#*@}"
  repo_digests="$(docker image inspect "$image_ref" --format '{{json .RepoDigests}}' 2>/dev/null || true)"
  jq -e --arg canonical "$image_ref" 'index($canonical) != null' <<< "$repo_digests" >/dev/null \
    || fail_apply "prepared immutable image is missing locally: $image_repo@$image_digest" PREPARED_IMAGE_DRIFT
done

SOURCE_HEAD="$(git -C "$REPO" rev-parse HEAD 2>/dev/null || true)"
[[ "$SOURCE_HEAD" == "$PRODUCTION_COMMIT" ]] || fail_apply 'production source HEAD changed since preparation' SOURCE_HEAD_DRIFT
[[ -z "$(git -C "$REPO" status --porcelain --untracked-files=all)" ]] || fail_apply 'production source worktree is dirty; prepare again' SOURCE_WORKTREE_DIRTY
release_job_update "$JOB_ID" apply_queued 'Prepared manifest revalidated; switching production locally' '{}'
git -C "$REPO" merge --ff-only "$TARGET_COMMIT" >> "$LOG" 2>&1 || fail_apply 'Production source could not fast-forward to prepared commit' SOURCE_FAST_FORWARD_FAILED

pin_image_env() {
  local key="$1" value="$2"
  if grep -q "^${key}=" "$ENV_FILE"; then
    sed -i "s#^${key}=.*#${key}=${value}#" "$ENV_FILE"
  else
    printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
  fi
}

rollback_started=0
rollback_message=''

wait_container_healthy() {
  local container="$1" deadline status
  deadline=$((SECONDS + HEALTH_WAIT_TIMEOUT_SECONDS))
  while (( SECONDS < deadline )); do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container" 2>/dev/null || true)"
    case "$status" in
      healthy) return 0 ;;
      unhealthy|exited|dead) return 1 ;;
    esac
    sleep "$HEALTH_WAIT_INTERVAL_SECONDS"
  done
  return 1
}

restore_source() {
  local source_head="$1"
  [[ "$source_head" =~ ^[0-9a-f]{40}$ ]] || return 1
  [[ "$(git -C "$REPO" branch --show-current)" == "$BRANCH" ]] || return 1
  # Move the checked-out branch pointer only while detached. This avoids
  # git reset --hard while restoring the exact pre-apply source tree.
  git -C "$REPO" switch --detach "$source_head" >> "$LOG" 2>&1 || return 1
  git -C "$REPO" branch -f "$BRANCH" "$source_head" >> "$LOG" 2>&1 || return 1
  git -C "$REPO" switch "$BRANCH" >> "$LOG" 2>&1 || return 1
}

rollback_on_error() {
  local code="$?"
  trap - ERR
  if [[ "$rollback_started" -eq 1 ]]; then
    rollback_message='automatic rollback attempted using the prepared local images, source, and Compose pair'
    release_job_update "$JOB_ID" rolling_back 'Production switch failed; restoring prepared rollback pair' "$(jq -n --arg message "$rollback_message" '{rollback:{attempted:true,succeeded:false,message:$message}}')" || true
    old_main="$(jq -r '.main_digest // empty' "$PRODUCTION_RELEASE_STATE_FILE" 2>/dev/null || true)"
    old_ext="$(jq -r '.extensions_digest // empty' "$PRODUCTION_RELEASE_STATE_FILE" 2>/dev/null || true)"
    rollback_ok=0
    if restore_source "$SOURCE_HEAD" \
      && [[ -r "$BACKUP_DIR/docker-compose.yml" && -r "$BACKUP_DIR/docker-compose.custom.yml" && -r "$BACKUP_DIR/.env" ]] \
      && cp -p "$BACKUP_DIR/docker-compose.yml" "$COMPOSE_BASE" \
      && cp -p "$BACKUP_DIR/docker-compose.custom.yml" "$COMPOSE_CUSTOM" \
      && cp -p "$BACKUP_DIR/.env" "$ENV_FILE" \
      && [[ "$old_main" =~ ^sha256:[0-9a-f]{64}$ && "$old_ext" =~ ^sha256:[0-9a-f]{64}$ ]] \
      && SUB2API_IMAGE="$MAIN_REPOSITORY@$old_main" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$old_ext" \
        docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" up -d --pull never --no-deps --force-recreate extensions-self sub2api >> "$LOG" 2>&1 \
      && wait_container_healthy extensions-self \
      && wait_container_healthy sub2api; then
      rollback_ok=1
    fi
    if [[ "$rollback_ok" -eq 1 ]]; then
      release_job_update "$JOB_ID" failed 'Production switch failed and previous source, Compose, and digest pair was restored' "$(jq -n '{error_code:"APPLY_FAILED_ROLLED_BACK",production_changed:false,rollback:{attempted:true,succeeded:true,message:"automatic rollback completed"}}')" || true
    else
      release_job_update "$JOB_ID" failed 'Production switch failed and automatic rollback was incomplete' "$(jq -n '{error_code:"APPLY_FAILED_ROLLBACK_FAILED",production_changed:false,rollback:{attempted:true,succeeded:false,message:"automatic rollback failed; inspect the prepared backup"}}')" || true
    fi
  fi
  exit "$code"
}
trap 'rollback_on_error' ERR

rollback_started=1
pin_image_env SUB2API_IMAGE "$MAIN_REPOSITORY@$MAIN_DIGEST"
pin_image_env EXTENSIONS_SELF_IMAGE "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST"
release_job_update "$JOB_ID" deploying_extensions "Switching extensions to $EXTENSIONS_DIGEST" '{}'
SUB2API_IMAGE="$MAIN_REPOSITORY@$MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" up -d --pull never --no-deps --force-recreate extensions-self >> "$LOG" 2>&1
wait_container_healthy extensions-self

release_job_update "$JOB_ID" deploying_main "Switching main application to $MAIN_DIGEST" '{}'
SUB2API_IMAGE="$MAIN_REPOSITORY@$MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" up -d --pull never --no-deps --force-recreate sub2api >> "$LOG" 2>&1
wait_container_healthy sub2api

release_job_update "$JOB_ID" health_checking 'Checking internal, public, admin, extension, and data-quality health' '{}'
docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" ps --status running >/dev/null
if [[ "${SUB2API_SKIP_EXTERNAL_HEALTH_CHECKS:-0}" != 1 ]]; then
  curl -fsS --max-time 15 "$INTERNAL_HEALTH_URL" >/dev/null || fail_apply 'internal health check failed' INTERNAL_HEALTH_FAILED
  curl -fsS --max-time 15 "$PUBLIC_HEALTH_URL" >/dev/null || fail_apply 'public health check failed' PUBLIC_HEALTH_FAILED
  curl -fsS --max-time 15 "$ADMIN_HEALTH_URL" >/dev/null || fail_apply 'native admin page health check failed' ADMIN_HEALTH_FAILED
  curl -fsS --max-time 15 "$EXTENSION_ROUTE_URL" >/dev/null || fail_apply 'extension route health check failed' EXTENSION_ROUTE_HEALTH_FAILED
  if [[ -n "$DATA_QUALITY_URL" ]]; then
    curl -fsS --max-time 15 "$DATA_QUALITY_URL" >/dev/null || fail_apply 'data-quality health check failed' DATA_QUALITY_HEALTH_FAILED
  fi
fi

published_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
production_state="$(jq -n --arg production_commit "$TARGET_COMMIT" --arg stable_release_tag "$(jq -r '.stable_tag' "$manifest")" --arg stable_release_commit "$(jq -r '.stable_commit' "$manifest")" --arg main_digest "$MAIN_DIGEST" --arg extensions_digest "$EXTENSIONS_DIGEST" --arg published_at "$published_at" --arg backup_dir "$BACKUP_DIR" '{production_commit:$production_commit,stable_release_tag:$stable_release_tag,stable_release_commit:$stable_release_commit,main_digest:$main_digest,extensions_digest:$extensions_digest,published_at:$published_at,backup_dir:$backup_dir}')"
release_production_state_write "$production_state" || fail_apply 'Could not atomically persist release-state.json' RELEASE_STATE_WRITE_FAILED
release_job_update "$JOB_ID" success "Published prepared commit $TARGET_COMMIT" "$(jq -n --arg commit "$TARGET_COMMIT" --arg main "$MAIN_DIGEST" --arg ext "$EXTENSIONS_DIGEST" --arg dir "$BACKUP_DIR" '{published:true,published_commit:$commit,main_digest:$main,extensions_digest:$ext,production_changed:true,artifact_path:$dir,rollback:{attempted:false,succeeded:false,message:""}}')"
