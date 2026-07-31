#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="${SUB2API_REPO:-/root/sub2api}"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
BASELINE_FILE="${SUB2API_STABLE_BASELINE_FILE:-$REPO/deploy/stable-release-baseline.json}"
VERIFY_IMAGES_SCRIPT="${SUB2API_VERIFY_IMAGES_SCRIPT:-$SCRIPT_DIR/verify-release-images.sh}"
COMMON_HELPER="${SUB2API_RELEASE_COMMON_HELPER:-$SCRIPT_DIR/release-common.sh}"
LEDGER_HELPER="${SUB2API_RELEASE_LEDGER_HELPER:-$SCRIPT_DIR/release-ledger.sh}"
INSTALL_ROOT="${SUB2API_OPS_INSTALL_ROOT:-/opt/sub2api-custom}"
SYSTEMD_ROOT="${SUB2API_SYSTEMD_ROOT:-/etc/systemd/system}"
NGINX_VHOST="${SUB2API_NGINX_VHOST:-/etc/nginx/sites-available/sub.ailisten.top}"
ORIGIN_CERT="${SUB2API_ORIGIN_CERT:-/etc/nginx/ssl/ailisten.top.crt}"
ORIGIN_KEY="${SUB2API_ORIGIN_KEY:-/etc/nginx/ssl/ailisten.top.key}"
INTERNAL_HEALTH_URL="${SUB2API_INTERNAL_HEALTH_URL:-http://127.0.0.1:8081/health}"
PUBLIC_HEALTH_URL="${SUB2API_PUBLIC_HEALTH_URL:-}"
MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"
EXPECTED_PLATFORM='linux/amd64'
BOOTSTRAP_OWNER="bootstrap-$(date -u '+%Y%m%dT%H%M%S%NZ')-$$"

MODE="${1:-}"
[[ "$MODE" == fresh || "$MODE" == migrate ]] || {
  printf 'usage: bootstrap-custom-site.sh <fresh|migrate> [--env-file path|--bundle path] --confirm <token> [--check-only]\n' >&2
  exit 2
}
shift

INPUT_ENV=''
BUNDLE=''
CONFIRM=''
CHECK_ONLY=false
MUTATED=false
COMPLETED=false
STAGED_ENV=''
RENDERED_JSON=''
CREATED_VOLUMES=()
TARGET_CONTAINERS=(sub2api extensions-self risk-control-postgres sub2api-postgres sub2api-redis)
CREATED_CONTAINERS=()
CREATED_CONTAINER_IDS=()
INSTALLED_ENV=false
INSTALLED_OPS=false
INSTALLED_UNITS=false
WATCHER_ENABLED=false
CREATED_NGINX_FILES=()

fail() {
  printf 'Site bootstrap failed: %s\n' "$1" >&2
  exit 1
}

compose() {
  docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" "$@"
}

