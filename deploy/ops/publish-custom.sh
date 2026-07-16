#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="${SUB2API_REPO:-/root/sub2api}"
BRANCH="${SUB2API_BRANCH:-custom-release}"
ORIGIN_REMOTE="${SUB2API_ORIGIN_REMOTE:-origin}"
UPSTREAM_REMOTE="${SUB2API_UPSTREAM_REMOTE:-upstream}"
ORIGIN_REF="$ORIGIN_REMOTE/$BRANCH"
COMPOSE="$REPO/deploy/docker-compose.yml"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
VERIFY_IMAGES_SCRIPT="${SUB2API_VERIFY_IMAGES_SCRIPT:-$SCRIPT_DIR/verify-release-images.sh}"
MAIN_REPOSITORY="${SUB2API_MAIN_IMAGE_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_IMAGE_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"
BACKUP_ROOT="${SUB2API_BACKUP_ROOT:-/root/backups/sub2api}"
NGINX_VHOST="${SUB2API_NGINX_VHOST:-/etc/nginx/sites-available/sub.ailisten.top}"
PUBLIC_HEALTH_URL="${SUB2API_PUBLIC_HEALTH_URL:-https://sub.ailisten.top/health}"
LOG="${SUB2API_PUBLISH_LOG:-/var/log/sub2api-publish.log}"
STAMP="$(date -u '+%Y%m%d-%H%M%S')"
BACKUP_DIR="$BACKUP_ROOT/$STAMP"
MUTATION_STARTED=0
ROLLBACK_ACTIVE=0
LEGACY_BOOTSTRAP=0
ANONYMOUS_DOCKER_CONFIG=''

source "$STATE_HELPER"

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1" | tee -a "$LOG"
}

cleanup_publisher() {
  [[ -z "$ANONYMOUS_DOCKER_CONFIG" ]] || rm -rf "$ANONYMOUS_DOCKER_CONFIG"
}

prepare_anonymous_docker() {
  ANONYMOUS_DOCKER_CONFIG="$(mktemp -d)"
  printf '{"auths":{}}\n' > "$ANONYMOUS_DOCKER_CONFIG/config.json"
  chmod 0700 "$ANONYMOUS_DOCKER_CONFIG"
  chmod 0600 "$ANONYMOUS_DOCKER_CONFIG/config.json"
}

anonymous_docker() {
  DOCKER_CONFIG="$ANONYMOUS_DOCKER_CONFIG" docker "$@"
}

job_update() {
  local status="$1"
  local message="$2"
  local metadata="${3-}"
  [[ -n "$metadata" ]] || metadata='{}'
  release_job_update "$JOB_ID" "$status" "$message" "$metadata"
}

parse_verified_images() {
  local output="$1"
  local line key value
  local seen_main=0 seen_extensions=0
  VERIFIED_MAIN_DIGEST=''
  VERIFIED_EXTENSIONS_DIGEST=''
  while IFS= read -r line; do
    [[ "$line" == *=* ]] || return 1
    key="${line%%=*}"
    value="${line#*=}"
    case "$key" in
      main_digest) ((seen_main == 0)) || return 1; VERIFIED_MAIN_DIGEST="$value"; seen_main=1 ;;
      extensions_digest) ((seen_extensions == 0)) || return 1; VERIFIED_EXTENSIONS_DIGEST="$value"; seen_extensions=1 ;;
      *) return 1 ;;
    esac
  done <<< "$output"
  [[ "$seen_main" -eq 1 && "$seen_extensions" -eq 1 ]]
}

write_image_environment() {
  local main_ref="$1"
  local extensions_ref="$2"
  local temporary="$ENV_FILE.tmp.$$"
  awk -v main_ref="$main_ref" -v extensions_ref="$extensions_ref" '
    BEGIN { main_seen=0; extensions_seen=0 }
    /^SUB2API_IMAGE=/ { print "SUB2API_IMAGE=" main_ref; main_seen=1; next }
    /^EXTENSIONS_SELF_IMAGE=/ { print "EXTENSIONS_SELF_IMAGE=" extensions_ref; extensions_seen=1; next }
    { print }
    END {
      if (!main_seen) print "SUB2API_IMAGE=" main_ref
      if (!extensions_seen) print "EXTENSIONS_SELF_IMAGE=" extensions_ref
    }
  ' "$ENV_FILE" > "$temporary"
  chmod --reference="$ENV_FILE" "$temporary"
  mv -f "$temporary" "$ENV_FILE"
}

compose_with() {
  local compose_file="$1"
  shift
  docker compose --project-name deploy -f "$compose_file" --env-file "$ENV_FILE" "$@"
}

