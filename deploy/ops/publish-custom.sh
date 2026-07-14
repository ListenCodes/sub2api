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
rendered_risk_url="$(
  docker compose --project-name deploy -f "$COMPOSE" --env-file "$ENV_FILE" config --format json |
    python3 -c 'import json, sys; print(json.load(sys.stdin)["services"]["sub2api"]["environment"].get("RISK_CONTROL_URL", ""))'
)" || fail 'could not read rendered risk-control URL'
[[ "$rendered_risk_url" == 'http://extensions-self:8090' ]] || fail "rendered RISK_CONTROL_URL must be http://extensions-self:8090, got $rendered_risk_url"

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
old_extension_image="$(docker inspect extensions-self --format '{{.Image}}' 2>/dev/null || docker inspect risk-control --format '{{.Image}}' 2>/dev/null || true)"
if [[ -n "$old_extension_image" ]]; then
  docker tag "$old_extension_image" "deploy-extensions-self:rollback-$STAMP"
fi
sha256sum "$BACKUP_DIR/sub2api_db.dump" > "$BACKUP_DIR/SHA256SUMS"

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
