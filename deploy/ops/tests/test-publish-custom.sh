#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
  printf 'publisher fixture test failed: %s\n' "$1" >&2
  exit 1
}

assert_eq() {
  local expected="$1"
  local actual="$2"
  local message="$3"
  [[ "$actual" == "$expected" ]] || fail "$message (expected=$expected actual=$actual)"
}

canonical_path() {
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -m "$1"
  else
    realpath "$1"
  fi
}

DIGEST_OLD_MAIN="sha256:$(printf '1%.0s' {1..64})"
DIGEST_OLD_EXTENSIONS="sha256:$(printf '2%.0s' {1..64})"
DIGEST_TARGET_MAIN="sha256:$(printf '3%.0s' {1..64})"
DIGEST_TARGET_EXTENSIONS="sha256:$(printf '4%.0s' {1..64})"
MAIN_REPOSITORY='ghcr.io/listencodes/sub2api-custom'
EXTENSIONS_REPOSITORY='ghcr.io/listencodes/sub2api-extensions'

SEED_REPO="$TMP_DIR/seed"
ORIGIN_REPO="$TMP_DIR/origin.git"
UPSTREAM_REPO="$TMP_DIR/upstream.git"
git init -q -b custom-release "$SEED_REPO"
git -C "$SEED_REPO" config user.name 'Publisher Fixture'
git -C "$SEED_REPO" config user.email 'publisher-fixture@example.com'
mkdir -p "$SEED_REPO/deploy"
cat > "$SEED_REPO/deploy/docker-compose.yml" <<'YAML'
services:
  sub2api:
    image: sub2api:custom
  extensions-self:
    image: deploy-extensions-self
YAML
printf 'deploy/.env\n' > "$SEED_REPO/.gitignore"
printf 'stable\n' > "$SEED_REPO/application.txt"
git -C "$SEED_REPO" add .
git -C "$SEED_REPO" commit -q -m stable
STABLE_COMMIT="$(git -C "$SEED_REPO" rev-parse HEAD)"
git -C "$SEED_REPO" tag -a v0.1.158 -m v0.1.158
TAG_OBJECT="$(git -C "$SEED_REPO" rev-parse 'v0.1.158^{tag}')"
jq -n \
  --arg tag v0.1.158 \
  --arg tag_object_sha "$TAG_OBJECT" \
  --arg commit_sha "$STABLE_COMMIT" \
  '{repository:"Wei-Shaw/sub2api",tag:$tag,tag_object_sha:$tag_object_sha,commit_sha:$commit_sha,published_at:"2026-07-16T12:37:06Z"}' \
  > "$SEED_REPO/deploy/stable-release-baseline.json"
git -C "$SEED_REPO" add deploy/stable-release-baseline.json
git -C "$SEED_REPO" commit -q -m baseline
PREVIOUS_COMMIT="$(git -C "$SEED_REPO" rev-parse HEAD)"
printf 'target\n' >> "$SEED_REPO/application.txt"
cat > "$SEED_REPO/deploy/docker-compose.yml" <<'YAML'
services:
  sub2api:
    image: ${SUB2API_IMAGE:?SUB2API_IMAGE is required}
  extensions-self:
    image: ${EXTENSIONS_SELF_IMAGE:?EXTENSIONS_SELF_IMAGE is required}
YAML
git -C "$SEED_REPO" commit -q -am target
INTERMEDIATE_COMMIT="$(git -C "$SEED_REPO" rev-parse HEAD)"
printf 'successor\n' >> "$SEED_REPO/application.txt"
git -C "$SEED_REPO" commit -q -am successor
TARGET_COMMIT="$(git -C "$SEED_REPO" rev-parse HEAD)"
git clone -q --bare "$SEED_REPO" "$ORIGIN_REPO"
git clone -q --bare "$SEED_REPO" "$UPSTREAM_REPO"