wait_container_health() {
  local container="$1"
  local attempts="${2:-60}"
  local status
  for _ in $(seq 1 "$attempts"); do
    status="$(docker inspect "$container" --format '{{.State.Health.Status}}' 2>/dev/null || true)"
    [[ "$status" == healthy ]] && return 0
    [[ "$status" == unhealthy ]] && return 1
    sleep 2
  done
  return 1
}

check_monitor_api() {
  [[ "$monitor_enabled" == true ]] || return 0
  local timestamp nonce signature
  timestamp="$(date +%s)"
  nonce="publish-$STAMP-$timestamp"
  signature="$(
    MONITOR_SECRET="$rendered_internal_secret" MONITOR_TIMESTAMP="$timestamp" MONITOR_NONCE="$nonce" python3 -c '
import hashlib, hmac, os
message = (os.environ["MONITOR_TIMESTAMP"] + "\n" + os.environ["MONITOR_NONCE"] + "\n").encode()
print(hmac.new(os.environ["MONITOR_SECRET"].encode(), message, hashlib.sha256).hexdigest())
'
  )"
  docker exec extensions-self wget -qO- -T 5 \
    --header="X-Risk-Timestamp: $timestamp" \
    --header="X-Risk-Nonce: $nonce" \
    --header="X-Risk-Signature: $signature" \
    --header='X-Risk-Actor-ID: 1' \
    http://extensions-self:8090/api/v1/admin/account-monitor/data-quality >/dev/null
}

full_health_check() {
  local name
  for name in sub2api extensions-self risk-control-postgres sub2api-postgres sub2api-redis; do
    [[ "$(docker inspect "$name" --format '{{.State.Health.Status}}' 2>/dev/null || true)" == healthy ]] || return 1
  done
  curl -fsS --max-time 10 http://127.0.0.1:8081/health >/dev/null || return 1
  docker exec extensions-self wget -qO- -T 5 http://extensions-self:8090/healthz >/dev/null || return 1
  curl -fsS --max-time 10 http://127.0.0.1:8081/api/v1/extensions-self/homepage/ >/dev/null || return 1
  check_monitor_api || return 1
  nginx -t >> "$LOG" 2>&1 || return 1
  curl -fsS --max-time 15 "$PUBLIC_HEALTH_URL" >/dev/null || return 1
}

perform_rollback() {
  local reason="$1"
  ROLLBACK_ACTIVE=1
  job_update rolling_back "Restoring previous image digests after: $reason" "$(jq -n --arg message "$reason" '{rollback:{attempted:true,succeeded:false,message:$message}}')" || true
  log "Rolling back to $PREVIOUS_COMMIT"

  cp -p "$BACKUP_DIR/main.env" "$ENV_FILE" || return 1
  if [[ "$LEGACY_BOOTSTRAP" -eq 1 ]]; then
    docker image inspect "$PREVIOUS_MAIN_REF" "$PREVIOUS_EXTENSIONS_REF" >/dev/null || return 1
    ROLLBACK_COMPOSE="$BACKUP_DIR/main-docker-compose.yml"
  else
    anonymous_docker pull "$PREVIOUS_MAIN_REF" >> "$LOG" 2>&1 || return 1
    anonymous_docker pull "$PREVIOUS_EXTENSIONS_REF" >> "$LOG" 2>&1 || return 1
    ROLLBACK_COMPOSE="$COMPOSE"
  fi
  if [[ "$LEGACY_BOOTSTRAP" -eq 0 ]] \
    && grep -q 'SUB2API_IMAGE' "$BACKUP_DIR/main-docker-compose.yml" \
    && grep -q 'EXTENSIONS_SELF_IMAGE' "$BACKUP_DIR/main-docker-compose.yml"; then
    ROLLBACK_COMPOSE="$BACKUP_DIR/main-docker-compose.yml"
  fi
  compose_with "$ROLLBACK_COMPOSE" up -d --no-deps --force-recreate extensions-self >> "$LOG" 2>&1 || return 1
  wait_container_health extensions-self || return 1
  compose_with "$ROLLBACK_COMPOSE" up -d --no-deps --force-recreate sub2api >> "$LOG" 2>&1 || return 1
  wait_container_health sub2api || return 1
  full_health_check || return 1
  return 0
}

