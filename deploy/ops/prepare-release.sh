#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
COMMON_HELPER="${SUB2API_RELEASE_COMMON_HELPER:-$SCRIPT_DIR/release-common.sh}"
SYNC_SCRIPT="${SUB2API_SYNC_SCRIPT:-$SCRIPT_DIR/sync-upstream.sh}"
WAIT_ACTIONS_SCRIPT="${SUB2API_WAIT_ACTIONS_SCRIPT:-$SCRIPT_DIR/wait-for-actions.sh}"
VERIFY_IMAGES_SCRIPT="${SUB2API_VERIFY_IMAGES_SCRIPT:-$SCRIPT_DIR/verify-release-images.sh}"
SCOPE_SCRIPT="${SUB2API_SCOPE_SCRIPT:-$SCRIPT_DIR/classify-release-scope.sh}"
PROMOTE_SCRIPT="${SUB2API_PROMOTE_SCRIPT:-$SCRIPT_DIR/promote-release.sh}"
REPO="${SUB2API_REPO:-/root/sub2api}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
NGINX_VHOST="${SUB2API_NGINX_VHOST:-/etc/nginx/sites-available/sub.ailisten.top}"
ORIGIN_CERT="${SUB2API_ORIGIN_CERT:-/etc/nginx/ssl/ailisten.top.crt}"
ORIGIN_KEY="${SUB2API_ORIGIN_KEY:-/etc/nginx/ssl/ailisten.top.key}"
BACKUP_ROOT="${SUB2API_BACKUP_ROOT:-$DATA_DIR/release-backups}"
MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"
LOG="${SUB2API_SYNC_PUBLISH_LOG:-/var/log/sub2api-release.log}"

source "$STATE_HELPER"
source "$COMMON_HELPER"

JOB_ID="${SUB2API_JOB_ID:-${2:-}}"
[[ "${1:-}" == --job-id && -n "$JOB_ID" ]] || { printf 'usage: prepare-release.sh --job-id <job-id>\n' >&2; exit 2; }
release_valid_job_id "$JOB_ID" || { printf 'invalid job id\n' >&2; exit 2; }
JOB_FILE="$(release_job_path "$JOB_ID")"
[[ -r "$JOB_FILE" ]] || { printf 'release job file is missing\n' >&2; exit 1; }
[[ "$(jq -r '.action // empty' "$JOB_FILE")" == prepare ]] || { release_job_fail "$JOB_ID" LEGACY_SINGLE_PHASE_UNSUPPORTED 'Legacy or non-prepare release job rejected'; exit 1; }

fail_prepare() {
  local message="$1" code="${2:-PREPARE_FAILED}"
  release_job_update "$JOB_ID" failed "$message" "$(jq -n --arg code "$code" '{error_code:$code,production_changed:false}')" || true
  printf '%s\n' "$message" >&2
  exit 1
}
trap 'fail_prepare "unexpected prepare failure at line $LINENO" UNEXPECTED_PREPARE_ERROR' ERR

mkdir -p "$DATA_DIR" "$BACKUP_ROOT" "$(dirname "$LOG")"
touch "$LOG"
release_job_update "$JOB_ID" checking_updates 'Checking the locked update target' '{}'

"$SYNC_SCRIPT" --job-id "$JOB_ID" >> "$LOG" 2>&1 || {
  status="$(jq -r '.status // empty' "$JOB_FILE" 2>/dev/null || true)"
  [[ "$status" == conflict || "$status" == failed ]] && exit 1
  fail_prepare 'Stable Release preparation failed' RELEASE_PREPARATION_FAILED
}

