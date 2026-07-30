#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

CURRENT_COMMIT="$(printf '1%.0s' {1..40})"
TARGET_COMMIT="$(printf '2%.0s' {1..40})"
OTHER_COMMIT="$(printf '3%.0s' {1..40})"
MISSING_COMMIT="$(printf '4%.0s' {1..40})"
CURRENT_MAIN="sha256:$(printf 'a%.0s' {1..64})"
CURRENT_EXT="sha256:$(printf 'b%.0s' {1..64})"
TARGET_MAIN="sha256:$(printf 'c%.0s' {1..64})"
TARGET_EXT="sha256:$(printf 'd%.0s' {1..64})"
MISSING_MAIN="sha256:$(printf 'e%.0s' {1..64})"

fail() { printf 'prepare rollback test failed: %s\n' "$*" >&2; exit 1; }
assert_eq() { [[ "$1" == "$2" ]] || fail "$3 (expected=$1 actual=$2)"; }

write_target() {
  local dir="$1" main="$2" ext="$3"
  mkdir -p "$dir/target"
  printf 'services:\n  sub2api: {}\n' > "$dir/target/docker-compose.yml"
  printf 'services:\n  extensions-self: {}\n' > "$dir/target/docker-compose.custom.yml"
  printf 'SUB2API_IMAGE=ghcr.io/listencodes/sub2api-custom@%s\nEXTENSIONS_SELF_IMAGE=ghcr.io/listencodes/sub2api-extensions@%s\n' \
    "$main" "$ext" > "$dir/target/.env"
  jq -n --arg main "ghcr.io/listencodes/sub2api-custom@$main" --arg ext "ghcr.io/listencodes/sub2api-extensions@$ext" '
    {name:"deploy",services:{sub2api:{image:$main,healthcheck:{},volumes:[{target:"/app/data"},{target:"/app/scripts/sync-upstream.sh"},{target:"/repo"},{target:"/usr/bin/docker"},{target:"/var/run/docker.sock"}],networks:{"sub2api-network":{}}},
    "extensions-self":{image:$ext,healthcheck:{},networks:{"sub2api-network":{}}},postgres:{},redis:{},"risk-control-postgres":{}},
    volumes:{sub2api_data:{},postgres_data:{},redis_data:{},risk_control_postgres_data:{}}}
  ' > "$dir/target/rendered-compose.json"
  (cd "$dir/target" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
}

complete_backup_contract() {
  local dir="$1"
  for name in .env docker-compose.yml docker-compose.custom.yml release-state.json container-metadata.json image-metadata.txt rollback-tags.txt \
    sub2api_db.dump sub2api_db.list risk_control_db.dump risk_control_db.list docker-containers.txt docker-images.txt; do
    printf 'fixture %s\n' "$name" > "$dir/$name"
  done
  printf '/etc/nginx/site.conf\n' > "$dir/nginx-vhost.path"
  printf '/etc/nginx/origin.crt\n' > "$dir/origin-cert.path"
  printf '/etc/nginx/origin.key\n' > "$dir/origin-key.path"
  printf 'nginx\n' > "$dir/site.conf"
  printf 'cert\n' > "$dir/origin.crt"
  printf 'key\n' > "$dir/origin.key"
  (cd "$dir" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
}

write_record() {
  local root="$1" id="$2" sequence="$3" commit="$4" main="$5" ext="$6" backup="$7" published="$8"
  jq -n --arg id "$id" --arg commit "$commit" --arg main "$main" --arg ext "$ext" --arg backup "$backup" \
    --arg published "$published" --argjson sequence "$sequence" \
    --arg base_sha "$(sha256sum "$backup/target/docker-compose.yml" | awk '{print $1}')" \
    --arg custom_sha "$(sha256sum "$backup/target/docker-compose.custom.yml" | awk '{print $1}')" \
    --arg rendered_sha "$(sha256sum "$backup/target/rendered-compose.json" | awk '{print $1}')" \
    --arg env_sha "$(sha256sum "$backup/target/.env" | awk '{print $1}')" \
    --arg manifest_sha "$(sha256sum "$backup/SHA256SUMS" 2>/dev/null | awk '{print $1}')" '
    {schema_version:1,release_id:$id,official_version:(if $sequence == 7 then "v0.1.164" else "v0.1.162" end),
    official_commit:$commit,custom_version:("v1.0."+($sequence|tostring)),custom_version_sequence:$sequence,custom_commit:$commit,
    main_digest:$main,extensions_digest:$ext,base_compose_sha256:$base_sha,custom_compose_sha256:$custom_sha,
    rendered_compose_sha256:$rendered_sha,env_sha256:$env_sha,backup_dir:$backup,backup_manifest_sha256:$manifest_sha,
    published_at:$published,source_kind:"custom",operation_id:("update-record-"+$id)}' \
    > "$root/data/release-ledger/releases/$id.json"
}

make_fake_tools() {
  local root="$1"
  mkdir -p "$root/bin"
  cat > "$root/bin/git" <<'EOF'
#!/usr/bin/env bash
printf 'git %s\n' "$*" >> "$FIXTURE_CALLS"
[[ "$FIXTURE_SCENARIO" != missing-git ]] || exit 127
[[ "$*" == *'cat-file -e'* ]] || exit 2
[[ "$FIXTURE_SCENARIO" != missing-source ]] || exit 1
[[ "$FIXTURE_SCENARIO" != crowded-missing-source || "$*" != *"$FIXTURE_MISSING_COMMIT"* ]]
EOF
  cat > "$root/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >> "$FIXTURE_CALLS"
if [[ "${1:-}" == image && "${2:-}" == inspect ]]; then
  ref="${3:-}"
  if [[ "$FIXTURE_SCENARIO" == main-pull-failure && "$ref" == *sub2api-custom* ]]; then exit 1; fi
  if [[ "$FIXTURE_SCENARIO" == extensions-pull-failure && "$ref" == *sub2api-extensions* ]]; then exit 1; fi
  if [[ "$FIXTURE_SCENARIO" == missing-image && "$ref" == *sub2api-extensions* && ! -e "$FIXTURE_ROOT/pulled-ext" ]]; then exit 1; fi
  if [[ "$FIXTURE_SCENARIO" == crowded-missing-image && "$ref" == *"$FIXTURE_MISSING_MAIN"* ]]; then exit 1; fi
  case "$*" in
    *'.Config.Labels'*)
      revision="$FIXTURE_TARGET_COMMIT"
      [[ "$FIXTURE_SCENARIO" != wrong-revision || "$ref" != *sub2api-custom* ]] || revision="$FIXTURE_OTHER_COMMIT"
      jq -n --arg revision "$revision" '{"org.opencontainers.image.revision":$revision,"org.opencontainers.image.version":"0.1.162","org.opencontainers.image.source":"https://github.com/ListenCodes/sub2api"}'
      ;;
    *'.Architecture'*) printf 'amd64\n' ;;
    *'.RepoDigests'*) jq -cn --arg ref "$ref" '[$ref]' ;;
    *'--format {{.Id}}'*) printf 'sha256:imageid\n' ;;
    *) printf '{}\n' ;;
  esac
  exit 0
