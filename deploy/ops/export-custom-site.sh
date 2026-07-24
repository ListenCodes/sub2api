#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="${SUB2API_REPO:-/root/sub2api}"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
NGINX_VHOST="${SUB2API_NGINX_VHOST:-/etc/nginx/sites-available/sub.ailisten.top}"
ORIGIN_CERT="${SUB2API_ORIGIN_CERT:-/etc/nginx/ssl/ailisten.top.crt}"
ORIGIN_KEY="${SUB2API_ORIGIN_KEY:-/etc/nginx/ssl/ailisten.top.key}"
LEDGER_HELPER="${SUB2API_RELEASE_LEDGER_HELPER:-$SCRIPT_DIR/release-ledger.sh}"
MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"

OUTPUT=''
CONFIRM=''
WORK_DIR=''

fail() {
  printf 'Site export failed: %s\n' "$1" >&2
  exit 1
}

cleanup() {
  [[ -z "$WORK_DIR" || ! -e "$WORK_DIR" ]] || rm -rf -- "$WORK_DIR"
}
trap cleanup EXIT

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output) OUTPUT="${2:-}"; shift 2 ;;
    --confirm) CONFIRM="${2:-}"; shift 2 ;;
    *) printf 'usage: export-custom-site.sh --output <absolute-new-directory> --confirm EXPORT-SITE\n' >&2; exit 2 ;;
  esac
done