FAKE_BIN="$TMP_DIR/fake-bin"
mkdir -p "$FAKE_BIN"
cat > "$FAKE_BIN/verify-images" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'main_digest=%s\nextensions_digest=%s\n' "$DIGEST_TARGET_MAIN" "$DIGEST_TARGET_EXTENSIONS"
SH
cat > "$FAKE_BIN/nginx" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'nginx %s\n' "$*" >> "$FAKE_DOCKER_CALLS"
[[ "$*" == '-t' ]]
SH
cat > "$FAKE_BIN/curl" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'curl %s\n' "$*" >> "$FAKE_DOCKER_CALLS"
if [[ "$*" == *"$SUB2API_PUBLIC_HEALTH_URL"* \
  && ( "$PUBLISH_SCENARIO" == health-failure || "$PUBLISH_SCENARIO" == rollback-failure ) \
  && ! -e "$FAKE_DOCKER_STATE/public-health-failed" ]]; then
  touch "$FAKE_DOCKER_STATE/public-health-failed"
  exit 1
fi
SH
cat > "$FAKE_BIN/sleep" <<'SH'
#!/usr/bin/env sh
exit 0
SH
cat > "$FAKE_BIN/docker" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'docker %s\n' "$*" >> "$FAKE_DOCKER_CALLS"

if [[ "${1:-}" == pull ]]; then
  [[ -n "${DOCKER_CONFIG:-}" && -r "$DOCKER_CONFIG/config.json" ]]
  jq -e '.auths == {}' "$DOCKER_CONFIG/config.json" >/dev/null
fi

increment() {
  local name="$1"
  local path="$FAKE_DOCKER_STATE/$name"
  local value=0
  [[ ! -r "$path" ]] || value="$(cat "$path")"
  value=$((value + 1))
  printf '%s\n' "$value" > "$path"
  printf '%s\n' "$value"
}

if [[ "${1:-}" == container && "${2:-}" == inspect ]]; then
  case "${3:-}" in
    sub2api-risk-control|risk-control) exit 1 ;;
    *) printf '[]\n'; exit 0 ;;
  esac
fi

if [[ "${1:-}" == compose ]]; then
  if [[ "$*" == *'config --format json'* ]]; then
    cat <<'JSON'
{"services":{"sub2api":{"environment":{"RISK_CONTROL_URL":"http://extensions-self:8090"}},"extensions-self":{"environment":{"ACCOUNT_MONITOR_ENABLED":"false","ACCOUNT_MONITOR_SOURCE_DATABASE_URL":"","RISK_CONTROL_INTERNAL_SECRET":"fixture-secret"}},"postgres":{"environment":{"POSTGRES_USER":"sub2api","POSTGRES_DB":"sub2api"}},"risk-control-postgres":{"environment":{"POSTGRES_USER":"risk_control_app","POSTGRES_DB":"risk_control"}}}}
JSON
    exit 0
  fi
  if [[ "$*" == *'config --quiet'* ]]; then
    exit 0
  fi
  if [[ "$*" == *'up -d --no-deps --force-recreate extensions-self'* ]]; then
    count="$(increment extensions-up)"
    if [[ ( "$PUBLISH_SCENARIO" == extension-failure || "$PUBLISH_SCENARIO" == legacy-bootstrap-failure ) && "$count" -eq 1 ]]; then exit 1; fi
    if [[ "$PUBLISH_SCENARIO" == rollback-failure && "$count" -eq 2 ]]; then exit 1; fi
    exit 0
  fi
  if [[ "$*" == *'up -d --no-deps --force-recreate sub2api'* ]]; then
    increment main-up >/dev/null
    exit 0
  fi
fi

if [[ "${1:-}" == inspect ]]; then
  if [[ "$*" == *'.State.Health.Status'* ]]; then printf 'healthy\n'; exit 0; fi
  if [[ "$*" == *"{{.Image}}"* ]]; then
    case "${2:-}" in
      sub2api) printf 'sha256:%064d\n' 8 ;;
      extensions-self) printf 'sha256:%064d\n' 9 ;;
      *) printf 'sha256:%064d\n' 7 ;;
    esac
    exit 0
  fi
  printf '[]\n'
  exit 0
fi