cleanup_new_site() {
  local index container container_id volume volume_owner path source_path
  [[ "$MUTATED" == true && "$COMPLETED" != true ]] || return 0
  if [[ "$WATCHER_ENABLED" == true ]]; then
    systemctl disable --now sub2api-release.path >/dev/null 2>&1 || true
  fi
  if [[ "$INSTALLED_UNITS" == true ]]; then
    rm -f -- "$SYSTEMD_ROOT/sub2api-release.path" "$SYSTEMD_ROOT/sub2api-release.service"
    systemctl daemon-reload >/dev/null 2>&1 || true
  fi
  if [[ "$INSTALLED_OPS" == true ]]; then
    for source_path in "$REPO"/deploy/ops/*.sh; do
      rm -f -- "$INSTALL_ROOT/$(basename "$source_path")"
    done
    rm -f -- "$INSTALL_ROOT/actions-check-result.jq"
    rmdir -- "$INSTALL_ROOT" >/dev/null 2>&1 || true
  fi
  for index in "${!CREATED_CONTAINERS[@]}"; do
    container="${CREATED_CONTAINERS[$index]}"
    container_id="$(docker container inspect --format '{{.Id}}' "$container" 2>/dev/null || true)"
    [[ "$container_id" != "${CREATED_CONTAINER_IDS[$index]}" ]] || docker rm -f "$container" >/dev/null 2>&1 || true
  done
  for volume in "${CREATED_VOLUMES[@]}"; do
    volume_owner="$(docker volume inspect --format '{{index .Labels "com.listencodes.sub2api.bootstrap-owner"}}' "$volume" 2>/dev/null || true)"
    [[ "$volume_owner" != "$BOOTSTRAP_OWNER" ]] || docker volume rm "$volume" >/dev/null 2>&1 || true
  done
  for path in "${CREATED_NGINX_FILES[@]}"; do
    rm -f -- "$path"
  done
  [[ "$INSTALLED_ENV" != true ]] || rm -f -- "$ENV_FILE"
  [[ -z "$STAGED_ENV" || ! -e "$STAGED_ENV" ]] || rm -f -- "$STAGED_ENV"
}
trap cleanup_new_site EXIT

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env-file) INPUT_ENV="${2:-}"; shift 2 ;;
    --bundle) BUNDLE="${2:-}"; shift 2 ;;
    --confirm) CONFIRM="${2:-}"; shift 2 ;;
    --check-only) CHECK_ONLY=true; shift ;;
    *) fail "unknown argument: $1" ;;
  esac
done

if [[ "$MODE" == fresh ]]; then
  [[ "$CONFIRM" == FRESH-EMPTY-SITE ]] || fail 'fresh confirmation must be FRESH-EMPTY-SITE'
  [[ "$INPUT_ENV" == /* && -f "$INPUT_ENV" && ! -L "$INPUT_ENV" ]] || fail 'fresh requires an absolute regular --env-file'
  [[ -z "$BUNDLE" ]] || fail 'fresh does not accept --bundle'
else
  [[ "$CONFIRM" == RESTORE-MIGRATION ]] || fail 'migrate confirmation must be RESTORE-MIGRATION'
  [[ "$BUNDLE" == /* && -d "$BUNDLE" && ! -L "$BUNDLE" ]] || fail 'migrate requires an absolute bundle directory'
  [[ -z "$INPUT_ENV" ]] || fail 'migrate uses only the bundled environment'
fi

[[ "${EUID:-$(id -u)}" -eq 0 || "${SUB2API_ALLOW_NON_ROOT_FOR_TESTS:-}" == 1 ]] || fail 'root is required'
for command_name in git jq docker curl sha256sum find xargs flock install stat uname; do
  command -v "$command_name" >/dev/null 2>&1 || fail "required command is missing: $command_name"
done
HOST_OS="${SUB2API_HOST_OS:-$(uname -s)}"
HOST_ARCH="${SUB2API_HOST_ARCH:-$(uname -m)}"
[[ "$HOST_OS" == Linux && "$HOST_ARCH" =~ ^(x86_64|amd64)$ ]] || fail "host must provide $EXPECTED_PLATFORM"
[[ -r "$COMMON_HELPER" && -r "$LEDGER_HELPER" && -x "$VERIFY_IMAGES_SCRIPT" ]] || fail 'release helpers are unavailable'
[[ "$INSTALL_ROOT" == /* && "$INSTALL_ROOT" != / && "$SYSTEMD_ROOT" == /* && "$SYSTEMD_ROOT" != / ]] || fail 'install paths must be absolute non-root paths'
[[ ! -e "$INSTALL_ROOT" ]] || fail 'operations install root already exists'
[[ ! -e "$SYSTEMD_ROOT/sub2api-release.path" && ! -e "$SYSTEMD_ROOT/sub2api-release.service" ]] || fail 'release systemd units already exist'
[[ ! -e "$ENV_FILE" ]] || fail 'production environment already exists'
[[ -d "$(dirname "$ENV_FILE")" && ! -L "$(dirname "$ENV_FILE")" ]] || fail 'production environment parent is invalid'

REPO_REAL="$(cd "$REPO" 2>/dev/null && pwd -P)" || fail 'repository is missing'
[[ "$(git -C "$REPO" branch --show-current)" == custom-release ]] || fail 'repository branch must be custom-release'
HEAD_COMMIT="$(git -C "$REPO" rev-parse HEAD 2>/dev/null || true)"
ORIGIN_COMMIT="$(git -C "$REPO" rev-parse origin/custom-release 2>/dev/null || true)"
[[ "$HEAD_COMMIT" =~ ^[0-9a-f]{40}$ && "$HEAD_COMMIT" == "$ORIGIN_COMMIT" ]] || fail 'checkout must match origin/custom-release exactly'
[[ -z "$(git -C "$REPO" status --porcelain --untracked-files=all)" ]] || fail 'repository is dirty'
[[ -r "$COMPOSE_BASE" && -r "$COMPOSE_CUSTOM" && -r "$BASELINE_FILE" ]] || fail 'Compose pair or Stable metadata is missing'

for container in "${TARGET_CONTAINERS[@]}"; do
  ! docker container inspect "$container" >/dev/null 2>&1 || fail "target container already exists: $container"
done
for volume in deploy_sub2api_data deploy_postgres_data deploy_redis_data deploy_risk_control_postgres_data; do
  ! docker volume inspect "$volume" >/dev/null 2>&1 || fail "target volume already exists: $volume"
done

secure_file() {
  local path="$1" mode
  [[ -f "$path" && ! -L "$path" ]] || return 1
  mode="$(stat -c '%a' "$path")" || return 1
  (( (8#$mode & 8#077) == 0 ))
}

env_value() {
  local key="$1" file="$2"
  awk -F= -v key="$key" '$1 == key {sub(/^[^=]*=/, ""); value=$0} END {print value}' "$file"
}

source "$COMMON_HELPER"
source "$LEDGER_HELPER"

verify_bundle() {
  local expected_commit state_commit current_release record original_backup relative_backup mapped_backup record_tmp bundled_key
  BUNDLE_REAL="$(cd "$BUNDLE" && pwd -P)" || return 1
  (
    [[ -s "$BUNDLE_REAL/SHA256SUMS" && ! -L "$BUNDLE_REAL/SHA256SUMS" ]] || return 1
    [[ -z "$(find "$BUNDLE_REAL" -type l -print -quit)" ]] || return 1
    RELEASE_BACKUP_ROOT="$BUNDLE_REAL"
    ledger_validate_manifest_exact "$BUNDLE_REAL" SHA256SUMS || return 1
    for required in bundle.json release-state.json sub2api_db.dump sub2api_db.list risk_control_db.dump risk_control_db.list \
      config/.env config/docker-compose.yml config/docker-compose.custom.yml nginx/origin-key.path release-ledger/state.json; do
      [[ -s "$BUNDLE_REAL/$required" && ! -L "$BUNDLE_REAL/$required" ]] || return 1
    done
    secure_file "$BUNDLE_REAL/config/.env" || return 1
    bundled_key="$(head -n 1 "$BUNDLE_REAL/nginx/origin-key.path")"
    [[ "$bundled_key" == /* && "$bundled_key" != / ]] || return 1
    secure_file "$BUNDLE_REAL/nginx/$(basename "$bundled_key")" || return 1
    expected_commit="$(jq -r '.source_commit // empty' "$BUNDLE_REAL/bundle.json")"
    state_commit="$(jq -r '.production_commit // empty' "$BUNDLE_REAL/release-state.json")"
    [[ "$expected_commit" == "$HEAD_COMMIT" && "$state_commit" == "$HEAD_COMMIT" ]] || return 1
    RELEASE_LEDGER_ROOT="$BUNDLE_REAL/release-ledger"
    PRODUCTION_RELEASE_STATE_FILE="$BUNDLE_REAL/release-state.json"
    RELEASE_BACKUP_ROOT="$BUNDLE_REAL/release-backups"
    ledger_validate_state "$RELEASE_LEDGER_ROOT/state.json" || return 1
    [[ "$(jq -r '.active_operation_id // empty' "$RELEASE_LEDGER_ROOT/state.json")" == '' ]] || return 1
    current_release="$(jq -r '.current_release_id' "$RELEASE_LEDGER_ROOT/state.json")"
    [[ -s "$RELEASE_LEDGER_ROOT/releases/$current_release.json" ]] || return 1
    shopt -s nullglob
    records=("$RELEASE_LEDGER_ROOT"/releases/*.json)
    [[ "${#records[@]}" -gt 0 ]] || return 1
    for record in "${records[@]}"; do
      original_backup="$(jq -r '.backup_dir // empty' "$record")"
      [[ "$original_backup" == "$DATA_DIR/release-backups/"* ]] || return 1
      relative_backup="${original_backup#"$DATA_DIR/release-backups/"}"
      [[ -n "$relative_backup" && "$relative_backup" != */../* && "$relative_backup" != ../* ]] || return 1
      mapped_backup="$BUNDLE_REAL/release-backups/$relative_backup"
      record_tmp="$(mktemp "$BUNDLE_REAL/.mapped-release.XXXXXX")"
      jq --arg backup "$mapped_backup" '.backup_dir=$backup' "$record" > "$record_tmp"
      ledger_validate_release_artifacts "$record_tmp" || { rm -f -- "$record_tmp"; return 1; }
      rm -f -- "$record_tmp"
    done
    ledger_validate_current_projection_consistency "$RELEASE_LEDGER_ROOT/state.json" \
      "$PRODUCTION_RELEASE_STATE_FILE" "$RELEASE_LEDGER_ROOT/releases/$current_release.json" || return 1
    grep -q . "$BUNDLE_REAL/sub2api_db.list" && grep -q . "$BUNDLE_REAL/risk_control_db.list"
  )
}

if [[ "$MODE" == fresh ]]; then
  secure_file "$INPUT_ENV" || fail 'fresh environment must be a regular mode-0600 secret file'
  [[ "$INPUT_ENV" != "$ENV_FILE" ]] || fail 'fresh input environment must be outside the production destination'
  STABLE_TAG="$(jq -r '.tag // empty' "$BASELINE_FILE")"
  STABLE_COMMIT="$(jq -r '.commit_sha // empty' "$BASELINE_FILE")"
  [[ "$STABLE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ && "$STABLE_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail 'Stable metadata is invalid'
  git -C "$REPO" merge-base --is-ancestor "$STABLE_COMMIT" "$HEAD_COMMIT" || fail 'checkout does not contain the recorded Stable commit'
  SOURCE_ENV="$INPUT_ENV"
else
  verify_bundle || fail 'migration bundle validation failed'
  SOURCE_ENV="$BUNDLE_REAL/config/.env"
  STABLE_TAG="$(jq -r '.stable_release_tag' "$BUNDLE_REAL/release-state.json")"
  STABLE_COMMIT="$(jq -r '.stable_release_commit' "$BUNDLE_REAL/release-state.json")"
  [[ "$(sha256sum "$COMPOSE_BASE" | awk '{print $1}')" == "$(sha256sum "$BUNDLE_REAL/config/docker-compose.yml" | awk '{print $1}')" ]] || fail 'bundled base Compose does not match source'
  [[ "$(sha256sum "$COMPOSE_CUSTOM" | awk '{print $1}')" == "$(sha256sum "$BUNDLE_REAL/config/docker-compose.custom.yml" | awk '{print $1}')" ]] || fail 'bundled custom Compose does not match source'
fi

VERIFY_ARGS=()
[[ "$CHECK_ONLY" != true ]] || VERIFY_ARGS+=(--no-pull)
images_output="$($VERIFY_IMAGES_SCRIPT "${VERIFY_ARGS[@]}" "$HEAD_COMMIT" "${STABLE_TAG#v}")" || fail 'paired public image verification failed'
MAIN_DIGEST="$(awk -F= '$1=="main_digest" {print $2}' <<< "$images_output")"
EXTENSIONS_DIGEST="$(awk -F= '$1=="extensions_digest" {print $2}' <<< "$images_output")"
[[ "$MAIN_DIGEST" =~ ^sha256:[0-9a-f]{64}$ && "$EXTENSIONS_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || fail 'image verifier returned invalid digests'
if [[ "$MODE" == migrate ]]; then
  [[ "$MAIN_DIGEST" == "$(jq -r '.main_digest' "$BUNDLE_REAL/release-state.json")" ]] || fail 'bundled main digest does not match GHCR'
  [[ "$EXTENSIONS_DIGEST" == "$(jq -r '.extensions_digest' "$BUNDLE_REAL/release-state.json")" ]] || fail 'bundled extensions digest does not match GHCR'
fi

STAGED_ENV="$(mktemp "$(dirname "$ENV_FILE")/.bootstrap-env.XXXXXX")"
release_stage_target_env "$SOURCE_ENV" "$STAGED_ENV" "$MAIN_REPOSITORY@$MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  || fail 'digest environment staging failed'
chmod 0600 "$STAGED_ENV"
RENDERED_JSON="$(mktemp "$(dirname "$ENV_FILE")/.bootstrap-compose.XXXXXX")"
release_render_explicit_compose "$COMPOSE_BASE" "$COMPOSE_CUSTOM" "$STAGED_ENV" "$RENDERED_JSON" /dev/null \
  || fail 'explicit Compose rendering failed'
release_validate_rendered_compose "$RENDERED_JSON" "$MAIN_REPOSITORY@$MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  || fail 'rendered Compose contract failed'

if [[ "$CHECK_ONLY" == true ]]; then
  printf 'check_only=true\nmode=%s\nsource_commit=%s\nstable_release=%s@%s\nmain_digest=%s\nextensions_digest=%s\n' \
    "$MODE" "$HEAD_COMMIT" "$STABLE_TAG" "$STABLE_COMMIT" "$MAIN_DIGEST" "$EXTENSIONS_DIGEST"
  rm -f -- "$STAGED_ENV" "$RENDERED_JSON"
  STAGED_ENV=''
  exit 0
fi

[[ -n "$PUBLIC_HEALTH_URL" ]] || fail 'SUB2API_PUBLIC_HEALTH_URL is required for the final public health gate'
MUTATED=true
if [[ "$MODE" == fresh ]]; then
  for source_path in "$NGINX_VHOST" "$ORIGIN_CERT" "$ORIGIN_KEY"; do
    [[ -f "$source_path" && ! -L "$source_path" && -r "$source_path" ]] || fail "health/backup file is unavailable: $source_path"
  done
else
  NGINX_VHOST="$(head -n 1 "$BUNDLE_REAL/nginx/nginx-vhost.path")"
  ORIGIN_CERT="$(head -n 1 "$BUNDLE_REAL/nginx/origin-cert.path")"
  ORIGIN_KEY="$(head -n 1 "$BUNDLE_REAL/nginx/origin-key.path")"
  for source_path in "$NGINX_VHOST" "$ORIGIN_CERT" "$ORIGIN_KEY"; do
    [[ "$source_path" == /* && "$source_path" != / && ! -e "$source_path" ]] || fail "migration destination already exists or is invalid: $source_path"
    [[ -d "$(dirname "$source_path")" && ! -L "$(dirname "$source_path")" ]] || fail "migration destination parent is invalid: $source_path"
  done
  install -m 0644 "$BUNDLE_REAL/nginx/$(basename "$NGINX_VHOST")" "$NGINX_VHOST"
  CREATED_NGINX_FILES+=("$NGINX_VHOST")
  install -m 0644 "$BUNDLE_REAL/nginx/$(basename "$ORIGIN_CERT")" "$ORIGIN_CERT"
  CREATED_NGINX_FILES+=("$ORIGIN_CERT")
  install -m 0600 "$BUNDLE_REAL/nginx/$(basename "$ORIGIN_KEY")" "$ORIGIN_KEY"
  CREATED_NGINX_FILES+=("$ORIGIN_KEY")
fi

install -m 0600 "$STAGED_ENV" "$ENV_FILE"
INSTALLED_ENV=true
rm -f -- "$STAGED_ENV"
STAGED_ENV=''
compose pull postgres redis risk-control-postgres
for volume in deploy_sub2api_data deploy_postgres_data deploy_redis_data deploy_risk_control_postgres_data; do
  docker volume create --label com.docker.compose.project=deploy --label "com.docker.compose.volume=${volume#deploy_}" \
    --label "com.listencodes.sub2api.bootstrap-owner=$BOOTSTRAP_OWNER" "$volume" >/dev/null
  volume_owner="$(docker volume inspect --format '{{index .Labels "com.listencodes.sub2api.bootstrap-owner"}}' "$volume" 2>/dev/null || true)"
  [[ "$volume_owner" == "$BOOTSTRAP_OWNER" ]] || fail "target volume ownership changed during bootstrap: $volume"
  CREATED_VOLUMES+=("$volume")
done
DATA_MOUNT="$(docker volume inspect --format '{{.Mountpoint}}' deploy_sub2api_data)"
[[ "$(cd "$DATA_MOUNT" && pwd -P)" == "$(cd "$DATA_DIR" && pwd -P)" ]] || fail 'deploy_sub2api_data mountpoint does not match the standard artifact root'

POSTGRES_USER_VALUE="$(env_value POSTGRES_USER "$ENV_FILE")"
POSTGRES_DB_VALUE="$(env_value POSTGRES_DB "$ENV_FILE")"
RISK_USER_VALUE="$(env_value RISK_CONTROL_POSTGRES_USER "$ENV_FILE")"
RISK_DB_VALUE="$(env_value RISK_CONTROL_POSTGRES_DB "$ENV_FILE")"
POSTGRES_USER_VALUE="${POSTGRES_USER_VALUE:-sub2api}"
POSTGRES_DB_VALUE="${POSTGRES_DB_VALUE:-sub2api}"
RISK_USER_VALUE="${RISK_USER_VALUE:-risk_control_app}"
RISK_DB_VALUE="${RISK_DB_VALUE:-risk_control}"

wait_container_healthy() {
  local container="$1" attempt status
  for attempt in {1..60}; do
    status="$(docker inspect --format '{{.State.Health.Status}}' "$container" 2>/dev/null || true)"
    case "$status" in healthy) return 0 ;; unhealthy|exited|dead) return 1 ;; esac
    sleep 2
  done
  return 1
}

start_service() {
  local service="$1" container="$2" container_id
  compose up -d --no-deps --pull never "$service"
  container_id="$(docker container inspect --format '{{.Id}}' "$container" 2>/dev/null || true)"
  [[ -n "$container_id" ]] || fail "service container was not created: $service"
  CREATED_CONTAINERS+=("$container")
  CREATED_CONTAINER_IDS+=("$container_id")
  wait_container_healthy "$container" || fail "service did not become healthy: $service"
}

start_service postgres sub2api-postgres
start_service redis sub2api-redis
start_service risk-control-postgres risk-control-postgres

if [[ "$MODE" == migrate ]]; then
  docker exec -i sub2api-postgres pg_restore --list < "$BUNDLE_REAL/sub2api_db.dump" >/dev/null
  docker exec -i risk-control-postgres pg_restore --list < "$BUNDLE_REAL/risk_control_db.dump" >/dev/null
  docker exec -i sub2api-postgres pg_restore -U "$POSTGRES_USER_VALUE" -d "$POSTGRES_DB_VALUE" --clean --if-exists < "$BUNDLE_REAL/sub2api_db.dump"
  docker exec -i risk-control-postgres pg_restore -U "$RISK_USER_VALUE" -d "$RISK_DB_VALUE" --clean --if-exists < "$BUNDLE_REAL/risk_control_db.dump"
  cp -a "$BUNDLE_REAL/release-backups" "$DATA_DIR/release-backups"
  cp -a "$BUNDLE_REAL/release-ledger" "$DATA_DIR/release-ledger"
  cp -p "$BUNDLE_REAL/release-state.json" "$DATA_DIR/release-state.json"
fi

start_service extensions-self extensions-self
start_service sub2api sub2api
compose ps --status running >/dev/null || fail 'not every Compose service is running'
curl --fail --silent --show-error --max-time 10 "$INTERNAL_HEALTH_URL" >/dev/null || fail 'internal health check failed'
curl --fail --silent --show-error --max-time 15 "$PUBLIC_HEALTH_URL" >/dev/null || fail 'public health check failed'
docker exec extensions-self wget -qO- -T 5 http://extensions-self:8090/healthz >/dev/null || fail 'extensions internal health failed'

if [[ "$MODE" == fresh ]]; then
  mkdir -p "$DATA_DIR/release-backups" "$DATA_DIR/release-ledger/releases" "$DATA_DIR/release-ledger/operations"
  BOOTSTRAP_STAMP="$(date -u '+%Y%m%dT%H%M%S%NZ')"
  RELEASE_ID="release-bootstrap-$BOOTSTRAP_STAMP-${HEAD_COMMIT:0:9}"
  BACKUP_DIR="$DATA_DIR/release-backups/bootstrap-$BOOTSTRAP_STAMP-${HEAD_COMMIT:0:9}"
  mkdir -p "$BACKUP_DIR/target"
  cp -p "$COMPOSE_BASE" "$BACKUP_DIR/target/docker-compose.yml"
  cp -p "$COMPOSE_CUSTOM" "$BACKUP_DIR/target/docker-compose.custom.yml"
  cp -p "$ENV_FILE" "$BACKUP_DIR/target/.env"
  cp -p "$RENDERED_JSON" "$BACKUP_DIR/target/rendered-compose.json"
  PUBLISHED_AT="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  PROJECTION_TMP="$DATA_DIR/.bootstrap-release-state.$$.json"
  jq -n --arg commit "$HEAD_COMMIT" --arg stable_tag "$STABLE_TAG" --arg stable_commit "$STABLE_COMMIT" \
    --arg main "$MAIN_DIGEST" --arg ext "$EXTENSIONS_DIGEST" --arg release "$RELEASE_ID" \
    --arg published "$PUBLISHED_AT" --arg backup "$BACKUP_DIR" \
    '{production_commit:$commit,stable_release_tag:$stable_tag,stable_release_commit:$stable_commit,
      main_digest:$main,extensions_digest:$ext,published_at:$published,backup_dir:$backup,
      release_id:$release,official_version:$stable_tag,custom_version:"v1.0.0",custom_version_sequence:0}' \
    > "$PROJECTION_TMP"
  ledger_validate_projection "$(cat "$PROJECTION_TMP")" || fail 'fresh release projection is invalid'
  OLD_STATE_FILE="$PRODUCTION_RELEASE_STATE_FILE"
  PRODUCTION_RELEASE_STATE_FILE="$PROJECTION_TMP"
  POSTGRES_USER="$POSTGRES_USER_VALUE" POSTGRES_DB="$POSTGRES_DB_VALUE" \
    RISK_POSTGRES_USER="$RISK_USER_VALUE" RISK_POSTGRES_DB="$RISK_DB_VALUE" \
    release_create_complete_backup "$BACKUP_DIR" "bootstrap-${HEAD_COMMIT:0:9}" /dev/null \
    || fail 'fresh complete backup failed'
  PRODUCTION_RELEASE_STATE_FILE="$OLD_STATE_FILE"
  BASE_HASH="$(sha256sum "$BACKUP_DIR/target/docker-compose.yml" | awk '{print $1}')"
  CUSTOM_HASH="$(sha256sum "$BACKUP_DIR/target/docker-compose.custom.yml" | awk '{print $1}')"
  RENDERED_HASH="$(sha256sum "$BACKUP_DIR/target/rendered-compose.json" | awk '{print $1}')"
  ENV_HASH="$(sha256sum "$BACKUP_DIR/target/.env" | awk '{print $1}')"
  BACKUP_HASH="$(sha256sum "$BACKUP_DIR/SHA256SUMS" | awk '{print $1}')"
  RECORD="$(jq -n --arg release "$RELEASE_ID" --arg official "$STABLE_TAG" --arg official_commit "$STABLE_COMMIT" \
    --arg custom_commit "$HEAD_COMMIT" --arg main "$MAIN_DIGEST" --arg ext "$EXTENSIONS_DIGEST" \
    --arg base_hash "$BASE_HASH" --arg custom_hash "$CUSTOM_HASH" --arg rendered_hash "$RENDERED_HASH" \
    --arg env_hash "$ENV_HASH" --arg backup "$BACKUP_DIR" --arg backup_hash "$BACKUP_HASH" --arg published "$PUBLISHED_AT" \
    '{schema_version:1,release_id:$release,official_version:$official,official_commit:$official_commit,
      custom_version:"v1.0.0",custom_version_sequence:0,custom_commit:$custom_commit,
      main_digest:$main,extensions_digest:$ext,base_compose_sha256:$base_hash,
      custom_compose_sha256:$custom_hash,rendered_compose_sha256:$rendered_hash,env_sha256:$env_hash,
      backup_dir:$backup,backup_manifest_sha256:$backup_hash,published_at:$published,
      source_kind:"bootstrap",operation_id:"fresh-site-bootstrap"}')"
  RELEASE_BACKUP_ROOT="$DATA_DIR/release-backups"
  ledger_create_release "$RECORD" || fail 'fresh release record creation failed'
  PRODUCTION_RELEASE_STATE_FILE="$OLD_STATE_FILE"
  ledger_write_projection "$(cat "$PROJECTION_TMP")" || fail 'fresh release projection write failed'
  rm -f -- "$PROJECTION_TMP"
  LEDGER_STATE="$(jq -n --arg release "$RELEASE_ID" --arg now "$PUBLISHED_AT" \
    '{schema_version:1,current_release_id:$release,custom_version_high_water:0,active_operation_id:null,updated_at:$now}')"
  ledger_atomic_write "$DATA_DIR/release-ledger/state.json" "$LEDGER_STATE" || fail 'fresh ledger state write failed'
else
  RELEASE_LEDGER_ROOT="$DATA_DIR/release-ledger"
  RELEASE_BACKUP_ROOT="$DATA_DIR/release-backups"
  PRODUCTION_RELEASE_STATE_FILE="$DATA_DIR/release-state.json"
  ledger_validate_state "$RELEASE_LEDGER_ROOT/state.json" || fail 'restored ledger state is invalid'
  ledger_validate_projection "$(cat "$PRODUCTION_RELEASE_STATE_FILE")" || fail 'restored release projection is invalid'
  RELEASE_ID="$(jq -r '.current_release_id' "$RELEASE_LEDGER_ROOT/state.json")"
  ledger_validate_release_artifacts "$RELEASE_LEDGER_ROOT/releases/$RELEASE_ID.json" || fail 'restored current release artifacts are invalid'
fi

install -d -m 0755 "$INSTALL_ROOT"
INSTALLED_OPS=true
install -m 0755 "$REPO"/deploy/ops/*.sh "$INSTALL_ROOT/"
install -m 0644 "$REPO/deploy/ops/actions-check-result.jq" "$INSTALL_ROOT/"
install -m 0644 "$REPO/deploy/ops/sub2api-release.path" "$SYSTEMD_ROOT/"
install -m 0644 "$REPO/deploy/ops/sub2api-release.service" "$SYSTEMD_ROOT/"
INSTALLED_UNITS=true
systemctl daemon-reload
systemctl enable --now sub2api-release.path
WATCHER_ENABLED=true
[[ "$(systemctl is-active sub2api-release.path)" == active ]] || fail 'sub2api-release.path is not active'

COMPLETED=true
rm -f -- "$RENDERED_JSON"
printf 'mode=%s\nsource_commit=%s\nstable_release=%s@%s\nrelease_id=%s\nmain_digest=%s\nextensions_digest=%s\nledger=%s\nwatcher=active\n' \
  "$MODE" "$HEAD_COMMIT" "$STABLE_TAG" "$STABLE_COMMIT" "$RELEASE_ID" "$MAIN_DIGEST" "$EXTENSIONS_DIGEST" "$DATA_DIR/release-ledger/state.json"
