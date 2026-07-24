#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BOOTSTRAP="$ROOT/deploy/ops/bootstrap-custom-site.sh"
FIXTURES="$ROOT/deploy/tests/fixtures/bootstrap"
TMP="$(mktemp -d)"
if [[ "${KEEP_BOOTSTRAP_TEST_TMP:-0}" == 1 ]]; then
  trap 'printf "fixture_tmp=%s\n" "$TMP"' EXIT
else
  trap 'rm -rf -- "$TMP"' EXIT
fi

pass=0
failures=0
ok() { printf 'ok - %s\n' "$1"; pass=$((pass + 1)); }
not_ok() { printf 'not ok - %s\n' "$1" >&2; failures=$((failures + 1)); }
expect_success() {
  local name="$1"; shift
  if "$@" >"$TMP/stdout" 2>"$TMP/stderr"; then ok "$name"; else cat "$TMP/stderr" >&2; not_ok "$name"; fi
}
expect_failure() {
  local name="$1"; shift
  if "$@" >"$TMP/stdout" 2>"$TMP/stderr"; then not_ok "$name"; else ok "$name"; fi
}

chmod +x "$FIXTURES/bin/"* "$FIXTURES/verify-release-images.sh"
export PATH="$FIXTURES/bin:$PATH"
export BOOTSTRAP_COMMAND_LOG="$TMP/commands.log"
export SUB2API_ALLOW_NON_ROOT_FOR_TESTS=1
export SUB2API_HOST_OS=Linux
export SUB2API_HOST_ARCH=x86_64
export SUB2API_RELEASE_COMMON_HELPER="$ROOT/deploy/ops/release-common.sh"
export SUB2API_RELEASE_LEDGER_HELPER="$ROOT/deploy/ops/release-ledger.sh"
export SUB2API_VERIFY_IMAGES_SCRIPT="$FIXTURES/verify-release-images.sh"
export SUB2API_OPS_INSTALL_ROOT="$TMP/ops-install"
export SUB2API_SYSTEMD_ROOT="$TMP/systemd"
export SUB2API_PUBLIC_HEALTH_URL=https://example.invalid/health
mkdir -p "$SUB2API_SYSTEMD_ROOT"

REPO="$TMP/repo"
mkdir -p "$REPO/deploy"
cp "$ROOT/deploy/docker-compose.yml" "$REPO/deploy/docker-compose.yml"
cp "$ROOT/deploy/docker-compose.custom.yml" "$REPO/deploy/docker-compose.custom.yml"
git -C "$REPO" init -q -b custom-release
git -C "$REPO" config user.name fixture
git -C "$REPO" config user.email fixture@example.invalid
git -C "$REPO" add deploy/docker-compose.yml deploy/docker-compose.custom.yml
git -C "$REPO" commit -qm 'fixture base'
STABLE_COMMIT="$(git -C "$REPO" rev-parse HEAD)"
jq -n --arg commit "$STABLE_COMMIT" '{repository:"fixture/sub2api",tag:"v0.1.164",tag_object_sha:$commit,commit_sha:$commit,published_at:"2026-07-23T09:54:02Z"}' > "$REPO/deploy/stable-release-baseline.json"
git -C "$REPO" add deploy/stable-release-baseline.json
git -C "$REPO" commit -qm 'fixture custom release'
HEAD_COMMIT="$(git -C "$REPO" rev-parse HEAD)"
git -C "$REPO" update-ref refs/remotes/origin/custom-release "$HEAD_COMMIT"

SECRET_ENV="$TMP/site.env"
printf 'POSTGRES_PASSWORD=test\nRISK_CONTROL_POSTGRES_PASSWORD=test\nRISK_CONTROL_INTERNAL_SECRET=test\n' > "$SECRET_ENV"
chmod 0600 "$SECRET_ENV"
export SUB2API_REPO="$REPO"
export SUB2API_DATA_DIR="$TMP/data"
export SUB2API_ENV_FILE="$REPO/deploy/.env"
export SUB2API_COMPOSE_BASE="$REPO/deploy/docker-compose.yml"
export SUB2API_COMPOSE_CUSTOM="$REPO/deploy/docker-compose.custom.yml"
export SUB2API_STABLE_BASELINE_FILE="$REPO/deploy/stable-release-baseline.json"

expect_failure 'fresh rejects the wrong confirmation' \
  "$BOOTSTRAP" fresh --env-file "$SECRET_ENV" --confirm WRONG --check-only

: > "$BOOTSTRAP_COMMAND_LOG"
expect_success 'fresh check-only validates without runtime mutation' \
  "$BOOTSTRAP" fresh --env-file "$SECRET_ENV" --confirm FRESH-EMPTY-SITE --check-only
if grep -Eq 'compose .* (up|pull)|volume create|systemctl' "$BOOTSTRAP_COMMAND_LOG"; then
  not_ok 'fresh check-only emitted a runtime mutation'
else
  ok 'fresh check-only emitted no runtime mutation'