fi
if [[ "${1:-}" == manifest && "${2:-}" == inspect ]]; then
  [[ "$FIXTURE_SCENARIO" != crowded-missing-image || "${3:-}" != *"$FIXTURE_MISSING_MAIN"* ]]
  exit
fi
if [[ "${1:-}" == pull ]]; then
  [[ "$FIXTURE_SCENARIO" != pull-failure ]] || exit 1
  [[ "$FIXTURE_SCENARIO" != main-pull-failure || "${2:-}" != *sub2api-custom* ]] || exit 1
  [[ "$FIXTURE_SCENARIO" != extensions-pull-failure || "${2:-}" != *sub2api-extensions* ]] || exit 1
  [[ "${2:-}" != *sub2api-extensions* ]] || touch "$FIXTURE_ROOT/pulled-ext"
  exit 0
fi
if [[ "${1:-}" == compose ]]; then
  [[ ! " $* " =~ [[:space:]](up|down|rm|restart|stop|kill)[[:space:]] ]] || exit 90
  if [[ "$*" == *'config --format json'* ]]; then
    args=("$@")
    env_file=''
    for ((index=0; index<${#args[@]}; index++)); do
      [[ "${args[$index]}" != --env-file ]] || env_file="${args[$((index+1))]}"
    done
    main="$(sed -n 's/^SUB2API_IMAGE=//p' "$env_file")"
    ext="$(sed -n 's/^EXTENSIONS_SELF_IMAGE=//p' "$env_file")"
    jq -n --arg main "$main" --arg ext "$ext" '
      {name:"deploy",services:{sub2api:{image:$main,healthcheck:{},volumes:[{target:"/app/data"},{target:"/app/scripts/sync-upstream.sh"},{target:"/repo"},{target:"/usr/bin/docker"},{target:"/var/run/docker.sock"}],networks:{"sub2api-network":{}}},
      "extensions-self":{image:$ext,healthcheck:{},networks:{"sub2api-network":{}}},postgres:{},redis:{},"risk-control-postgres":{}},
      volumes:{sub2api_data:{},postgres_data:{},redis_data:{},risk_control_postgres_data:{}}}'
  fi
  exit 0
fi
if [[ "${1:-}" == inspect ]]; then printf '[]\n'; exit 0; fi
if [[ "${1:-}" == image && "${2:-}" == ls ]]; then printf 'images\n'; exit 0; fi
if [[ "${1:-}" == image && "${2:-}" == tag ]]; then [[ "$FIXTURE_SCENARIO" != backup-failure ]]; exit; fi
if [[ "${1:-}" == exec ]]; then
  if [[ "$*" == *'pg_restore --list'* ]]; then [[ "$FIXTURE_SCENARIO" != dump-validation-failure ]] || exit 1; printf 'dump list\n';
  else printf 'dump\n'; fi
  exit 0
fi
if [[ "${1:-}" == ps || "${1:-}" == images ]]; then printf 'metadata\n'; exit 0; fi
exit 2
EOF
  cat > "$root/bin/flock" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  cat > "$root/bin/sync" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$root/bin/git" "$root/bin/docker" "$root/bin/flock" "$root/bin/sync"
}

seed_case() {
  local scenario="$1" root="$TMP_DIR/$scenario" job_id="rollback-prepare-$scenario"
  local current_backup="$root/data/release-backups/current" old1="$root/data/release-backups/old1" old2="$root/data/release-backups/old2" old3="$root/data/release-backups/old3" old4="$root/data/release-backups/old4"
  mkdir -p "$root/data/release-ledger/releases" "$root/data/release-ledger/operations" "$root/repo/deploy" "$root/runtime" "$old4/target"
  local old1_commit="$TARGET_COMMIT" old1_main="$TARGET_MAIN"
  [[ "$scenario" != crowded-missing-source ]] || old1_commit="$MISSING_COMMIT"
  [[ "$scenario" != crowded-missing-image ]] || old1_main="$MISSING_MAIN"
  write_target "$current_backup" "$CURRENT_MAIN" "$CURRENT_EXT"
  complete_backup_contract "$current_backup"
  write_target "$old1" "$old1_main" "$TARGET_EXT"; complete_backup_contract "$old1"
  write_target "$old2" "$TARGET_MAIN" "$TARGET_EXT"; complete_backup_contract "$old2"
  write_target "$old3" "$TARGET_MAIN" "$TARGET_EXT"; complete_backup_contract "$old3"
  write_target "$old4" "$TARGET_MAIN" "$TARGET_EXT"; complete_backup_contract "$old4"
  write_record "$root" release-current 7 "$CURRENT_COMMIT" "$CURRENT_MAIN" "$CURRENT_EXT" "$current_backup" 2026-07-23T08:30:00Z
  write_record "$root" release-old1 6 "$old1_commit" "$old1_main" "$TARGET_EXT" "$old1" 2026-07-23T08:20:00Z
  write_record "$root" release-old2 5 "$TARGET_COMMIT" "$TARGET_MAIN" "$TARGET_EXT" "$old2" 2026-07-23T08:10:00Z
  write_record "$root" release-old3 4 "$TARGET_COMMIT" "$TARGET_MAIN" "$TARGET_EXT" "$old3" 2026-07-23T08:00:00Z
  write_record "$root" release-old4 3 "$OTHER_COMMIT" "$TARGET_MAIN" "$TARGET_EXT" "$old4" 2026-07-23T07:50:00Z
  jq -n --arg job "$job_id" '{schema_version:1,current_release_id:"release-current",custom_version_high_water:7,active_operation_id:$job,updated_at:"2026-07-23T08:31:00Z"}' > "$root/data/release-ledger/state.json"
  jq --argjson record "$(cat "$root/data/release-ledger/releases/release-current.json")" '
    {production_commit:$record.custom_commit,stable_release_tag:$record.official_version,stable_release_commit:$record.official_commit,
    main_digest:$record.main_digest,extensions_digest:$record.extensions_digest,published_at:$record.published_at,backup_dir:$record.backup_dir,
    release_id:$record.release_id,official_version:$record.official_version,custom_version:$record.custom_version,custom_version_sequence:$record.custom_version_sequence}' \
    <<< '{}' > "$root/data/release-state.json"
  cp -p "$current_backup/target/docker-compose.yml" "$root/repo/deploy/docker-compose.yml"
  cp -p "$current_backup/target/docker-compose.custom.yml" "$root/repo/deploy/docker-compose.custom.yml"
  cp -p "$current_backup/target/.env" "$root/repo/deploy/.env"
  printf 'nginx\n' > "$root/nginx.conf"; printf 'cert\n' > "$root/origin.crt"; printf 'key\n' > "$root/origin.key"
  : > "$root/calls"
  target=release-old2
  case "$scenario" in
    current) target=release-current ;;
    invalid) target=release-missing ;;
    ineligible|crowded-missing-source|crowded-missing-image) target=release-old4 ;;
  esac
  jq -n --arg job "$job_id" --arg target "$target" '
    {job_id:$job,operation_kind:"rollback",action:"prepare",status:"resolving_snapshot",message:"queued",target_release_id:$target,
    base_release_id:"release-current",published:false,production_changed:false,rollback:{attempted:false,succeeded:false,message:""}}' \
    > "$root/data/release-ledger/operations/$job_id.json"
  case "$scenario" in
    corrupt-target) printf 'drift\n' >> "$old2/target/.env" ;;
    ledger-drift) jq '.current_release_id="release-old1"' "$root/data/release-ledger/state.json" > "$root/state.tmp" && mv "$root/state.tmp" "$root/data/release-ledger/state.json" ;;
  esac
  make_fake_tools "$root"
  printf '%s\n' "$root"
}