abort_release() {
  local reason="$1"
  local error_code="${2:-PUBLICATION_FAILED}"
  trap - ERR
  if [[ "$MUTATION_STARTED" -eq 1 && "$ROLLBACK_ACTIVE" -eq 0 ]]; then
    if perform_rollback "$reason"; then
      metadata="$(jq -n --arg error_code "$error_code" --arg reason "$reason" --arg artifact_path "$BACKUP_DIR" '{error_code:$error_code,artifact_path:$artifact_path,production_changed:false,rollback:{attempted:true,succeeded:true,message:("Restored previous digest pair: "+$reason)}}')"
      job_update failed "$reason; previous digest pair restored" "$metadata" || true
      log "FAILED: $reason; automatic rollback succeeded"
    else
      metadata="$(jq -n --arg error_code "$error_code" --arg reason "$reason" --arg artifact_path "$BACKUP_DIR" '{error_code:$error_code,artifact_path:$artifact_path,production_changed:true,rollback:{attempted:true,succeeded:false,message:("Automatic rollback failed: "+$reason)}}')"
      job_update failed "$reason; automatic rollback failed" "$metadata" || true
      log "FAILED: $reason; automatic rollback failed; backup=$BACKUP_DIR"
    fi
  else
    job_update failed "$reason" "$(jq -n --arg error_code "$error_code" '{error_code:$error_code,production_changed:false}')" || true
    log "FAILED: $reason"
  fi
  exit 1
}

on_error() {
  local code="$?"
  local line="${1:-unknown}"
  abort_release "unexpected publisher error at line $line (exit=$code)" UNEXPECTED_PUBLISHER_ERROR
}

APPROVED_COMMIT=''
MAIN_DIGEST=''
EXTENSIONS_DIGEST=''
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --commit) APPROVED_COMMIT="${2:-}"; shift 2 ;;
    --main-digest) MAIN_DIGEST="${2:-}"; shift 2 ;;
    --extensions-digest) EXTENSIONS_DIGEST="${2:-}"; shift 2 ;;
    *) printf 'Unknown publisher argument: %s\n' "$1" >&2; exit 1 ;;
  esac
done