fi

FAKE_INVALID_DIGESTS=1 expect_failure 'fresh rejects non-digest image evidence' \
  "$BOOTSTRAP" fresh --env-file "$SECRET_ENV" --confirm FRESH-EMPTY-SITE --check-only
FAKE_DOCKER_EXISTING=1 expect_failure 'fresh rejects an existing target resource' \
  "$BOOTSTRAP" fresh --env-file "$SECRET_ENV" --confirm FRESH-EMPTY-SITE --check-only

touch "$REPO/untracked"
expect_failure 'fresh rejects a dirty checkout' \
  "$BOOTSTRAP" fresh --env-file "$SECRET_ENV" --confirm FRESH-EMPTY-SITE --check-only
rm -f "$REPO/untracked"

make_backup_contract() {
  local backup="$1" projection="$2"
  mkdir -p "$backup/target"
  cp "$REPO/deploy/docker-compose.yml" "$backup/docker-compose.yml"
  cp "$REPO/deploy/docker-compose.custom.yml" "$backup/docker-compose.custom.yml"
  cp "$SECRET_ENV" "$backup/.env"
  cp "$projection" "$backup/release-state.json"
  cp "$REPO/deploy/docker-compose.yml" "$backup/target/docker-compose.yml"
  cp "$REPO/deploy/docker-compose.custom.yml" "$backup/target/docker-compose.custom.yml"
  cp "$SECRET_ENV" "$backup/target/.env"
  printf '{"name":"deploy"}\n' > "$backup/target/rendered-compose.json"
  for file in container-metadata.json image-metadata.txt rollback-tags.txt sub2api_db.dump sub2api_db.list risk_control_db.dump risk_control_db.list docker-containers.txt docker-images.txt; do
    printf 'fixture\n' > "$backup/$file"
  done
  printf '/etc/nginx/site.conf\n' > "$backup/nginx-vhost.path"
  printf '/etc/nginx/site.crt\n' > "$backup/origin-cert.path"
  printf '/etc/nginx/site.key\n' > "$backup/origin-key.path"
  printf 'server {}\n' > "$backup/site.conf"
  printf 'certificate\n' > "$backup/site.crt"
  printf 'private-key\n' > "$backup/site.key"
  (cd "$backup/target" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
  (cd "$backup" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
}

BUNDLE="$TMP/bundle"
BACKUP="$BUNDLE/release-backups/backup-1"
export MSYS2_ARG_CONV_EXCL="$SUB2API_DATA_DIR/release-backups/;$BUNDLE/release-backups/"
RELEASE_ID="release-bootstrap-fixture-${HEAD_COMMIT:0:9}"
MAIN_DIGEST="sha256:$(printf '%064d' 1)"
EXT_DIGEST="sha256:$(printf '%064d' 2)"
mkdir -p "$BUNDLE/config" "$BUNDLE/nginx" "$BUNDLE/release-ledger/releases" "$BUNDLE/release-ledger/operations"
PROJECTION="$TMP/projection.json"
jq -n --arg commit "$HEAD_COMMIT" --arg stable "$STABLE_COMMIT" --arg release "$RELEASE_ID" --arg main "$MAIN_DIGEST" --arg ext "$EXT_DIGEST" --arg backup "$SUB2API_DATA_DIR/release-backups/backup-1" \
  '{production_commit:$commit,stable_release_tag:"v0.1.164",stable_release_commit:$stable,main_digest:$main,extensions_digest:$ext,published_at:"2026-07-24T00:00:00Z",backup_dir:$backup,release_id:$release,official_version:"v0.1.164",custom_version:"v1.0.2",custom_version_sequence:2}' > "$PROJECTION"
make_backup_contract "$BACKUP" "$PROJECTION"
cp "$PROJECTION" "$BUNDLE/release-state.json"
cp "$SECRET_ENV" "$BUNDLE/config/.env"
chmod 0600 "$BUNDLE/config/.env"
cp "$REPO/deploy/docker-compose.yml" "$BUNDLE/config/docker-compose.yml"
cp "$REPO/deploy/docker-compose.custom.yml" "$BUNDLE/config/docker-compose.custom.yml"
cp "$BACKUP/site.conf" "$BUNDLE/nginx/site.conf"
cp "$BACKUP/site.crt" "$BUNDLE/nginx/site.crt"
cp "$BACKUP/site.key" "$BUNDLE/nginx/site.key"
cp "$BACKUP/nginx-vhost.path" "$BUNDLE/nginx/nginx-vhost.path"
cp "$BACKUP/origin-cert.path" "$BUNDLE/nginx/origin-cert.path"
cp "$BACKUP/origin-key.path" "$BUNDLE/nginx/origin-key.path"
printf 'dump\n' > "$BUNDLE/sub2api_db.dump"
printf 'list\n' > "$BUNDLE/sub2api_db.list"
printf 'dump\n' > "$BUNDLE/risk_control_db.dump"
printf 'list\n' > "$BUNDLE/risk_control_db.list"
jq -n --arg release "$RELEASE_ID" --arg now '2026-07-24T00:00:00Z' '{schema_version:1,current_release_id:$release,custom_version_high_water:2,active_operation_id:null,updated_at:$now}' > "$BUNDLE/release-ledger/state.json"
BASE_HASH="$(sha256sum "$BACKUP/target/docker-compose.yml" | awk '{print $1}')"
CUSTOM_HASH="$(sha256sum "$BACKUP/target/docker-compose.custom.yml" | awk '{print $1}')"
RENDERED_HASH="$(sha256sum "$BACKUP/target/rendered-compose.json" | awk '{print $1}')"
ENV_HASH="$(sha256sum "$BACKUP/target/.env" | awk '{print $1}')"
BACKUP_HASH="$(sha256sum "$BACKUP/SHA256SUMS" | awk '{print $1}')"
jq -n --arg release "$RELEASE_ID" --arg official_commit "$STABLE_COMMIT" --arg custom_commit "$HEAD_COMMIT" --arg main "$MAIN_DIGEST" --arg ext "$EXT_DIGEST" \
  --arg base_hash "$BASE_HASH" --arg custom_hash "$CUSTOM_HASH" --arg rendered_hash "$RENDERED_HASH" --arg env_hash "$ENV_HASH" --arg backup "$SUB2API_DATA_DIR/release-backups/backup-1" --arg backup_hash "$BACKUP_HASH" \
  '{schema_version:1,release_id:$release,official_version:"v0.1.164",official_commit:$official_commit,custom_version:"v1.0.2",custom_version_sequence:2,custom_commit:$custom_commit,main_digest:$main,extensions_digest:$ext,base_compose_sha256:$base_hash,custom_compose_sha256:$custom_hash,rendered_compose_sha256:$rendered_hash,env_sha256:$env_hash,backup_dir:$backup,backup_manifest_sha256:$backup_hash,published_at:"2026-07-24T00:00:00Z",source_kind:"bootstrap",operation_id:"fixture-bootstrap"}' > "$BUNDLE/release-ledger/releases/$RELEASE_ID.json"
jq -n --arg commit "$HEAD_COMMIT" --arg release "$RELEASE_ID" --arg main "$MAIN_DIGEST" --arg ext "$EXT_DIGEST" '{schema_version:1,source_commit:$commit,current_release_id:$release,main_digest:$main,extensions_digest:$ext,created_at:"2026-07-24T00:00:00Z"}' > "$BUNDLE/bundle.json"
(cd "$BUNDLE" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)

: > "$BOOTSTRAP_COMMAND_LOG"
expect_success 'migrate check-only validates a complete bundle without runtime mutation' \
  "$BOOTSTRAP" migrate --bundle "$BUNDLE" --confirm RESTORE-MIGRATION --check-only
if grep -Eq 'compose .* (up|pull)|volume create|systemctl' "$BOOTSTRAP_COMMAND_LOG"; then
  not_ok 'migrate check-only emitted a runtime mutation'
else
  ok 'migrate check-only emitted no runtime mutation'
fi

jq '.main_digest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"' \
  "$BUNDLE/release-state.json" > "$BUNDLE/release-state.tmp"
mv "$BUNDLE/release-state.tmp" "$BUNDLE/release-state.json"
(cd "$BUNDLE" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
expect_failure 'migrate rejects an internally inconsistent projection with valid checksums' \
  "$BOOTSTRAP" migrate --bundle "$BUNDLE" --confirm RESTORE-MIGRATION --check-only
jq --arg main "$MAIN_DIGEST" '.main_digest = $main' "$BUNDLE/release-state.json" > "$BUNDLE/release-state.tmp"
mv "$BUNDLE/release-state.tmp" "$BUNDLE/release-state.json"

jq '.custom_version_high_water = 1' "$BUNDLE/release-ledger/state.json" > "$BUNDLE/release-ledger/state.tmp"
mv "$BUNDLE/release-ledger/state.tmp" "$BUNDLE/release-ledger/state.json"
(cd "$BUNDLE" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
expect_failure 'migrate rejects high-water below historical release sequences' \
  "$BOOTSTRAP" migrate --bundle "$BUNDLE" --confirm RESTORE-MIGRATION --check-only
jq '.custom_version_high_water = 2' "$BUNDLE/release-ledger/state.json" > "$BUNDLE/release-ledger/state.tmp"
mv "$BUNDLE/release-ledger/state.tmp" "$BUNDLE/release-ledger/state.json"

(cd "$BUNDLE" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
printf '\ncorrupt\n' >> "$BUNDLE/bundle.json"
expect_failure 'migrate rejects checksum drift' \
  "$BOOTSTRAP" migrate --bundle "$BUNDLE" --confirm RESTORE-MIGRATION --check-only

printf '1..%d\n' "$((pass + failures))"
printf '# pass %d\n# fail %d\n' "$pass" "$failures"
[[ "$failures" -eq 0 ]]