invoke_prepare() {
  local root="$1" scenario="$2" job_id="rollback-prepare-$scenario" target_commit="$TARGET_COMMIT"
  [[ "$scenario" != crowded-missing-source && "$scenario" != crowded-missing-image ]] || target_commit="$OTHER_COMMIT"
  PATH="$root/bin:$PATH" FIXTURE_ROOT="$root" FIXTURE_CALLS="$root/calls" FIXTURE_SCENARIO="$scenario" \
    FIXTURE_MISSING_COMMIT="$MISSING_COMMIT" FIXTURE_MISSING_MAIN="$MISSING_MAIN" \
    FIXTURE_OTHER_COMMIT="$OTHER_COMMIT" \
    FIXTURE_TARGET_COMMIT="$target_commit" FIXTURE_TARGET_RENDERED="$root/data/release-backups/old2/target/rendered-compose.json" \
    SUB2API_DATA_DIR="$root/data" SUB2API_REPO="$root/repo" SUB2API_ENV_FILE="$root/repo/deploy/.env" \
    SUB2API_COMPOSE_BASE="$root/repo/deploy/docker-compose.yml" SUB2API_COMPOSE_CUSTOM="$root/repo/deploy/docker-compose.custom.yml" \
    SUB2API_RELEASE_LEDGER_ROOT="$root/data/release-ledger" SUB2API_RELEASE_OPERATIONS_DIR="$root/data/release-ledger/operations" \
    SUB2API_RELEASE_BACKUP_ROOT="$root/data/release-backups" SUB2API_BACKUP_ROOT="$root/data/release-backups" \
    SUB2API_PREPARED_ROOT="$root/data/release-prepared" SUB2API_RELEASE_STATE_FILE="$root/data/release-state.json" \
    SUB2API_RELEASE_LEDGER_LOCK_FILE="$root/data/release.lock" SUB2API_NGINX_VHOST="$root/nginx.conf" \
    SUB2API_ORIGIN_CERT="$root/origin.crt" SUB2API_ORIGIN_KEY="$root/origin.key" SUB2API_SYNC_PUBLISH_LOG="$root/release.log" \
    "$ROOT_DIR/deploy/ops/prepare-rollback.sh" --job-id "$job_id"
}