if [[ "${1:-}" == image && "${2:-}" == inspect ]]; then printf '[]\n'; exit 0; fi
if [[ "${1:-}" == image && "${2:-}" == tag ]]; then exit 0; fi
if [[ "${1:-}" == ps || "${1:-}" == images ]]; then printf 'fixture\n'; exit 0; fi
if [[ "${1:-}" == pull ]]; then exit 0; fi
if [[ "${1:-}" == exec ]]; then
  if [[ "$*" == *'pg_dump '* ]]; then printf 'fixture-dump\n'; exit 0; fi
  if [[ "$*" == *'pg_restore --list'* ]]; then printf 'fixture-list\n'; exit 0; fi
  if [[ "$*" == *'wget '* ]]; then printf 'ok\n'; exit 0; fi
  exit 0
fi

printf 'unexpected fake docker command: %s\n' "$*" >&2
exit 2
SH
chmod +x "$FAKE_BIN"/*

setup_case() {
  local scenario="$1"
  CASE_DIR="$TMP_DIR/case-$scenario"
  REPO="$CASE_DIR/repo"
  DATA_DIR="$CASE_DIR/data"
  BACKUP_ROOT="$CASE_DIR/backups"
  mkdir -p "$CASE_DIR" "$DATA_DIR" "$BACKUP_ROOT" "$CASE_DIR/docker-state" "$CASE_DIR/certs"
  git clone -q "$ORIGIN_REPO" "$REPO"
  git -C "$REPO" remote add upstream "$UPSTREAM_REPO"
  local source_commit="$PREVIOUS_COMMIT"
  [[ "$scenario" != intermediate-source ]] || source_commit="$INTERMEDIATE_COMMIT"
  git -C "$REPO" switch -q --detach "$source_commit"
  git -C "$REPO" branch -f custom-release "$source_commit"
  git -C "$REPO" switch -q custom-release
  if [[ "$scenario" == legacy-bootstrap* ]]; then
    cat > "$REPO/deploy/.env" <<'EOF'
RISK_CONTROL_URL=http://extensions-self:8090
EOF
  else
    cat > "$REPO/deploy/.env" <<EOF
SUB2API_IMAGE=$MAIN_REPOSITORY@$DIGEST_OLD_MAIN
EXTENSIONS_SELF_IMAGE=$EXTENSIONS_REPOSITORY@$DIGEST_OLD_EXTENSIONS
EOF
  fi
  printf 'certificate\n' > "$CASE_DIR/certs/origin.crt"
  printf 'private-key\n' > "$CASE_DIR/certs/origin.key"
  cat > "$CASE_DIR/nginx.conf" <<EOF
ssl_certificate $CASE_DIR/certs/origin.crt;
ssl_certificate_key $CASE_DIR/certs/origin.key;
EOF

  SUB2API_DATA_DIR="$DATA_DIR"
  SUB2API_RELEASE_STATE_FILE="$DATA_DIR/release-state.json"
  source "$ROOT_DIR/deploy/ops/release-state.sh"
  JOB_ID="update-publisher-$scenario"
  release_job_init "$JOB_ID"
  if [[ "$scenario" != legacy-bootstrap* ]]; then
    release_production_state_write "$(jq -n \
      --arg production_commit "$PREVIOUS_COMMIT" \
      --arg main_digest "$DIGEST_OLD_MAIN" \
      --arg extensions_digest "$DIGEST_OLD_EXTENSIONS" \
      '{production_commit:$production_commit,stable_release_tag:"v0.1.158",stable_release_commit:"'"$STABLE_COMMIT"'",main_digest:$main_digest,extensions_digest:$extensions_digest,published_at:"2026-07-16T12:00:00Z",backup_dir:"/root/backups/sub2api/previous"}')"
  fi
  : > "$CASE_DIR/docker-calls"
}

run_case() {
  local scenario="$1"
  local exit_code
  setup_case "$scenario"
  set +e
  PATH="$FAKE_BIN:$PATH" \
  PUBLISH_SCENARIO="$scenario" \
  FAKE_DOCKER_CALLS="$CASE_DIR/docker-calls" \
  FAKE_DOCKER_STATE="$CASE_DIR/docker-state" \
  DIGEST_TARGET_MAIN="$DIGEST_TARGET_MAIN" \
  DIGEST_TARGET_EXTENSIONS="$DIGEST_TARGET_EXTENSIONS" \
  SUB2API_REPO="$REPO" \
  SUB2API_DATA_DIR="$DATA_DIR" \
  SUB2API_RELEASE_STATE_FILE="$DATA_DIR/release-state.json" \
  SUB2API_JOB_ID="$JOB_ID" \
  SUB2API_RELEASE_STATE_HELPER="$ROOT_DIR/deploy/ops/release-state.sh" \
  SUB2API_VERIFY_IMAGES_SCRIPT="$FAKE_BIN/verify-images" \
  SUB2API_ENV_FILE="$REPO/deploy/.env" \
  SUB2API_BACKUP_ROOT="$BACKUP_ROOT" \
  SUB2API_NGINX_VHOST="$CASE_DIR/nginx.conf" \
  SUB2API_PUBLIC_HEALTH_URL='https://fixture.example/health' \
  SUB2API_PUBLISH_LOG="$CASE_DIR/publish.log" \
  "$ROOT_DIR/deploy/ops/publish-custom.sh" \
    --commit "$TARGET_COMMIT" \
    --main-digest "$DIGEST_TARGET_MAIN" \
    --extensions-digest "$DIGEST_TARGET_EXTENSIONS"
  exit_code=$?
  set -e
  RUN_EXIT="$exit_code"
  JOB_FILE="$DATA_DIR/release-jobs/$JOB_ID.json"
  BACKUP_DIR="$(find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
}

run_case healthy
assert_eq 0 "$RUN_EXIT" 'healthy publication failed'
assert_eq success "$(jq -r '.status' "$JOB_FILE")" 'healthy publication did not persist success'
assert_eq "$TARGET_COMMIT" "$(jq -r '.production_commit' "$DATA_DIR/release-state.json")" 'healthy publication did not update production state'
assert_eq "$TARGET_COMMIT" "$(git -C "$REPO" rev-parse HEAD)" 'publisher did not fast-forward source after backup'
grep -q "^SUB2API_IMAGE=$MAIN_REPOSITORY@$DIGEST_TARGET_MAIN$" "$REPO/deploy/.env" || fail 'healthy publication did not pin the main digest'
grep -q "^EXTENSIONS_SELF_IMAGE=$EXTENSIONS_REPOSITORY@$DIGEST_TARGET_EXTENSIONS$" "$REPO/deploy/.env" || fail 'healthy publication did not pin the extensions digest'
extensions_line="$(grep -n 'up -d --no-deps --force-recreate extensions-self' "$CASE_DIR/docker-calls" | head -n 1 | cut -d: -f1)"
main_line="$(grep -n 'up -d --no-deps --force-recreate sub2api' "$CASE_DIR/docker-calls" | head -n 1 | cut -d: -f1)"
[[ -n "$extensions_line" && -n "$main_line" && "$extensions_line" -lt "$main_line" ]] || fail 'healthy deployment was not staged extensions-first'
[[ -s "$BACKUP_DIR/sub2api_db.dump" && -s "$BACKUP_DIR/risk_control_db.dump" ]] || fail 'database dumps were not backed up'
[[ -s "$BACKUP_DIR/SHA256SUMS" && -s "$BACKUP_DIR/container-metadata.json" ]] || fail 'rollback metadata is incomplete'
grep -q 'docker image tag .* sub2api:rollback-' "$CASE_DIR/docker-calls" || fail 'main rollback image was not tagged'
grep -q 'docker image tag .* deploy-extensions-self:rollback-' "$CASE_DIR/docker-calls" || fail 'extensions rollback image was not tagged'

run_case intermediate-source
assert_eq 0 "$RUN_EXIT" 'publisher could not advance from a clean intermediate source commit'
assert_eq "$TARGET_COMMIT" "$(git -C "$REPO" rev-parse HEAD)" 'intermediate source did not fast-forward to the approved target'

run_case legacy-bootstrap
assert_eq 0 "$RUN_EXIT" 'legacy bootstrap publication failed'
assert_eq success "$(jq -r '.status' "$JOB_FILE")" 'legacy bootstrap did not persist success'
assert_eq "$TARGET_COMMIT" "$(jq -r '.production_commit' "$DATA_DIR/release-state.json")" 'legacy bootstrap did not create the first digest state'
grep -q '^LEGACY_BOOTSTRAP=true$' "$BACKUP_DIR/release-metadata.env" || fail 'legacy bootstrap was not recorded in rollback evidence'

run_case legacy-bootstrap-failure
[[ "$RUN_EXIT" -ne 0 ]] || fail 'legacy bootstrap failure unexpectedly succeeded'
assert_eq true "$(jq -r '.rollback.succeeded' "$JOB_FILE")" 'legacy bootstrap failure did not restore the old deployment'
[[ ! -e "$DATA_DIR/release-state.json" ]] || fail 'failed legacy bootstrap created a healthy production state'
grep -q -- "-f $BACKUP_DIR/main-docker-compose.yml" "$CASE_DIR/docker-calls" || fail 'legacy bootstrap rollback did not use the old Compose file'
grep -q '^RISK_CONTROL_URL=http://extensions-self:8090$' "$REPO/deploy/.env" || fail 'legacy bootstrap rollback did not restore the old environment'

for scenario in extension-failure health-failure; do
  run_case "$scenario"
  [[ "$RUN_EXIT" -ne 0 ]] || fail "$scenario unexpectedly succeeded"
  assert_eq failed "$(jq -r '.status' "$JOB_FILE")" "$scenario did not persist failure"
  assert_eq true "$(jq -r '.rollback.attempted' "$JOB_FILE")" "$scenario did not attempt rollback"
  assert_eq true "$(jq -r '.rollback.succeeded' "$JOB_FILE")" "$scenario did not record rollback success"
  grep -q "^SUB2API_IMAGE=$MAIN_REPOSITORY@$DIGEST_OLD_MAIN$" "$REPO/deploy/.env" || fail "$scenario did not restore the main image reference"
  grep -q "^EXTENSIONS_SELF_IMAGE=$EXTENSIONS_REPOSITORY@$DIGEST_OLD_EXTENSIONS$" "$REPO/deploy/.env" || fail "$scenario did not restore the extensions image reference"
  assert_eq "$PREVIOUS_COMMIT" "$(jq -r '.production_commit' "$DATA_DIR/release-state.json")" "$scenario changed the healthy production state"
done

run_case rollback-failure
[[ "$RUN_EXIT" -ne 0 ]] || fail 'rollback failure scenario unexpectedly succeeded'
assert_eq failed "$(jq -r '.status' "$JOB_FILE")" 'rollback failure did not persist failure'
assert_eq true "$(jq -r '.rollback.attempted' "$JOB_FILE")" 'rollback failure did not record an attempt'
assert_eq false "$(jq -r '.rollback.succeeded' "$JOB_FILE")" 'rollback failure was reported as successful'
assert_eq "$(canonical_path "$BACKUP_DIR")" "$(canonical_path "$(jq -r '.artifact_path' "$JOB_FILE")")" 'rollback failure did not expose its backup evidence'
[[ -s "$BACKUP_DIR/release-metadata.env" && -s "$BACKUP_DIR/SHA256SUMS" ]] || fail 'rollback failure evidence is incomplete'

if grep -Eq 'docker (build|compose .* (build|down|rm))' "$TMP_DIR"/case-*/docker-calls; then
  fail 'publisher built images or managed database/application lifecycle broadly'
fi
if grep -Eq 'risk-control-postgres.*(^|[[:space:]])(up|rm|down)([[:space:]]|$)|(^|[[:space:]])(up|rm|down)([[:space:]]|$).*risk-control-postgres' "$TMP_DIR"/case-*/docker-calls; then
  fail 'publisher recreated or removed risk-control-postgres'
fi
if grep -Eq 'pg_restore[^\n]*(--clean|--if-exists|-d )' "$TMP_DIR"/case-*/docker-calls; then
  fail 'publisher restored a database automatically'
fi

printf 'publisher fixture tests: PASS\n'