PRODUCTION_COMMIT="$(jq -r '.production_commit // empty' "$PRODUCTION_RELEASE_STATE_FILE" 2>/dev/null || true)"
TARGET_COMMIT="$(jq -r '.target_commit // empty' "$JOB_FILE")"
BASE_COMMIT="$(jq -r '.base_commit // empty' "$JOB_FILE")"
RELEASE_TAG="$(jq -r '.release_tag // empty' "$JOB_FILE")"
RELEASE_COMMIT="$(jq -r '.release_commit // empty' "$JOB_FILE")"
INTEGRATION_BRANCH="$(jq -r '.integration_branch // empty' "$JOB_FILE")"
[[ "$TARGET_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail_prepare 'target commit is invalid' INVALID_TARGET_COMMIT
[[ "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail_prepare 'stable tag is invalid' INVALID_RELEASE_TAG
[[ "$RELEASE_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail_prepare 'stable commit is invalid' INVALID_RELEASE_COMMIT

scope_base="$PRODUCTION_COMMIT"
[[ "$scope_base" =~ ^[0-9a-f]{40}$ ]] || scope_base="$BASE_COMMIT"
scope_output="$($SCOPE_SCRIPT "$scope_base" "$TARGET_COMMIT")" || fail_prepare 'release scope classification failed' SCOPE_CLASSIFICATION_FAILED
if [[ "$scope_output" == docs_only=true ]]; then
  release_job_update "$JOB_ID" success "Documentation-only commit $TARGET_COMMIT; production unchanged" '{"docs_only":true,"published":false,"production_changed":false}'
  exit 0
fi

release_job_update "$JOB_ID" waiting_actions "Waiting for Actions on $TARGET_COMMIT" '{}'
actions_output="$($WAIT_ACTIONS_SCRIPT "$TARGET_COMMIT")" || fail_prepare 'required GitHub Actions checks failed' ACTIONS_FAILED
WORKFLOW_URL="${actions_output#workflow_url=}"
[[ "$actions_output" == workflow_url=* && -n "$WORKFLOW_URL" ]] || fail_prepare 'Actions waiter returned invalid evidence' ACTIONS_EVIDENCE_INVALID
release_job_update "$JOB_ID" downloading_images "Verifying paired GHCR images for $TARGET_COMMIT" "$(jq -n --arg url "$WORKFLOW_URL" '{workflow_url:$url}')"
images_output="$($VERIFY_IMAGES_SCRIPT "$TARGET_COMMIT" "${RELEASE_TAG#v}")" || fail_prepare 'paired GHCR image verification failed' IMAGES_FAILED
MAIN_DIGEST="$(awk -F= '$1=="main_digest" {print $2}' <<< "$images_output")"
EXTENSIONS_DIGEST="$(awk -F= '$1=="extensions_digest" {print $2}' <<< "$images_output")"
[[ "$MAIN_DIGEST" =~ ^sha256:[0-9a-f]{64}$ && "$EXTENSIONS_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || fail_prepare 'image verifier returned invalid digest evidence' IMAGE_EVIDENCE_INVALID

if [[ -n "$INTEGRATION_BRANCH" ]]; then
  release_job_update "$JOB_ID" promoting_release "Promoting $INTEGRATION_BRANCH after immutable verification" '{}'
  "$PROMOTE_SCRIPT" "$BASE_COMMIT" "$TARGET_COMMIT" "$INTEGRATION_BRANCH" >> "$LOG" 2>&1 || fail_prepare 'approved custom-release promotion failed' PROMOTION_FAILED
fi

release_job_update "$JOB_ID" downloading_images 'Pulling verified immutable images' "$(jq -n --arg main "$MAIN_DIGEST" --arg ext "$EXTENSIONS_DIGEST" '{main_digest:$main,extensions_digest:$ext}')"
docker pull "$MAIN_REPOSITORY@$MAIN_DIGEST" >> "$LOG" 2>&1 || fail_prepare 'main image pull failed' TARGET_PULL_FAILED
docker pull "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" >> "$LOG" 2>&1 || fail_prepare 'extensions image pull failed' TARGET_PULL_FAILED

release_job_update "$JOB_ID" preparing_compose 'Rendering and validating the explicit Compose pair' '{}'
[[ -r "$COMPOSE_BASE" && -r "$COMPOSE_CUSTOM" && -r "$ENV_FILE" ]] || fail_prepare 'production Compose pair or environment is missing' COMPOSE_INPUT_MISSING
COMPOSE_JSON="$DATA_DIR/.prepared-$JOB_ID-compose.json"
SUB2API_IMAGE="$MAIN_REPOSITORY@$MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" config --quiet >> "$LOG" 2>&1 \
  || fail_prepare 'Compose config validation failed' COMPOSE_INVALID
SUB2API_IMAGE="$MAIN_REPOSITORY@$MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" config --format json > "$COMPOSE_JSON" \
  || fail_prepare 'Compose JSON rendering failed' COMPOSE_INVALID
jq -e --arg main "$MAIN_REPOSITORY@$MAIN_DIGEST" --arg ext "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" '
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
  and ([.services.sub2api.volumes[]?.target] | index("/app/data") != null)
  and ([.services.sub2api.volumes[]?.target] | index("/repo") != null)
  and ([.services.sub2api.volumes[]?.target] | index("/var/run/docker.sock") != null)
  and ((.services.sub2api.networks // {}) | has("sub2api-network"))
  and ((.services["extensions-self"].networks // {}) | has("sub2api-network"))
  and ((.volumes // {}) | has("sub2api_data"))
  and ((.volumes // {}) | has("postgres_data"))
  and ((.volumes // {}) | has("redis_data"))
  and ((.volumes // {}) | has("risk_control_postgres_data"))
' "$COMPOSE_JSON" >/dev/null || fail_prepare 'Compose project, service, mount, network, volume, healthcheck, or digest contract failed' COMPOSE_CONTRACT_INVALID

STAMP="$(date -u '+%Y%m%dT%H%M%SZ')"
BACKUP_DIR="$BACKUP_ROOT/$JOB_ID-$STAMP"
mkdir -p "$BACKUP_DIR"
release_job_update "$JOB_ID" backing_up "Backing up production without changing containers" "$(jq -n --arg dir "$BACKUP_DIR" '{backup_dir:$dir}')"
cp -p "$ENV_FILE" "$BACKUP_DIR/.env"
cp -p "$COMPOSE_BASE" "$BACKUP_DIR/docker-compose.yml"
cp -p "$COMPOSE_CUSTOM" "$BACKUP_DIR/docker-compose.custom.yml"
if [[ -r "$PRODUCTION_RELEASE_STATE_FILE" ]]; then cp -p "$PRODUCTION_RELEASE_STATE_FILE" "$BACKUP_DIR/release-state.json"; fi
for source_path in "$NGINX_VHOST" "$ORIGIN_CERT" "$ORIGIN_KEY"; do
  if [[ -r "$source_path" ]]; then
    cp -p "$source_path" "$BACKUP_DIR/$(basename "$source_path")"
  fi
done
docker inspect sub2api sub2api-postgres redis risk-control-postgres extensions-self > "$BACKUP_DIR/container-metadata.json" 2>/dev/null \
  || fail_prepare 'container metadata backup failed' CONTAINER_METADATA_FAILED
docker image ls --digests --no-trunc > "$BACKUP_DIR/image-metadata.txt" || fail_prepare 'image metadata backup failed' IMAGE_METADATA_FAILED
if [[ -r "$PRODUCTION_RELEASE_STATE_FILE" ]]; then
  old_main_digest="$(jq -r '.main_digest // empty' "$PRODUCTION_RELEASE_STATE_FILE")"
  old_ext_digest="$(jq -r '.extensions_digest // empty' "$PRODUCTION_RELEASE_STATE_FILE")"
  if [[ "$old_main_digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    docker image inspect "$MAIN_REPOSITORY@$old_main_digest" > "$BACKUP_DIR/old-main-image.json" 2>/dev/null || true
    docker image tag "$MAIN_REPOSITORY@$old_main_digest" "sub2api:rollback-$JOB_ID" >> "$LOG" 2>&1 || true
  fi
  if [[ "$old_ext_digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    docker image inspect "$EXTENSIONS_REPOSITORY@$old_ext_digest" > "$BACKUP_DIR/old-extensions-image.json" 2>/dev/null || true
    docker image tag "$EXTENSIONS_REPOSITORY@$old_ext_digest" "extensions-self:rollback-$JOB_ID" >> "$LOG" 2>&1 || true
  fi
fi
printf '%s\n' "$NGINX_VHOST" > "$BACKUP_DIR/nginx-vhost.path"
printf '%s\n' "$ORIGIN_CERT" > "$BACKUP_DIR/origin-cert.path"
printf '%s\n' "$ORIGIN_KEY" > "$BACKUP_DIR/origin-key.path"
printf '%s\n' "sub2api:rollback-$JOB_ID" "extensions-self:rollback-$JOB_ID" > "$BACKUP_DIR/rollback-tags.txt"
docker exec sub2api-postgres pg_dump -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" -Fc > "$BACKUP_DIR/sub2api_db.dump" || fail_prepare 'main database backup failed' MAIN_BACKUP_FAILED
docker exec risk-control-postgres pg_dump -U "${RISK_POSTGRES_USER:-risk_control_app}" -d "${RISK_POSTGRES_DB:-risk_control}" -Fc > "$BACKUP_DIR/risk_control_db.dump" || fail_prepare 'extensions database backup failed' EXTENSIONS_BACKUP_FAILED
docker exec -i sub2api-postgres pg_restore --list < "$BACKUP_DIR/sub2api_db.dump" > "$BACKUP_DIR/sub2api_db.list" || fail_prepare 'main backup validation failed' MAIN_BACKUP_INVALID
docker exec -i risk-control-postgres pg_restore --list < "$BACKUP_DIR/risk_control_db.dump" > "$BACKUP_DIR/risk_control_db.list" || fail_prepare 'extensions backup validation failed' EXTENSIONS_BACKUP_INVALID
docker ps -a --no-trunc > "$BACKUP_DIR/docker-containers.txt"
docker images --digests --no-trunc > "$BACKUP_DIR/docker-images.txt"
find "$BACKUP_DIR" -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > "$BACKUP_DIR/SHA256SUMS"
sha256sum -c "$BACKUP_DIR/SHA256SUMS" >> "$LOG" 2>&1 || fail_prepare 'backup checksum validation failed' BACKUP_CHECKSUM_FAILED

prepared_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
expires_at="$(date -u -d '+15 minutes' '+%Y-%m-%dT%H:%M:%SZ')"
MANIFEST_DIR="$PREPARED_ROOT/$JOB_ID"
mkdir -p "$MANIFEST_DIR"
jq -n \
  --arg production_commit "$PRODUCTION_COMMIT" --arg target_commit "$TARGET_COMMIT" \
  --arg stable_tag "$RELEASE_TAG" --arg stable_commit "$RELEASE_COMMIT" \
  --arg main_digest "$MAIN_DIGEST" --arg extensions_digest "$EXTENSIONS_DIGEST" \
  --arg compose_hash "$(sha256sum "$COMPOSE_BASE" "$COMPOSE_CUSTOM" | sha256sum | awk '{print $1}')" \
  --arg rendered_compose_hash "$(sha256sum "$COMPOSE_JSON" | awk '{print $1}')" \
  --arg env_hash "$(sha256sum "$ENV_FILE" | awk '{print $1}')" \
  --arg backup_dir "$BACKUP_DIR" --arg prepared_at "$prepared_at" --arg expires_at "$expires_at" \
  --arg workflow_url "$WORKFLOW_URL" \
  '{version:1,production_commit:$production_commit,target_commit:$target_commit,stable_tag:$stable_tag,stable_commit:$stable_commit,main_digest:$main_digest,extensions_digest:$extensions_digest,compose_hash:$compose_hash,rendered_compose_hash:$rendered_compose_hash,env_hash:$env_hash,backup_dir:$backup_dir,prepared_at:$prepared_at,expires_at:$expires_at,workflow_url:$workflow_url,images_verified:true,compose_contract:"deploy-explicit-pair-v1",backup_contract:"dual-db-compose-env-nginx-cert-metadata-rollback-v1"}' \
  > "$MANIFEST_DIR/manifest.json"
sha256sum "$MANIFEST_DIR/manifest.json" > "$MANIFEST_DIR/manifest.sha256"
manifest_sha="$(awk '{print $1}' "$MANIFEST_DIR/manifest.sha256")"
release_job_update "$JOB_ID" validating_backup 'Backup and immutable manifest validated' "$(jq -n --arg manifest "$MANIFEST_DIR/manifest.json" --arg sha "$manifest_sha" --arg prepared "$prepared_at" --arg expires "$expires_at" --arg dir "$BACKUP_DIR" --arg main "$MAIN_DIGEST" --arg ext "$EXTENSIONS_DIGEST" '{prepared_manifest:$manifest,prepared_manifest_sha256:$sha,prepared_at:$prepared,expires_at:$expires,backup_dir:$dir,main_digest:$main,extensions_digest:$ext}')"
release_job_update "$JOB_ID" prepared "Update prepared; administrator confirmation is required" "$(jq -n --arg manifest "$MANIFEST_DIR/manifest.json" --arg sha "$manifest_sha" --arg prepared "$prepared_at" --arg expires "$expires_at" --arg dir "$BACKUP_DIR" '{prepared_manifest:$manifest,prepared_manifest_sha256:$sha,prepared_at:$prepared,expires_at:$expires,backup_dir:$dir,published:false,production_changed:false}')"