run_success() {
  local scenario="$1" root job_id manifest expected_target=release-old2 expected_custom=v1.0.5
  if [[ "$scenario" == crowded-missing-source || "$scenario" == crowded-missing-image ]]; then
    expected_target=release-old4
    expected_custom=v1.0.3
  fi
  root="$(seed_case "$scenario")"; job_id="rollback-prepare-$scenario"
  invoke_prepare "$root" "$scenario" || fail "$scenario prepare failed"
  manifest="$root/data/release-prepared/$job_id/manifest.json"
  [[ -s "$manifest" ]] || fail "$scenario manifest missing"
  assert_eq rollback "$(jq -r '.operation_kind' "$manifest")" "$scenario operation kind"
  assert_eq release-current "$(jq -r '.base_release_id' "$manifest")" "$scenario base release"
  assert_eq "$expected_target" "$(jq -r '.target_release_id' "$manifest")" "$scenario target release"
  assert_eq 7 "$(jq -r '.base_custom_high_water' "$manifest")" "$scenario high-water"
  assert_eq v0.1.162 "$(jq -r '.target_official_version' "$manifest")" "$scenario official version"
  assert_eq "$expected_custom" "$(jq -r '.target_custom_version' "$manifest")" "$scenario custom version"
  assert_eq 3600 "$(( $(date -u -d "$(jq -r '.expires_at' "$manifest")" +%s) - $(date -u -d "$(jq -r '.prepared_at' "$manifest")" +%s) ))" "$scenario expiry"
  assert_eq prepared "$(jq -r '.status' "$root/data/release-ledger/operations/$job_id.json")" "$scenario status"
  assert_eq "$job_id" "$(jq -r '.active_operation_id' "$root/data/release-ledger/state.json")" "$scenario active operation"
  [[ -z "$(grep -E 'docker compose .* (up|down|rm|restart|stop|kill)|api\.github\.com|wait-for-actions' "$root/calls" || true)" ]] || fail "$scenario used forbidden prepare action"
  if [[ "$scenario" == missing-image ]]; then
    assert_eq 1 "$(grep -c '^docker pull .*sub2api-extensions' "$root/calls")" 'missing extension digest was not pulled exactly once'
    [[ -z "$(grep '^docker pull .*sub2api-custom' "$root/calls" || true)" ]] || fail 'present main digest was pulled'
  else
    [[ -z "$(grep '^docker pull ' "$root/calls" || true)" ]] || fail 'present images were pulled'
  fi
}