[[ "$CONFIRM" == EXPORT-SITE ]] || fail 'confirmation must be EXPORT-SITE'
[[ "$OUTPUT" == /* && "$OUTPUT" != / ]] || fail 'output must be an absolute non-root path'
[[ ! -e "$OUTPUT" ]] || fail 'output path must not already exist'
OUTPUT_PARENT="$(dirname "$OUTPUT")"
[[ -d "$OUTPUT_PARENT" && ! -L "$OUTPUT_PARENT" ]] || fail 'output parent must be a real directory'
[[ "${EUID:-$(id -u)}" -eq 0 || "${SUB2API_ALLOW_NON_ROOT_FOR_TESTS:-}" == 1 ]] || fail 'root is required'

for command_name in git jq docker sha256sum find xargs cp flock; do
  command -v "$command_name" >/dev/null 2>&1 || fail "required command is missing: $command_name"
done
[[ -r "$LEDGER_HELPER" ]] || fail 'release ledger helper is missing'
source "$LEDGER_HELPER"

REPO_REAL="$(cd "$REPO" 2>/dev/null && pwd -P)" || fail 'repository is missing'
OUTPUT_PARENT_REAL="$(cd "$OUTPUT_PARENT" && pwd -P)" || fail 'output parent cannot be resolved'
case "$OUTPUT_PARENT_REAL/" in "$REPO_REAL/"*) fail 'output must stay outside the repository' ;; esac

[[ "$(git -C "$REPO" branch --show-current)" == custom-release ]] || fail 'repository branch must be custom-release'
HEAD_COMMIT="$(git -C "$REPO" rev-parse HEAD 2>/dev/null || true)"
ORIGIN_COMMIT="$(git -C "$REPO" rev-parse origin/custom-release 2>/dev/null || true)"
[[ "$HEAD_COMMIT" =~ ^[0-9a-f]{40}$ && "$HEAD_COMMIT" == "$ORIGIN_COMMIT" ]] || fail 'checkout must match origin/custom-release exactly'
[[ -z "$(git -C "$REPO" status --porcelain --untracked-files=all)" ]] || fail 'repository is dirty'

STATE_PATH="$(ledger_state_path)"
ledger_validate_state "$STATE_PATH" || fail 'release-ledger state is invalid'
[[ "$(jq -r '.active_operation_id // empty' "$STATE_PATH")" == '' ]] || fail 'active_operation_id blocks export'
ledger_validate_projection "$(cat "$PRODUCTION_RELEASE_STATE_FILE")" || fail 'release-state.json is invalid'
CURRENT_RELEASE_ID="$(jq -r '.current_release_id' "$STATE_PATH")"
CURRENT_RELEASE_PATH="$(ledger_release_path "$CURRENT_RELEASE_ID")"
ledger_validate_release_artifacts "$CURRENT_RELEASE_PATH" || fail 'current release artifacts are incomplete'
[[ "$(jq -r '.production_commit' "$PRODUCTION_RELEASE_STATE_FILE")" == "$HEAD_COMMIT" ]] || fail 'release projection commit does not match source'

for source_path in "$ENV_FILE" "$COMPOSE_BASE" "$COMPOSE_CUSTOM" "$NGINX_VHOST" "$ORIGIN_CERT" "$ORIGIN_KEY"; do
  [[ -f "$source_path" && ! -L "$source_path" && -r "$source_path" ]] || fail "required source file is unavailable: $source_path"
done
[[ -d "$DATA_DIR/release-ledger" && -d "$DATA_DIR/release-backups" ]] || fail 'release-ledger or release-backups is missing'
[[ -z "$(find "$DATA_DIR/release-ledger" "$DATA_DIR/release-backups" -type l -print -quit)" ]] || fail 'release artifacts may not contain symlinks'

MAIN_DIGEST="$(jq -r '.main_digest' "$PRODUCTION_RELEASE_STATE_FILE")"
EXTENSIONS_DIGEST="$(jq -r '.extensions_digest' "$PRODUCTION_RELEASE_STATE_FILE")"
[[ "$MAIN_DIGEST" =~ ^sha256:[0-9a-f]{64}$ && "$EXTENSIONS_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || fail 'release digests are invalid'
[[ "$(docker inspect --format '{{.Config.Image}}' sub2api 2>/dev/null || true)" == "$MAIN_REPOSITORY@$MAIN_DIGEST" ]] || fail 'main container digest drifted'
[[ "$(docker inspect --format '{{.Config.Image}}' extensions-self 2>/dev/null || true)" == "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" ]] || fail 'extensions container digest drifted'
for container in sub2api sub2api-postgres sub2api-redis risk-control-postgres extensions-self; do
  [[ "$(docker inspect --format '{{.State.Health.Status}}' "$container" 2>/dev/null || true)" == healthy ]] || fail "container is not healthy: $container"
done

mkdir -p "$(dirname "$RELEASE_LEDGER_LOCK_FILE")"
exec 8>"$RELEASE_LEDGER_LOCK_FILE"
flock -n 8 || fail 'release lock is busy'

WORK_DIR="$(mktemp -d "$OUTPUT_PARENT/.sub2api-site-export.XXXXXX")"
mkdir -p "$WORK_DIR/config" "$WORK_DIR/nginx" "$WORK_DIR/metadata"
cp -a "$DATA_DIR/release-ledger" "$WORK_DIR/release-ledger"
cp -a "$DATA_DIR/release-backups" "$WORK_DIR/release-backups"
cp -p "$PRODUCTION_RELEASE_STATE_FILE" "$WORK_DIR/release-state.json"
cp -p "$ENV_FILE" "$WORK_DIR/config/.env"
chmod 0600 "$WORK_DIR/config/.env"
cp -p "$COMPOSE_BASE" "$WORK_DIR/config/docker-compose.yml"
cp -p "$COMPOSE_CUSTOM" "$WORK_DIR/config/docker-compose.custom.yml"
cp -p "$NGINX_VHOST" "$WORK_DIR/nginx/$(basename "$NGINX_VHOST")"
cp -p "$ORIGIN_CERT" "$WORK_DIR/nginx/$(basename "$ORIGIN_CERT")"
cp -p "$ORIGIN_KEY" "$WORK_DIR/nginx/$(basename "$ORIGIN_KEY")"
chmod 0600 "$WORK_DIR/nginx/$(basename "$ORIGIN_KEY")"
printf '%s\n' "$NGINX_VHOST" > "$WORK_DIR/nginx/nginx-vhost.path"
printf '%s\n' "$ORIGIN_CERT" > "$WORK_DIR/nginx/origin-cert.path"
printf '%s\n' "$ORIGIN_KEY" > "$WORK_DIR/nginx/origin-key.path"

docker exec sub2api-postgres pg_dump -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" -Fc > "$WORK_DIR/sub2api_db.dump"
docker exec risk-control-postgres pg_dump -U "${RISK_POSTGRES_USER:-risk_control_app}" -d "${RISK_POSTGRES_DB:-risk_control}" -Fc > "$WORK_DIR/risk_control_db.dump"
docker exec -i sub2api-postgres pg_restore --list < "$WORK_DIR/sub2api_db.dump" > "$WORK_DIR/sub2api_db.list"
docker exec -i risk-control-postgres pg_restore --list < "$WORK_DIR/risk_control_db.dump" > "$WORK_DIR/risk_control_db.list"
docker inspect sub2api sub2api-postgres sub2api-redis risk-control-postgres extensions-self > "$WORK_DIR/metadata/containers.json"
docker image inspect "$MAIN_REPOSITORY@$MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" > "$WORK_DIR/metadata/images.json"
git -C "$REPO" remote -v > "$WORK_DIR/metadata/git-remotes.txt"
git -C "$REPO" status --short --branch > "$WORK_DIR/metadata/git-status.txt"
jq -n --arg commit "$HEAD_COMMIT" --arg release "$CURRENT_RELEASE_ID" --arg main "$MAIN_DIGEST" --arg ext "$EXTENSIONS_DIGEST" \
  --arg created "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
  '{schema_version:1,source_commit:$commit,current_release_id:$release,main_digest:$main,extensions_digest:$ext,created_at:$created}' \
  > "$WORK_DIR/bundle.json"

(cd "$WORK_DIR" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
(cd "$WORK_DIR" && sha256sum -c SHA256SUMS >/dev/null)
mv -- "$WORK_DIR" "$OUTPUT"
WORK_DIR=''
printf 'site_bundle=%s\nsource_commit=%s\nrelease_id=%s\nmain_digest=%s\nextensions_digest=%s\n' \
  "$OUTPUT" "$HEAD_COMMIT" "$CURRENT_RELEASE_ID" "$MAIN_DIGEST" "$EXTENSIONS_DIGEST"
