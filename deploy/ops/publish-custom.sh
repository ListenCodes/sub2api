#!/usr/bin/env bash
set -Eeuo pipefail

REPO="${SUB2API_REPO:-/root/sub2api}"
COMPOSE="$REPO/deploy/docker-compose.yml"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
BACKUP_ROOT="${SUB2API_BACKUP_ROOT:-/root/backups/sub2api}"
LOG="${SUB2API_PUBLISH_LOG:-/var/log/sub2api-publish.log}"
STAMP="$(date -u '+%Y%m%d-%H%M%S')"
BACKUP_DIR="$BACKUP_ROOT/$STAMP"

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1" | tee -a "$LOG"
}

fail() {
  log "FAILED: $1"
  exit 1
}

[[ "${1:-}" == "--commit" && -n "${2:-}" ]] || fail 'usage: publish-custom.sh --commit <approved origin/custom commit>'
APPROVED_COMMIT="$2"

mkdir -p "$BACKUP_DIR" "$(dirname "$LOG")"
touch "$LOG"
cd "$REPO"

[[ "$(git branch --show-current)" == custom ]] || fail 'VPS source branch must be custom'
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || fail 'VPS source worktree is dirty'

git fetch origin custom >> "$LOG" 2>&1 || fail 'fetch origin/custom failed'
ORIGIN_COMMIT="$(git rev-parse origin/custom)"
[[ "$APPROVED_COMMIT" == "$ORIGIN_COMMIT" ]] || fail "approved commit $APPROVED_COMMIT is not origin/custom $ORIGIN_COMMIT"
git merge --ff-only origin/custom >> "$LOG" 2>&1 || fail 'fast-forward to origin/custom failed'
[[ "$(git rev-parse HEAD)" == "$APPROVED_COMMIT" ]] || fail 'source HEAD is not the approved commit'

if docker container inspect sub2api-risk-control >/dev/null 2>&1; then
  fail 'retired legacy container sub2api-risk-control still exists'
fi

docker compose --project-name deploy -f "$COMPOSE" --env-file "$ENV_FILE" config --quiet || fail 'compose configuration invalid'
rendered_compose="$(docker compose --project-name deploy -f "$COMPOSE" --env-file "$ENV_FILE" config --format json)" || fail 'could not render compose configuration'
rendered_values="$(printf '%s' "$rendered_compose" | python3 -c '
import json, sys
config = json.load(sys.stdin)["services"]
sub2api = config["sub2api"]["environment"]
extensions = config["extensions-self"]["environment"]
postgres = config["postgres"]["environment"]
values = [
    sub2api.get("RISK_CONTROL_URL", ""),
    extensions.get("ACCOUNT_MONITOR_ENABLED", "false"),
    extensions.get("ACCOUNT_MONITOR_SOURCE_DATABASE_URL", ""),
    extensions.get("RISK_CONTROL_INTERNAL_SECRET", ""),
    postgres.get("POSTGRES_USER", "sub2api"),
    postgres.get("POSTGRES_DB", "sub2api"),
]
print("\n".join(str(value) for value in values))
')" || fail 'could not read rendered extension configuration'
mapfile -t deploy_values <<< "$rendered_values"
[[ "${#deploy_values[@]}" -eq 6 ]] || fail 'rendered extension configuration is incomplete'
rendered_risk_url="${deploy_values[0]}"
monitor_enabled="$(printf '%s' "${deploy_values[1]}" | tr '[:upper:]' '[:lower:]')"
monitor_source_url="${deploy_values[2]}"
rendered_internal_secret="${deploy_values[3]}"
postgres_owner="${deploy_values[4]}"
postgres_database="${deploy_values[5]}"
[[ "$rendered_risk_url" == 'http://extensions-self:8090' ]] || fail "rendered RISK_CONTROL_URL must be http://extensions-self:8090, got $rendered_risk_url"
[[ "$monitor_enabled" == true || "$monitor_enabled" == false ]] || fail 'ACCOUNT_MONITOR_ENABLED must be true or false'

monitor_password=''
if [[ "$monitor_enabled" == true ]]; then
  monitor_password="$(
    ACCOUNT_MONITOR_SOURCE_DATABASE_URL="$monitor_source_url" EXPECTED_DATABASE="$postgres_database" python3 -c '