run_refusal() {
  local scenario="$1" root job_id state_before projection_before backup_count_before code
  root="$(seed_case "$scenario")"; job_id="rollback-prepare-$scenario"
  state_before="$(jq -c '{current_release_id,custom_version_high_water}' "$root/data/release-ledger/state.json")"
  projection_before="$(sha256sum "$root/data/release-state.json")"
  backup_count_before="$(find "$root/data/release-backups" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')"
  set +e; invoke_prepare "$root" "$scenario" >/dev/null 2>&1; code=$?; set -e
  [[ "$code" -ne 0 ]] || fail "$scenario unexpectedly succeeded"
  assert_eq "$state_before" "$(jq -c '{current_release_id,custom_version_high_water}' "$root/data/release-ledger/state.json")" "$scenario changed ledger pointer/high-water"
  assert_eq null "$(jq -r '.active_operation_id' "$root/data/release-ledger/state.json")" "$scenario did not clear active operation"
  assert_eq failed "$(jq -r '.status' "$root/data/release-ledger/operations/$job_id.json")" "$scenario did not settle failed"
  assert_eq "$projection_before" "$(sha256sum "$root/data/release-state.json")" "$scenario changed projection"
  [[ -z "$(grep -E 'docker compose .* (up|down|rm|restart|stop|kill)' "$root/calls" || true)" ]] || fail "$scenario changed container lifecycle"
  [[ ! -e "$root/data/release-prepared/$job_id/manifest.json" ]] || fail "$scenario created a prepared manifest"
  if [[ "$scenario" == wrong-revision ]]; then
    assert_eq "$backup_count_before" "$(find "$root/data/release-backups" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')" +      "$scenario created a fresh backup before rejecting the OCI revision"
  fi
}

if [[ -n "${FIXTURE_ONLY:-}" ]]; then
  case "$FIXTURE_ONLY" in success|missing-image|crowded-missing-source|crowded-missing-image) run_success "$FIXTURE_ONLY" ;; *) run_refusal "$FIXTURE_ONLY" ;; esac
  printf 'prepare-rollback=%s=PASS\n' "$FIXTURE_ONLY"
  exit 0
fi

scenarios=(success missing-image crowded-missing-source crowded-missing-image invalid current ineligible corrupt-target ledger-drift backup-failure dump-validation-failure missing-source missing-git main-pull-failure extensions-pull-failure wrong-revision)
for ((offset=0; offset<${#scenarios[@]}; offset+=5)); do
  pids=(); batch=()
  for ((index=offset; index<offset+5 && index<${#scenarios[@]}; index++)); do
    scenario="${scenarios[$index]}"
    FIXTURE_ONLY="$scenario" bash "$0" > "$TMP_DIR/$scenario.log" 2>&1 &
    pids+=("$!"); batch+=("$scenario")
  done
  for index in "${!pids[@]}"; do
    if ! wait "${pids[$index]}"; then
      cat "$TMP_DIR/${batch[$index]}.log" >&2
      fail "${batch[$index]} parallel fixture failed"
    fi
  done
done
printf 'prepare-rollback=PASS\n'