[[ "$BRANCH" == custom-release ]] || { printf 'Only custom-release may be published\n' >&2; exit 1; }
[[ "$APPROVED_COMMIT" =~ ^[0-9a-f]{40}$ ]] || { printf 'Approved commit must be a full SHA\n' >&2; exit 1; }
[[ "$MAIN_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || { printf 'Main digest is invalid\n' >&2; exit 1; }
[[ "$EXTENSIONS_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || { printf 'Extensions digest is invalid\n' >&2; exit 1; }
JOB_ID="${SUB2API_JOB_ID:-$(release_current_job_id 2>/dev/null || true)}"
release_valid_job_id "$JOB_ID" || { printf 'Durable release job id is required\n' >&2; exit 1; }
job_file="$(release_job_path "$JOB_ID")"
[[ -r "$job_file" ]] || { printf 'Durable release job file is missing\n' >&2; exit 1; }

trap 'on_error "$LINENO"' ERR
trap cleanup_publisher EXIT
mkdir -p "$BACKUP_ROOT" "$(dirname "$LOG")"
touch "$LOG"
prepare_anonymous_docker
cd "$REPO"
[[ "$(git branch --show-current)" == "$BRANCH" ]] || abort_release "VPS source branch is not $BRANCH" WRONG_SOURCE_BRANCH
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || abort_release 'VPS source worktree is dirty' DIRTY_SOURCE
[[ -r "$ENV_FILE" ]] || abort_release 'production environment file is missing' ENVIRONMENT_MISSING
[[ -r "$COMPOSE" ]] || abort_release 'current production Compose file is missing' COMPOSE_MISSING

git fetch "$ORIGIN_REMOTE" "$BRANCH" >> "$LOG" 2>&1 || abort_release "fetch $ORIGIN_REF failed" ORIGIN_FETCH_FAILED
ORIGIN_COMMIT="$(git rev-parse "$ORIGIN_REF")"
LOCAL_COMMIT="$(git rev-parse HEAD)"
[[ "$APPROVED_COMMIT" == "$ORIGIN_COMMIT" ]] || abort_release "approved commit is not $ORIGIN_REF" APPROVED_COMMIT_MISMATCH

if [[ -r "$PRODUCTION_RELEASE_STATE_FILE" ]]; then
  PREVIOUS_COMMIT="$(jq -er '.production_commit' "$PRODUCTION_RELEASE_STATE_FILE" 2>/dev/null || true)"
  PREVIOUS_MAIN_DIGEST="$(jq -er '.main_digest' "$PRODUCTION_RELEASE_STATE_FILE" 2>/dev/null || true)"
  PREVIOUS_EXTENSIONS_DIGEST="$(jq -er '.extensions_digest' "$PRODUCTION_RELEASE_STATE_FILE" 2>/dev/null || true)"
  [[ "$PREVIOUS_COMMIT" =~ ^[0-9a-f]{40}$ ]] || abort_release 'previous production commit is invalid' RELEASE_STATE_INVALID
  [[ "$PREVIOUS_MAIN_DIGEST" =~ ^sha256:[0-9a-f]{64}$ && "$PREVIOUS_EXTENSIONS_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] \
    || abort_release 'previous production digests are invalid' RELEASE_STATE_INVALID
  git merge-base --is-ancestor "$PREVIOUS_COMMIT" "$LOCAL_COMMIT" \
    || abort_release 'local source is behind or diverged from recorded production' LOCAL_SOURCE_MISMATCH
  git merge-base --is-ancestor "$LOCAL_COMMIT" "$APPROVED_COMMIT" \
    || abort_release 'local source is not an ancestor of the approved target' LOCAL_SOURCE_MISMATCH
  PREVIOUS_MAIN_REF="$MAIN_REPOSITORY@$PREVIOUS_MAIN_DIGEST"
  PREVIOUS_EXTENSIONS_REF="$EXTENSIONS_REPOSITORY@$PREVIOUS_EXTENSIONS_DIGEST"
else
  LEGACY_BOOTSTRAP=1
  PREVIOUS_COMMIT="$LOCAL_COMMIT"
  PREVIOUS_MAIN_DIGEST=''
  PREVIOUS_EXTENSIONS_DIGEST=''
  git merge-base --is-ancestor "$PREVIOUS_COMMIT" "$APPROVED_COMMIT" \
    || abort_release 'legacy production source is not an ancestor of the approved target' LEGACY_BOOTSTRAP_INVALID
fi
TARGET_MAIN_REF="$MAIN_REPOSITORY@$MAIN_DIGEST"
TARGET_EXTENSIONS_REF="$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST"

baseline_json="$(git show "$APPROVED_COMMIT:deploy/stable-release-baseline.json" 2>/dev/null)" \
  || abort_release 'target stable Release baseline is missing' BASELINE_MISSING
release_tag="$(jq -er '.tag' <<< "$baseline_json" 2>/dev/null || true)"
release_commit="$(jq -er '.commit_sha' <<< "$baseline_json" 2>/dev/null || true)"
release_tag_object_sha="$(jq -er '.tag_object_sha' <<< "$baseline_json" 2>/dev/null || true)"
[[ "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || abort_release 'target stable tag is invalid' BASELINE_INVALID
[[ "$release_commit" =~ ^[0-9a-f]{40}$ && "$release_tag_object_sha" =~ ^[0-9a-f]{40}$ ]] \
  || abort_release 'target stable object identity is invalid' BASELINE_INVALID
git cat-file -e "$release_commit^{commit}" >/dev/null 2>&1 || abort_release 'stable Release commit is unavailable' RELEASE_COMMIT_MISSING
git merge-base --is-ancestor "$release_commit" "$APPROVED_COMMIT" || abort_release 'approved target does not contain its stable baseline' RELEASE_ANCESTRY_INVALID
git fetch "$UPSTREAM_REMOTE" "refs/tags/$release_tag:refs/tags/$release_tag" >> "$LOG" 2>&1 \
  || abort_release 'fetch exact stable Release tag failed' RELEASE_TAG_FETCH_FAILED
[[ "$(git rev-parse "$release_tag^{tag}" 2>/dev/null || true)" == "$release_tag_object_sha" ]] \
  || abort_release 'stable Release tag object does not match target metadata' RELEASE_TAG_OBJECT_MISMATCH
[[ "$(git rev-parse "$release_tag^{commit}" 2>/dev/null || true)" == "$release_commit" ]] \
  || abort_release 'stable Release peeled commit does not match target metadata' RELEASE_COMMIT_MISMATCH
release_version="${release_tag#v}"

verified_output="$($VERIFY_IMAGES_SCRIPT "$APPROVED_COMMIT" "$release_version")" \
  || abort_release 'target GHCR images failed metadata verification' IMAGE_VERIFICATION_FAILED
parse_verified_images "$verified_output" || abort_release 'image verifier returned invalid evidence' IMAGE_EVIDENCE_INVALID
[[ "$VERIFIED_MAIN_DIGEST" == "$MAIN_DIGEST" && "$VERIFIED_EXTENSIONS_DIGEST" == "$EXTENSIONS_DIGEST" ]] \
  || abort_release 'provided digests do not match verified GHCR manifests' IMAGE_DIGEST_MISMATCH

if docker container inspect sub2api-risk-control >/dev/null 2>&1; then
  abort_release 'retired legacy container sub2api-risk-control still exists' LEGACY_CONTAINER_PRESENT
fi
required_containers=(sub2api extensions-self risk-control-postgres sub2api-postgres sub2api-redis)
for container_name in "${required_containers[@]}"; do
  docker container inspect "$container_name" >/dev/null 2>&1 || abort_release "required container is missing: $container_name" CONTAINER_MISSING
done

rendered_compose="$(docker compose --project-name deploy -f "$COMPOSE" --env-file "$ENV_FILE" config --format json)" \
  || abort_release 'could not render current production Compose configuration' COMPOSE_INVALID
rendered_values="$(printf '%s' "$rendered_compose" | python3 -c '
import json, sys
config = json.load(sys.stdin)["services"]
sub2api = config["sub2api"]["environment"]
extensions = config["extensions-self"]["environment"]
postgres = config["postgres"]["environment"]
risk_postgres = config["risk-control-postgres"]["environment"]
print("\n".join(str(value) for value in [
    sub2api.get("RISK_CONTROL_URL", ""),
    extensions.get("ACCOUNT_MONITOR_ENABLED", "false"),
    extensions.get("ACCOUNT_MONITOR_SOURCE_DATABASE_URL", ""),
    extensions.get("RISK_CONTROL_INTERNAL_SECRET", ""),
    postgres.get("POSTGRES_USER", "sub2api"),
    postgres.get("POSTGRES_DB", "sub2api"),
    risk_postgres.get("POSTGRES_USER", "risk_control_app"),
    risk_postgres.get("POSTGRES_DB", "risk_control"),
]), end="")
' | tr -d '\r')" || abort_release 'could not read rendered production settings' COMPOSE_INVALID
mapfile -t deploy_values <<< "$rendered_values"
[[ "${#deploy_values[@]}" -eq 8 ]] || abort_release 'rendered production settings are incomplete' COMPOSE_INVALID
rendered_risk_url="${deploy_values[0]}"
monitor_enabled="$(tr '[:upper:]' '[:lower:]' <<< "${deploy_values[1]}")"
monitor_source_url="${deploy_values[2]}"
rendered_internal_secret="${deploy_values[3]}"
postgres_owner="${deploy_values[4]}"
postgres_database="${deploy_values[5]}"
risk_postgres_owner="${deploy_values[6]}"
risk_postgres_database="${deploy_values[7]}"
[[ "$rendered_risk_url" == 'http://extensions-self:8090' ]] || abort_release 'RISK_CONTROL_URL is not the unified extension service' COMPOSE_INVALID
[[ "$monitor_enabled" == true || "$monitor_enabled" == false ]] || abort_release 'ACCOUNT_MONITOR_ENABLED is invalid' COMPOSE_INVALID

monitor_password=''
if [[ "$monitor_enabled" == true ]]; then
  monitor_password="$(ACCOUNT_MONITOR_SOURCE_DATABASE_URL="$monitor_source_url" EXPECTED_DATABASE="$postgres_database" python3 -c '
import os, sys
from urllib.parse import unquote, urlparse
parsed = urlparse(os.environ.get("ACCOUNT_MONITOR_SOURCE_DATABASE_URL", ""))
password = unquote(parsed.password or "")
valid = (parsed.scheme in {"postgres", "postgresql"}
  and unquote(parsed.username or "") == "extensions_self_monitor"
  and parsed.hostname == "postgres" and (parsed.port or 5432) == 5432
  and parsed.path.lstrip("/") == os.environ.get("EXPECTED_DATABASE", "")
  and password != "" and "\n" not in password and "\r" not in password)
if not valid: sys.exit(1)
print(password, end="")
')" || abort_release 'account monitor source DSN is invalid' MONITOR_SOURCE_INVALID
  [[ -n "$rendered_internal_secret" ]] || abort_release 'account monitor signing secret is missing' MONITOR_SECRET_MISSING
fi

job_update backing_up "Backing up production before $APPROVED_COMMIT" '{}'
mkdir -p "$BACKUP_DIR"
docker exec sub2api-postgres pg_dump -U "$postgres_owner" -d "$postgres_database" -Fc > "$BACKUP_DIR/sub2api_db.dump" \
  || abort_release 'main database backup failed' MAIN_BACKUP_FAILED
docker exec risk-control-postgres pg_dump -U "$risk_postgres_owner" -d "$risk_postgres_database" -Fc > "$BACKUP_DIR/risk_control_db.dump" \
  || abort_release 'extensions database backup failed' EXTENSIONS_BACKUP_FAILED
docker exec -i sub2api-postgres pg_restore --list < "$BACKUP_DIR/sub2api_db.dump" > "$BACKUP_DIR/sub2api_db.list" \
  || abort_release 'main database backup verification failed' MAIN_BACKUP_INVALID
docker exec -i risk-control-postgres pg_restore --list < "$BACKUP_DIR/risk_control_db.dump" > "$BACKUP_DIR/risk_control_db.list" \
  || abort_release 'extensions database backup verification failed' EXTENSIONS_BACKUP_INVALID
cp -p "$ENV_FILE" "$BACKUP_DIR/main.env"
cp -p "$COMPOSE" "$BACKUP_DIR/main-docker-compose.yml"
if [[ "$LEGACY_BOOTSTRAP" -eq 0 ]]; then
  cp -p "$PRODUCTION_RELEASE_STATE_FILE" "$BACKUP_DIR/release-state.json"
fi
[[ -f "$NGINX_VHOST" ]] || abort_release 'Nginx vhost is missing' NGINX_VHOST_MISSING
cp -p "$NGINX_VHOST" "$BACKUP_DIR/nginx-sub.ailisten.top.conf"
mkdir -p "$BACKUP_DIR/certificates"
while IFS=$'\t' read -r directive certificate_path; do
  [[ -f "$certificate_path" ]] || abort_release "Nginx $directive file is missing: $certificate_path" CERTIFICATE_MISSING
  certificate_name="$(basename "$certificate_path")"
  cp -p "$certificate_path" "$BACKUP_DIR/certificates/$certificate_name"
  printf '%s\t%s\t%s\n' "$directive" "$certificate_path" "$certificate_name" >> "$BACKUP_DIR/certificate-metadata.tsv"
done < <(awk '$1 == "ssl_certificate" || $1 == "ssl_certificate_key" { gsub(/;/, "", $2); print $1 "\t" $2 }' "$NGINX_VHOST")
[[ -s "$BACKUP_DIR/certificate-metadata.tsv" ]] || abort_release 'Nginx vhost exposes no certificate paths' CERTIFICATE_METADATA_MISSING
docker inspect "${required_containers[@]}" > "$BACKUP_DIR/container-metadata.json" || abort_release 'container metadata backup failed' CONTAINER_METADATA_FAILED
docker ps -a --no-trunc > "$BACKUP_DIR/docker-containers.txt" || abort_release 'container inventory backup failed' CONTAINER_METADATA_FAILED
docker images --digests --no-trunc > "$BACKUP_DIR/docker-images.txt" || abort_release 'image inventory backup failed' IMAGE_METADATA_FAILED
current_image_ids=()
for container_name in sub2api extensions-self; do
  current_image_ids+=("$(docker inspect "$container_name" --format '{{.Image}}')")
done
docker image inspect "${current_image_ids[@]}" > "$BACKUP_DIR/image-metadata.json" || abort_release 'active image metadata backup failed' IMAGE_METADATA_FAILED
MAIN_ROLLBACK_TAG="sub2api:rollback-$STAMP"
EXTENSIONS_ROLLBACK_TAG="deploy-extensions-self:rollback-$STAMP"
docker image tag "${current_image_ids[0]}" "$MAIN_ROLLBACK_TAG" || abort_release 'main rollback image tag failed' IMAGE_METADATA_FAILED
docker image tag "${current_image_ids[1]}" "$EXTENSIONS_ROLLBACK_TAG" || abort_release 'extensions rollback image tag failed' IMAGE_METADATA_FAILED
if [[ "$LEGACY_BOOTSTRAP" -eq 1 ]]; then
  PREVIOUS_MAIN_DIGEST="${current_image_ids[0]}"
  PREVIOUS_EXTENSIONS_DIGEST="${current_image_ids[1]}"
  PREVIOUS_MAIN_REF="$MAIN_ROLLBACK_TAG"
  PREVIOUS_EXTENSIONS_REF="$EXTENSIONS_ROLLBACK_TAG"
  jq -n \
    --arg production_commit "$PREVIOUS_COMMIT" \
    --arg main_image_id "$PREVIOUS_MAIN_DIGEST" \
    --arg extensions_image_id "$PREVIOUS_EXTENSIONS_DIGEST" \
    --arg main_rollback_tag "$MAIN_ROLLBACK_TAG" \
    --arg extensions_rollback_tag "$EXTENSIONS_ROLLBACK_TAG" \
    '{legacy_bootstrap:true,production_commit:$production_commit,main_image_id:$main_image_id,extensions_image_id:$extensions_image_id,main_rollback_tag:$main_rollback_tag,extensions_rollback_tag:$extensions_rollback_tag}' \
    > "$BACKUP_DIR/release-state.json"
fi
cat > "$BACKUP_DIR/release-metadata.env" <<EOF
PREVIOUS_COMMIT=$PREVIOUS_COMMIT
PREVIOUS_MAIN_DIGEST=$PREVIOUS_MAIN_DIGEST
PREVIOUS_EXTENSIONS_DIGEST=$PREVIOUS_EXTENSIONS_DIGEST
TARGET_COMMIT=$APPROVED_COMMIT
TARGET_MAIN_DIGEST=$MAIN_DIGEST
TARGET_EXTENSIONS_DIGEST=$EXTENSIONS_DIGEST
MAIN_ROLLBACK_TAG=$MAIN_ROLLBACK_TAG
EXTENSIONS_ROLLBACK_TAG=$EXTENSIONS_ROLLBACK_TAG
LEGACY_BOOTSTRAP=$([[ "$LEGACY_BOOTSTRAP" -eq 1 ]] && printf true || printf false)
STABLE_RELEASE_TAG=$release_tag
STABLE_RELEASE_COMMIT=$release_commit
BACKUP_DIR=$BACKUP_DIR
BACKFILL_STATUS=pending
BACKFILL_RANGE=pending
EOF
find "$BACKUP_DIR" -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > "$BACKUP_DIR/SHA256SUMS"

if [[ "$LOCAL_COMMIT" != "$APPROVED_COMMIT" ]]; then
  git merge --ff-only "$ORIGIN_REF" >> "$LOG" 2>&1 || abort_release 'local source fast-forward failed after backup' LOCAL_FAST_FORWARD_FAILED
fi
[[ "$(git rev-parse HEAD)" == "$APPROVED_COMMIT" ]] || abort_release 'local source did not reach the approved target' LOCAL_SOURCE_MISMATCH

SUB2API_IMAGE="$TARGET_MAIN_REF" EXTENSIONS_SELF_IMAGE="$TARGET_EXTENSIONS_REF" \
  docker compose --project-name deploy -f "$COMPOSE" --env-file "$ENV_FILE" config --quiet \
  || abort_release 'target Compose configuration is invalid' TARGET_COMPOSE_INVALID

if [[ "$monitor_enabled" == true ]]; then
  source_stage="/tmp/account-monitor-source-$STAMP"
  docker exec sub2api-postgres mkdir -p "$source_stage/deploy/ops" "$source_stage/extensions-self/account-monitor/sql" \
    || abort_release 'could not prepare account monitor SQL staging' MONITOR_SOURCE_INSTALL_FAILED
  docker cp "$REPO/deploy/ops/install-account-monitor-source.sql" "sub2api-postgres:$source_stage/deploy/ops/install-account-monitor-source.sql" \
    || abort_release 'could not stage account monitor installer' MONITOR_SOURCE_INSTALL_FAILED
  docker cp "$REPO/extensions-self/account-monitor/sql/main_source_views.sql" "sub2api-postgres:$source_stage/extensions-self/account-monitor/sql/main_source_views.sql" \
    || abort_release 'could not stage account monitor views' MONITOR_SOURCE_INSTALL_FAILED
  docker exec sub2api-postgres psql -U "$postgres_owner" -d "$postgres_database" -v ON_ERROR_STOP=1 \
    -v account_monitor_password="$monitor_password" -f "$source_stage/deploy/ops/install-account-monitor-source.sql" >> "$LOG" 2>&1 \
    || abort_release 'account monitor source installation failed' MONITOR_SOURCE_INSTALL_FAILED
  docker exec sub2api-postgres rm -rf "$source_stage" >/dev/null 2>&1 || true
  docker exec sub2api-postgres psql -U "$postgres_owner" -d "$postgres_database" -v ON_ERROR_STOP=1 \
    -c 'BEGIN; SET ROLE extensions_self_monitor_ro; SELECT 1 FROM extensions_self_ro.usage_source LIMIT 1; SELECT 1 FROM extensions_self_ro.group_dimension LIMIT 1; SELECT 1 FROM extensions_self_ro.account_group_dimension LIMIT 1; ROLLBACK;' >> "$LOG" 2>&1 \
    || abort_release 'source privilege role probe failed' MONITOR_SOURCE_PROBE_FAILED
  docker exec -e PGPASSWORD="$monitor_password" sub2api-postgres psql -h 127.0.0.1 -U extensions_self_monitor -d "$postgres_database" -v ON_ERROR_STOP=1 \
    -c 'SELECT 1 FROM extensions_self_ro.usage_source LIMIT 1; SELECT 1 FROM extensions_self_ro.group_dimension LIMIT 1; SELECT 1 FROM extensions_self_ro.account_group_dimension LIMIT 1;' >> "$LOG" 2>&1 \
    || abort_release 'source login allow probe failed' MONITOR_SOURCE_PROBE_FAILED
  if docker exec -e PGPASSWORD="$monitor_password" sub2api-postgres psql -h 127.0.0.1 -U extensions_self_monitor -d "$postgres_database" -v ON_ERROR_STOP=1 \
    -c 'SELECT key FROM public.api_keys LIMIT 1;' >> "$LOG" 2>&1; then
    abort_release 'source login unexpectedly accessed full API keys' MONITOR_SOURCE_DENY_FAILED
  fi
  if docker exec -e PGPASSWORD="$monitor_password" sub2api-postgres psql -h 127.0.0.1 -U extensions_self_monitor -d "$postgres_database" -v ON_ERROR_STOP=1 \
    -c 'SELECT credentials FROM public.accounts LIMIT 1;' >> "$LOG" 2>&1; then
    abort_release 'source login unexpectedly accessed account credentials' MONITOR_SOURCE_DENY_FAILED
  fi
fi

anonymous_docker pull "$TARGET_EXTENSIONS_REF" >> "$LOG" 2>&1 || abort_release 'pull target extensions digest failed' TARGET_PULL_FAILED
anonymous_docker pull "$TARGET_MAIN_REF" >> "$LOG" 2>&1 || abort_release 'pull target main digest failed' TARGET_PULL_FAILED

MUTATION_STARTED=1
write_image_environment "$TARGET_MAIN_REF" "$TARGET_EXTENSIONS_REF" || abort_release 'write target image environment failed' ENVIRONMENT_UPDATE_FAILED

job_update deploying_extensions "Deploying extensions digest $EXTENSIONS_DIGEST" '{}'
compose_with "$COMPOSE" up -d --no-deps --force-recreate extensions-self >> "$LOG" 2>&1 \
  || abort_release 'extensions deployment failed' EXTENSIONS_DEPLOY_FAILED
wait_container_health extensions-self || abort_release 'extensions container did not become healthy' EXTENSIONS_HEALTH_FAILED
docker exec extensions-self wget -qO- -T 5 http://extensions-self:8090/healthz >/dev/null \
  || abort_release 'extensions internal health check failed' EXTENSIONS_HEALTH_FAILED

job_update deploying_main "Deploying main digest $MAIN_DIGEST" '{}'
compose_with "$COMPOSE" up -d --no-deps --force-recreate sub2api >> "$LOG" 2>&1 \
  || abort_release 'main deployment failed' MAIN_DEPLOY_FAILED
wait_container_health sub2api || abort_release 'main container did not become healthy' MAIN_HEALTH_FAILED

job_update health_checking 'Checking internal and public production health' '{}'
full_health_check || abort_release 'production health checks failed' PRODUCTION_HEALTH_FAILED
if docker container inspect risk-control >/dev/null 2>&1; then
  docker rm -f risk-control >> "$LOG" 2>&1 || abort_release 'retired risk-control container removal failed' LEGACY_CONTAINER_REMOVE_FAILED
fi

published_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
production_state="$(jq -n \
  --arg production_commit "$APPROVED_COMMIT" \
  --arg stable_release_tag "$release_tag" \
  --arg stable_release_commit "$release_commit" \
  --arg main_digest "$MAIN_DIGEST" \
  --arg extensions_digest "$EXTENSIONS_DIGEST" \
  --arg published_at "$published_at" \
  --arg backup_dir "$BACKUP_DIR" \
  '{production_commit:$production_commit,stable_release_tag:$stable_release_tag,stable_release_commit:$stable_release_commit,main_digest:$main_digest,extensions_digest:$extensions_digest,published_at:$published_at,backup_dir:$backup_dir}')"
release_production_state_write "$production_state" || abort_release 'could not persist release-state.json' RELEASE_STATE_WRITE_FAILED
success_metadata="$(jq -n \
  --arg commit "$APPROVED_COMMIT" \
  --arg main_digest "$MAIN_DIGEST" \
  --arg extensions_digest "$EXTENSIONS_DIGEST" \
  --arg backup_dir "$BACKUP_DIR" \
  '{published:true,published_commit:$commit,target_commit:$commit,main_digest:$main_digest,extensions_digest:$extensions_digest,production_changed:true,artifact_path:$backup_dir,rollback:{attempted:false,succeeded:false,message:""}}')"
job_update success "Published $APPROVED_COMMIT by digest" "$success_metadata"
log "PUBLISH OK: commit=$APPROVED_COMMIT main=$MAIN_DIGEST extensions=$EXTENSIONS_DIGEST backup=$BACKUP_DIR"