import os, sys
from urllib.parse import unquote, urlparse
parsed = urlparse(os.environ.get("ACCOUNT_MONITOR_SOURCE_DATABASE_URL", ""))
password = unquote(parsed.password or "")
valid = (
    parsed.scheme in {"postgres", "postgresql"}
    and unquote(parsed.username or "") == "extensions_self_monitor"
    and parsed.hostname == "postgres"
    and (parsed.port or 5432) == 5432
    and parsed.path.lstrip("/") == os.environ.get("EXPECTED_DATABASE", "")
    and password != ""
    and "\n" not in password
    and "\r" not in password
)
if not valid:
    sys.exit(1)
print(password, end="")
'
  )" || fail 'enabled account monitor DSN must use extensions_self_monitor on postgres with a non-empty password'
  [[ -n "$rendered_internal_secret" ]] || fail 'account monitor readiness checks require RISK_CONTROL_INTERNAL_SECRET'
fi

log "Backing up approved release $APPROVED_COMMIT"
docker exec sub2api-postgres pg_dump -U sub2api -d sub2api -Fc > "$BACKUP_DIR/sub2api_db.dump" || fail 'database backup failed'
cp -p "$ENV_FILE" "$BACKUP_DIR/main.env"
cp -p "$COMPOSE" "$BACKUP_DIR/main-docker-compose.yml"
cp -p /etc/nginx/sites-available/sub.ailisten.top "$BACKUP_DIR/nginx-sub.ailisten.top.conf" 2>/dev/null || true
docker inspect sub2api extensions-self risk-control risk-control-postgres sub2api-postgres sub2api-redis > "$BACKUP_DIR/container-metadata.json" 2>/dev/null || true
docker image inspect sub2api:custom deploy-extensions-self deploy-risk-control > "$BACKUP_DIR/image-metadata.json" 2>/dev/null || true

old_main_image="$(docker inspect sub2api --format '{{.Image}}' 2>/dev/null || true)"
if [[ -n "$old_main_image" ]]; then
  docker tag "$old_main_image" "sub2api:rollback-$STAMP"
fi
old_extension_image=""
if docker container inspect extensions-self >/dev/null 2>&1; then
  old_extension_image="$(docker inspect extensions-self --format '{{.Image}}')"
elif docker container inspect risk-control >/dev/null 2>&1; then
  old_extension_image="$(docker inspect risk-control --format '{{.Image}}')"
fi
if [[ -n "$old_extension_image" ]]; then
  docker tag "$old_extension_image" "deploy-extensions-self:rollback-$STAMP"
fi
sha256sum "$BACKUP_DIR/sub2api_db.dump" > "$BACKUP_DIR/SHA256SUMS"

if [[ "$monitor_enabled" == true ]]; then
  log 'Installing account monitor source views and dedicated login role'
  source_stage="/tmp/account-monitor-source-$STAMP"
  docker exec sub2api-postgres mkdir -p \
    "$source_stage/deploy/ops" \
    "$source_stage/extensions-self/account-monitor/sql" || fail 'could not prepare source SQL staging directory'
  docker cp "$REPO/deploy/ops/install-account-monitor-source.sql" \
    "sub2api-postgres:$source_stage/deploy/ops/install-account-monitor-source.sql" || fail 'could not stage source installer'
  docker cp "$REPO/extensions-self/account-monitor/sql/main_source_views.sql" \
    "sub2api-postgres:$source_stage/extensions-self/account-monitor/sql/main_source_views.sql" || fail 'could not stage source views'
  if ! docker exec sub2api-postgres psql \
    -U "$postgres_owner" \
    -d "$postgres_database" \
    -v ON_ERROR_STOP=1 \
    -v account_monitor_password="$monitor_password" \
    -f "$source_stage/deploy/ops/install-account-monitor-source.sql" >> "$LOG" 2>&1; then
    docker exec sub2api-postgres rm -rf "$source_stage" >/dev/null 2>&1 || true
    fail 'account monitor source installation failed'
  fi
  docker exec sub2api-postgres rm -rf "$source_stage" >/dev/null 2>&1 || true

  docker exec sub2api-postgres psql \
    -U "$postgres_owner" \
    -d "$postgres_database" \
    -v ON_ERROR_STOP=1 \
    -c 'BEGIN; SET ROLE extensions_self_monitor_ro; SELECT 1 FROM extensions_self_ro.usage_source LIMIT 1; ROLLBACK;' \
    >> "$LOG" 2>&1 || fail 'source privilege role probe failed'
  docker exec -e PGPASSWORD="$monitor_password" sub2api-postgres psql \
    -h 127.0.0.1 \
    -U extensions_self_monitor \
    -d "$postgres_database" \
    -v ON_ERROR_STOP=1 \
    -c 'SELECT 1 FROM extensions_self_ro.usage_source LIMIT 1;' \
    >> "$LOG" 2>&1 || fail 'source login read probe failed'
  if docker exec -e PGPASSWORD="$monitor_password" sub2api-postgres psql \
    -h 127.0.0.1 \
    -U extensions_self_monitor \
    -d "$postgres_database" \
    -v ON_ERROR_STOP=1 \
    -c 'SELECT key FROM public.api_keys LIMIT 1;' \
    >> "$LOG" 2>&1; then
    fail 'source login unexpectedly accessed the full API key column'
  fi
  if docker exec -e PGPASSWORD="$monitor_password" sub2api-postgres psql \
    -h 127.0.0.1 \
    -U extensions_self_monitor \
    -d "$postgres_database" \
    -v ON_ERROR_STOP=1 \
    -c 'SELECT credentials FROM public.accounts LIMIT 1;' \
    >> "$LOG" 2>&1; then
    fail 'source login unexpectedly accessed account credentials'
  fi
  log 'Account monitor source privilege probes passed'
fi

log 'Building main application image'
docker build -t sub2api:custom "$REPO" >> "$LOG" 2>&1 || fail 'main image build failed'
log 'Building extensions-self image'
docker compose --project-name deploy -f "$COMPOSE" --env-file "$ENV_FILE" build extensions-self >> "$LOG" 2>&1 || fail 'extensions-self image build failed'

log 'Recreating only main and extensions-self services'
docker compose --project-name deploy -f "$COMPOSE" --env-file "$ENV_FILE" up -d --no-deps --force-recreate sub2api extensions-self >> "$LOG" 2>&1 || fail 'service recreate failed'

for attempt in $(seq 1 60); do
  main_health="$(docker inspect sub2api --format '{{.State.Health.Status}}' 2>/dev/null || true)"
  extension_health="$(docker inspect extensions-self --format '{{.State.Health.Status}}' 2>/dev/null || true)"
  if [[ "$main_health" == healthy && "$extension_health" == healthy ]] && curl -fsS http://127.0.0.1:8081/health >/dev/null; then
    docker exec extensions-self wget -qO- -T 5 http://extensions-self:8090/healthz >/dev/null || fail 'extensions-self health check failed'
    curl -fsS http://127.0.0.1:8081/api/v1/extensions-self/homepage/ >/dev/null || fail 'public homepage proxy health check failed'
    if [[ "$monitor_enabled" == true ]]; then
      docker exec extensions-self wget -qO- -T 5 http://extensions-self:8090/account-monitor/ >/dev/null || fail 'account monitor static health check failed'
      monitor_timestamp="$(date +%s)"
      monitor_nonce="publish-$STAMP-$attempt"
      monitor_signature="$(
        MONITOR_SECRET="$rendered_internal_secret" MONITOR_TIMESTAMP="$monitor_timestamp" MONITOR_NONCE="$monitor_nonce" python3 -c '
import hashlib, hmac, os
message = (os.environ.get("MONITOR_TIMESTAMP", "") + "\n" + os.environ.get("MONITOR_NONCE", "") + "\n").encode()
print(hmac.new(os.environ["MONITOR_SECRET"].encode(), message, hashlib.sha256).hexdigest())
'
      )" || fail 'could not sign account monitor readiness request'
      docker exec extensions-self wget -qO- -T 5 \
        --header="X-Risk-Timestamp: $monitor_timestamp" \
        --header="X-Risk-Nonce: $monitor_nonce" \
        --header="X-Risk-Signature: $monitor_signature" \
        --header='X-Risk-Actor-ID: 1' \
        http://extensions-self:8090/api/v1/admin/account-monitor/data-quality \
        >/dev/null || fail 'account monitor API readiness check failed'
      monitor_proxy_status="$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:8081/api/v1/extensions-self/account-monitor/)" || fail 'account monitor proxy route check failed'
      [[ "$monitor_proxy_status" == 401 || "$monitor_proxy_status" == 423 ]] || fail "account monitor proxy returned unexpected HTTP $monitor_proxy_status"
    fi
    if docker container inspect risk-control >/dev/null 2>&1; then
      docker rm -f risk-control >> "$LOG" 2>&1 || fail 'retired risk-control container removal failed'
    fi
    docker run --rm --entrypoint /app/sub2api sub2api:custom --version 2>&1 | tee -a "$LOG"
    log "PUBLISH OK: commit=$APPROVED_COMMIT backup=$BACKUP_DIR"
    exit 0
  fi
  sleep 2
done

fail "health check failed; rollback tags and backup remain at $BACKUP_DIR"
