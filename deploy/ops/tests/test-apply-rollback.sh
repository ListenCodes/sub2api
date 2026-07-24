#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

BASE_COMMIT="$(printf '1%.0s' {1..40})"
TARGET_COMMIT="$(printf '2%.0s' {1..40})"
BASE_MAIN="sha256:$(printf 'a%.0s' {1..64})"
BASE_EXT="sha256:$(printf 'b%.0s' {1..64})"
TARGET_MAIN="sha256:$(printf 'c%.0s' {1..64})"
TARGET_EXT="sha256:$(printf 'd%.0s' {1..64})"

fail() { printf 'apply rollback test failed: %s\n' "$*" >&2; exit 1; }
assert_eq() { [[ "$1" == "$2" ]] || fail "$3 (expected=$1 actual=$2)"; }

write_target() {
  local dir="$1" main="$2" ext="$3"
  mkdir -p "$dir/target"
  printf 'services:\n  sub2api: {}\n' > "$dir/target/docker-compose.yml"
  printf 'services:\n  extensions-self: {}\n' > "$dir/target/docker-compose.custom.yml"
  printf 'SUB2API_IMAGE=ghcr.io/listencodes/sub2api-custom@%s\nEXTENSIONS_SELF_IMAGE=ghcr.io/listencodes/sub2api-extensions@%s\n' "$main" "$ext" > "$dir/target/.env"
  render_json "$dir/target/rendered-compose.json" "ghcr.io/listencodes/sub2api-custom@$main" "ghcr.io/listencodes/sub2api-extensions@$ext"
  (cd "$dir/target" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
}

render_json() {
  local path="$1" main="$2" ext="$3"
  jq -n --arg main "$main" --arg ext "$ext" '
    {name:"deploy",services:{sub2api:{image:$main,healthcheck:{},volumes:[{target:"/app/data"},{target:"/repo"},{target:"/var/run/docker.sock"}],networks:{"sub2api-network":{}}},
    "extensions-self":{image:$ext,healthcheck:{},networks:{"sub2api-network":{}}},postgres:{},redis:{},"risk-control-postgres":{}},
    volumes:{sub2api_data:{},postgres_data:{},redis_data:{},risk_control_postgres_data:{}}}' > "$path"
}

complete_backup() {
  local dir="$1"
  for name in .env docker-compose.yml docker-compose.custom.yml release-state.json container-metadata.json image-metadata.txt rollback-tags.txt \
    sub2api_db.dump sub2api_db.list risk_control_db.dump risk_control_db.list docker-containers.txt docker-images.txt; do printf '%s\n' "$name" > "$dir/$name"; done
  printf '/etc/nginx/site.conf\n' > "$dir/nginx-vhost.path"; printf '/etc/nginx/origin.crt\n' > "$dir/origin-cert.path"; printf '/etc/nginx/origin.key\n' > "$dir/origin-key.path"
  printf 'nginx\n' > "$dir/site.conf"; printf 'cert\n' > "$dir/origin.crt"; printf 'key\n' > "$dir/origin.key"
  (cd "$dir" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
}

write_record() {
  local root="$1" id="$2" seq="$3" commit="$4" main="$5" ext="$6" backup="$7" official="$8" published="$9"
  jq -n --arg id "$id" --arg commit "$commit" --arg main "$main" --arg ext "$ext" --arg backup "$backup" --arg official "$official" --arg published "$published" --argjson seq "$seq" \
    --arg base "$(sha256sum "$backup/target/docker-compose.yml" | awk '{print $1}')" --arg custom "$(sha256sum "$backup/target/docker-compose.custom.yml" | awk '{print $1}')" \
    --arg rendered "$(sha256sum "$backup/target/rendered-compose.json" | awk '{print $1}')" --arg env "$(sha256sum "$backup/target/.env" | awk '{print $1}')" \
    --arg backup_sha "$(sha256sum "$backup/SHA256SUMS" | awk '{print $1}')" '
    {schema_version:1,release_id:$id,official_version:$official,official_commit:$commit,custom_version:("v1.0."+($seq|tostring)),custom_version_sequence:$seq,
    custom_commit:$commit,main_digest:$main,extensions_digest:$ext,base_compose_sha256:$base,custom_compose_sha256:$custom,rendered_compose_sha256:$rendered,
    env_sha256:$env,backup_dir:$backup,backup_manifest_sha256:$backup_sha,published_at:$published,source_kind:"custom",operation_id:("update-record-"+$id)}' \
    > "$root/data/release-ledger/releases/$id.json"
}

make_tools() {
  local root="$1"
  mkdir -p "$root/bin"
  cat > "$root/bin/flock" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  cat > "$root/bin/sync" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  cat > "$root/bin/git" <<'EOF'
#!/usr/bin/env bash
printf 'git %s\n' "$*" >> "$FIXTURE_CALLS"
case "$*" in
  *'rev-parse HEAD'*) cat "$FIXTURE_ROOT/repo-state/head" ;;
  *'symbolic-ref --quiet --short HEAD'*) cat "$FIXTURE_ROOT/repo-state/ref"; [[ -s "$FIXTURE_ROOT/repo-state/ref" ]] ;;
  *'status --porcelain --untracked-files=all'*) [[ ! -e "$FIXTURE_ROOT/repo-state/dirty" ]] ;;
  *'cat-file -e'*) exit 0 ;;
  *'switch --detach'*) printf '%s\n' "${*: -1}" > "$FIXTURE_ROOT/repo-state/head"; : > "$FIXTURE_ROOT/repo-state/ref" ;;
  *'branch -f'*) exit 0 ;;
  *'switch custom-release'*) printf '%s\n' "$FIXTURE_BASE_COMMIT" > "$FIXTURE_ROOT/repo-state/head"; printf 'custom-release\n' > "$FIXTURE_ROOT/repo-state/ref" ;;
  *) exit 2 ;;
esac
EOF
  cat > "$root/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >> "$FIXTURE_CALLS"
if [[ "${1:-}" == image && "${2:-}" == inspect ]]; then
  ref="${3:-}"
  [[ "$FIXTURE_SCENARIO" != missing-image || "$ref" != *"@$FIXTURE_TARGET_MAIN"* ]] || exit 1
  if [[ "$ref" == *"@$FIXTURE_TARGET_MAIN"* || "$ref" == *"@$FIXTURE_TARGET_EXT"* ]]; then revision="$FIXTURE_TARGET_COMMIT"; version=0.1.162; identity=target; else revision="$FIXTURE_BASE_COMMIT"; version=0.1.164; identity=base; fi
  case "$*" in
    *'.Config.Labels'*) jq -n --arg revision "$revision" --arg version "$version" '{"org.opencontainers.image.revision":$revision,"org.opencontainers.image.version":$version,"org.opencontainers.image.source":"https://github.com/ListenCodes/sub2api"}' ;;
    *'.Architecture'*) printf 'amd64\n' ;;
    *'.RepoDigests'*) jq -cn --arg ref "$ref" '[$ref]' ;;
    *'--format {{.Id}}'*) printf 'sha256:%sid\n' "$identity" ;;
    *) printf '{}\n' ;;
  esac
  exit 0
fi
if [[ "${1:-}" == inspect ]]; then
  if [[ "$*" == *'.Config.Image'* ]]; then
    container="${*: -1}"; identity="$(cat "$FIXTURE_ROOT/runtime/$container")"
    if [[ "$container" == sub2api ]]; then [[ "$identity" == target ]] && printf 'ghcr.io/listencodes/sub2api-custom@%s\n' "$FIXTURE_TARGET_MAIN" || printf 'ghcr.io/listencodes/sub2api-custom@%s\n' "$FIXTURE_BASE_MAIN";
    else [[ "$identity" == target ]] && printf 'ghcr.io/listencodes/sub2api-extensions@%s\n' "$FIXTURE_TARGET_EXT" || printf 'ghcr.io/listencodes/sub2api-extensions@%s\n' "$FIXTURE_BASE_EXT"; fi
  elif [[ "$*" == *'--format {{.Image}}'* ]]; then printf 'sha256:%sid\n' "$(cat "$FIXTURE_ROOT/runtime/${*: -1}")";
  elif [[ "$*" == *'.State.Health'* ]]; then
    container="${*: -1}"
    if [[ "$FIXTURE_SCENARIO" == health-failure && "$container" == sub2api && "$(cat "$FIXTURE_ROOT/runtime/sub2api")" == target && ! -e "$FIXTURE_ROOT/health-failed" ]]; then touch "$FIXTURE_ROOT/health-failed"; printf 'unhealthy\n'; else printf 'healthy\n'; fi
  else printf '[]\n'; fi
  exit 0
fi
if [[ "${1:-}" == compose ]]; then
  [[ ! " $* " =~ [[:space:]](down|rm|restart|stop|kill)[[:space:]] ]] || exit 90
  if [[ "$*" == *'config --format json'* ]]; then
    args=("$@"); env_file=''; for ((i=0;i<${#args[@]};i++)); do [[ "${args[$i]}" != --env-file ]] || env_file="${args[$((i+1))]}"; done
    main="$(sed -n 's/^SUB2API_IMAGE=//p' "$env_file")"; ext="$(sed -n 's/^EXTENSIONS_SELF_IMAGE=//p' "$env_file")"
    jq -n --arg main "$main" --arg ext "$ext" '{name:"deploy",services:{sub2api:{image:$main,healthcheck:{},volumes:[{target:"/app/data"},{target:"/repo"},{target:"/var/run/docker.sock"}],networks:{"sub2api-network":{}}},"extensions-self":{image:$ext,healthcheck:{},networks:{"sub2api-network":{}}},postgres:{},redis:{},"risk-control-postgres":{}},volumes:{sub2api_data:{},postgres_data:{},redis_data:{},risk_control_postgres_data:{}}}'
  elif [[ " $* " == *' up '* ]]; then
    service="${*: -1}"; args=("$@"); env_file=''; for ((i=0;i<${#args[@]};i++)); do [[ "${args[$i]}" != --env-file ]] || env_file="${args[$((i+1))]}"; done
    identity=base; grep -q "@$FIXTURE_TARGET_MAIN\|@$FIXTURE_TARGET_EXT" "$env_file" && identity=target
    marker="$FIXTURE_ROOT/${FIXTURE_SCENARIO}-${service}-failed"
    if { [[ "$FIXTURE_SCENARIO" == extension-failure && "$service" == extensions-self ]] || [[ "$FIXTURE_SCENARIO" == main-failure && "$service" == sub2api ]]; } && [[ "$identity" == target && ! -e "$marker" ]]; then touch "$marker"; exit 1; fi
    printf '%s\n' "$identity" > "$FIXTURE_ROOT/runtime/$service"
  fi
  exit 0
fi
if [[ "${1:-}" == exec ]]; then printf '{}\n'; exit 0; fi
exit 2
EOF
  chmod +x "$root/bin/"*
}

seed_case() {
  local scenario="$1" root="$TMP_DIR/$scenario" job="rollback-apply-$scenario" base_backup target_backup prepared
  base_backup="$root/data/release-backups/base"; target_backup="$root/data/release-backups/target"; prepared="$root/data/release-backups/prepared"
  mkdir -p "$root/data/release-ledger/releases" "$root/data/release-ledger/operations" "$root/data/release-prepared/$job" "$root/repo/deploy" "$root/repo-state" "$root/runtime"
  write_target "$base_backup" "$BASE_MAIN" "$BASE_EXT"; complete_backup "$base_backup"
  write_target "$target_backup" "$TARGET_MAIN" "$TARGET_EXT"; complete_backup "$target_backup"
  write_record "$root" release-base 7 "$BASE_COMMIT" "$BASE_MAIN" "$BASE_EXT" "$base_backup" v0.1.164 2026-07-23T08:20:00Z
  write_record "$root" release-target 5 "$TARGET_COMMIT" "$TARGET_MAIN" "$TARGET_EXT" "$target_backup" v0.1.162 2026-07-23T08:10:00Z
  mkdir -p "$prepared/target"
  cp -p "$target_backup/target/docker-compose.yml" "$target_backup/target/docker-compose.custom.yml" "$target_backup/target/.env" "$target_backup/target/rendered-compose.json" "$prepared/target/"
  cp -p "$base_backup/target/.env" "$prepared/.env"; cp -p "$base_backup/target/docker-compose.yml" "$prepared/docker-compose.yml"; cp -p "$base_backup/target/docker-compose.custom.yml" "$prepared/docker-compose.custom.yml"
  cp -p "$root/data/release-state.json" "$prepared/release-state.json" 2>/dev/null || printf '{}\n' > "$prepared/release-state.json"
  for name in container-metadata.json image-metadata.txt rollback-tags.txt sub2api_db.dump sub2api_db.list risk_control_db.dump risk_control_db.list docker-containers.txt docker-images.txt; do printf '%s\n' "$name" > "$prepared/$name"; done
  printf '/etc/nginx/site.conf\n' > "$prepared/nginx-vhost.path"; printf '/etc/nginx/origin.crt\n' > "$prepared/origin-cert.path"; printf '/etc/nginx/origin.key\n' > "$prepared/origin-key.path"
  printf 'nginx\n' > "$prepared/site.conf"; printf 'cert\n' > "$prepared/origin.crt"; printf 'key\n' > "$prepared/origin.key"
  (cd "$prepared/target" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
  (cd "$prepared" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
  jq -n --arg job "$job" '{schema_version:1,current_release_id:"release-base",custom_version_high_water:7,active_operation_id:$job,updated_at:"2026-07-23T08:30:00Z"}' > "$root/data/release-ledger/state.json"
  jq --argjson r "$(cat "$root/data/release-ledger/releases/release-base.json")" '{production_commit:$r.custom_commit,stable_release_tag:$r.official_version,stable_release_commit:$r.official_commit,main_digest:$r.main_digest,extensions_digest:$r.extensions_digest,published_at:$r.published_at,backup_dir:$r.backup_dir,release_id:$r.release_id,official_version:$r.official_version,custom_version:$r.custom_version,custom_version_sequence:$r.custom_version_sequence}' <<< '{}' > "$root/data/release-state.json"
  cp -p "$base_backup/target/docker-compose.yml" "$root/repo/deploy/docker-compose.yml"; cp -p "$base_backup/target/docker-compose.custom.yml" "$root/repo/deploy/docker-compose.custom.yml"; cp -p "$base_backup/target/.env" "$root/repo/deploy/.env"
  printf '%s\n' "$BASE_COMMIT" > "$root/repo-state/head"; printf 'custom-release\n' > "$root/repo-state/ref"; printf 'base\n' > "$root/runtime/sub2api"; printf 'base\n' > "$root/runtime/extensions-self"; : > "$root/calls"
  prepared_at=2026-07-23T08:30:00Z; expires_at=2099-07-23T08:45:00Z
  [[ "$scenario" != expired ]] || expires_at=2020-01-01T00:00:00Z
  jq -n --arg base release-base --arg target release-target --argjson high 7 --arg source "$BASE_COMMIT" --arg target_commit "$TARGET_COMMIT" --arg main "$TARGET_MAIN" --arg ext "$TARGET_EXT" --arg current_main "$BASE_MAIN" --arg current_ext "$BASE_EXT" \
    --arg cb "$(sha256sum "$prepared/docker-compose.yml"|awk '{print $1}')" --arg cc "$(sha256sum "$prepared/docker-compose.custom.yml"|awk '{print $1}')" --arg tb "$(sha256sum "$prepared/target/docker-compose.yml"|awk '{print $1}')" --arg tc "$(sha256sum "$prepared/target/docker-compose.custom.yml"|awk '{print $1}')" --arg tr "$(sha256sum "$prepared/target/rendered-compose.json"|awk '{print $1}')" --arg te "$(sha256sum "$prepared/target/.env"|awk '{print $1}')" --arg tm "$(sha256sum "$prepared/target/SHA256SUMS"|awk '{print $1}')" --arg backup "$prepared" --arg bm "$(sha256sum "$prepared/SHA256SUMS"|awk '{print $1}')" --arg pa "$prepared_at" --arg ex "$expires_at" '
    {schema_version:1,operation_kind:"rollback",base_release_id:$base,target_release_id:$target,base_custom_high_water:$high,source_commit:$source,target_commit:$target_commit,target_official_version:"v0.1.162",target_custom_version:"v1.0.5",main_digest:$main,extensions_digest:$ext,current_main_digest:$current_main,current_extensions_digest:$current_ext,current_base_compose_sha256:$cb,current_custom_compose_sha256:$cc,target_base_compose_sha256:$tb,target_custom_compose_sha256:$tc,target_rendered_compose_sha256:$tr,target_env_sha256:$te,target_artifact_manifest_sha256:$tm,backup_dir:$backup,backup_manifest_sha256:$bm,prepared_at:$pa,expires_at:$ex,images_verified:true,compose_contract:"deploy-explicit-pair-v1",backup_contract:"complete-paired-snapshot-v1"}' > "$root/data/release-prepared/$job/manifest.json"
  sha256sum "$root/data/release-prepared/$job/manifest.json" > "$root/data/release-prepared/$job/manifest.sha256"
  jq -n --arg job "$job" --arg manifest_path "$root/data/release-prepared/$job/manifest.json" --argjson manifest "$(cat "$root/data/release-prepared/$job/manifest.json")" '
    $manifest + {job_id:$job,operation_kind:"rollback",action:"apply",status:"apply_queued",message:"queued",
    prepared_manifest:$manifest_path,advances_custom_version:false,published:false,production_changed:false,
    rollback:{attempted:false,succeeded:false,message:""}}' > "$root/data/release-ledger/operations/$job.json"
  case "$scenario" in pointer-drift) jq '.current_release_id="release-target"' "$root/data/release-ledger/state.json" > "$root/s" && mv "$root/s" "$root/data/release-ledger/state.json" ;; high-water-drift) jq '.custom_version_high_water=8' "$root/data/release-ledger/state.json" > "$root/s" && mv "$root/s" "$root/data/release-ledger/state.json" ;; target-record-drift) jq '.custom_version_sequence=4|.custom_version="v1.0.4"' "$root/data/release-ledger/releases/release-target.json" > "$root/r" && mv "$root/r" "$root/data/release-ledger/releases/release-target.json" ;; artifact-drift) printf 'drift\n' >> "$prepared/target/.env" ;; backup-drift) printf 'drift\n' > "$prepared/undeclared" ;; dirty-source) touch "$root/repo-state/dirty" ;; esac
  make_tools "$root"
  printf '%s\n' "$root"
}

invoke() {
  local root="$1" scenario="$2" job="rollback-apply-$scenario" failpoint=''
  local -a command=("$ROOT_DIR/deploy/ops/apply-rollback.sh")
  [[ "${DEBUG_APPLY:-0}" != 1 ]] || command=(bash -x "$ROOT_DIR/deploy/ops/apply-rollback.sh")
  [[ "$scenario" != metadata-write-failure ]] || failpoint=before_state
  PATH="$root/bin:$PATH" FIXTURE_ROOT="$root" FIXTURE_CALLS="$root/calls" FIXTURE_SCENARIO="$scenario" FIXTURE_BASE_COMMIT="$BASE_COMMIT" FIXTURE_TARGET_COMMIT="$TARGET_COMMIT" FIXTURE_BASE_MAIN="$BASE_MAIN" FIXTURE_BASE_EXT="$BASE_EXT" FIXTURE_TARGET_MAIN="$TARGET_MAIN" FIXTURE_TARGET_EXT="$TARGET_EXT" \
    SUB2API_DATA_DIR="$root/data" SUB2API_REPO="$root/repo" SUB2API_ENV_FILE="$root/repo/deploy/.env" SUB2API_COMPOSE_BASE="$root/repo/deploy/docker-compose.yml" SUB2API_COMPOSE_CUSTOM="$root/repo/deploy/docker-compose.custom.yml" SUB2API_RELEASE_LEDGER_ROOT="$root/data/release-ledger" SUB2API_RELEASE_OPERATIONS_DIR="$root/data/release-ledger/operations" SUB2API_RELEASE_BACKUP_ROOT="$root/data/release-backups" SUB2API_PREPARED_ROOT="$root/data/release-prepared" SUB2API_RELEASE_STATE_FILE="$root/data/release-state.json" SUB2API_RELEASE_LEDGER_LOCK_FILE="$root/data/release.lock" SUB2API_LEDGER_COMMIT_FAILPOINT="$failpoint" SUB2API_SYNC_PUBLISH_LOG="$root/release.log" SUB2API_SKIP_EXTERNAL_HEALTH_CHECKS=1 SUB2API_HEALTH_WAIT_TIMEOUT_SECONDS=2 SUB2API_HEALTH_WAIT_INTERVAL_SECONDS=0 \
    "${command[@]}" --job-id "$job"
}

assert_local_only() { local root="$1"; [[ -z "$(grep -E 'docker pull|pg_dump|pg_restore|api\.github\.com|wait-for-actions|verify-release-images|git .* (fetch|merge|reset)' "$root/calls" || true)" ]] || fail "${root##*/} violated local-only apply"; }

run_success() {
  local scenario="$1" root job
  root="$(seed_case "$scenario")"; job="rollback-apply-$scenario"
  invoke "$root" "$scenario" || fail "$scenario failed"
  assert_local_only "$root"
  assert_eq release-target "$(jq -r '.current_release_id' "$root/data/release-ledger/state.json")" "$scenario current release"
  assert_eq 7 "$(jq -r '.custom_version_high_water' "$root/data/release-ledger/state.json")" "$scenario high-water changed"
  assert_eq v0.1.162 "$(jq -r '.official_version' "$root/data/release-state.json")" "$scenario official display"
  assert_eq v1.0.5 "$(jq -r '.custom_version' "$root/data/release-state.json")" "$scenario custom display"
  assert_eq "$TARGET_COMMIT" "$(cat "$root/repo-state/head")" "$scenario source"
  assert_eq target "$(cat "$root/runtime/extensions-self")" "$scenario extensions runtime"
  assert_eq target "$(cat "$root/runtime/sub2api")" "$scenario main runtime"
  assert_eq success "$(jq -r '.status' "$root/data/release-ledger/operations/$job.json")" "$scenario operation status"
  source "$ROOT_DIR/deploy/ops/release-ledger.sh"
  PATH="$root/bin:$PATH" FIXTURE_ROOT="$root" FIXTURE_CALLS="$root/calls" FIXTURE_SCENARIO="$scenario" \
    FIXTURE_BASE_COMMIT="$BASE_COMMIT" FIXTURE_TARGET_COMMIT="$TARGET_COMMIT" FIXTURE_BASE_MAIN="$BASE_MAIN" FIXTURE_BASE_EXT="$BASE_EXT" \
    FIXTURE_TARGET_MAIN="$TARGET_MAIN" FIXTURE_TARGET_EXT="$TARGET_EXT" RELEASE_REPO="$root/repo" \
    SUB2API_DATA_DIR="$root/data" RELEASE_LEDGER_ROOT="$root/data/release-ledger" RELEASE_BACKUP_ROOT="$root/data/release-backups" PRODUCTION_RELEASE_STATE_FILE="$root/data/release-state.json" \
    ledger_list_rollback_release_ids 3 | grep -qx release-base || fail "$scenario former current release not eligible"
}

run_pre_refusal() {
  local scenario="$1" root code before
  root="$(seed_case "$scenario")"; before="$(jq -c '{current_release_id,custom_version_high_water}' "$root/data/release-ledger/state.json")"
  set +e; invoke "$root" "$scenario" >/dev/null 2>&1; code=$?; set -e
  [[ "$code" -ne 0 ]] || fail "$scenario unexpectedly succeeded"
  assert_local_only "$root"; assert_eq "$before" "$(jq -c '{current_release_id,custom_version_high_water}' "$root/data/release-ledger/state.json")" "$scenario changed pointer/high-water"
  [[ -z "$(grep 'docker compose .* up ' "$root/calls" || true)" ]] || fail "$scenario mutated containers"
}

run_post_failure() {
  local scenario="$1" root job code
  root="$(seed_case "$scenario")"; job="rollback-apply-$scenario"
  set +e; invoke "$root" "$scenario" >/dev/null 2>&1; code=$?; set -e
  [[ "$code" -ne 0 ]] || fail "$scenario unexpectedly succeeded"
  assert_local_only "$root"
  assert_eq release-base "$(jq -r '.current_release_id' "$root/data/release-ledger/state.json")" "$scenario pointer not restored"
  assert_eq 7 "$(jq -r '.custom_version_high_water' "$root/data/release-ledger/state.json")" "$scenario high-water changed"
  assert_eq "$BASE_COMMIT" "$(cat "$root/repo-state/head")" "$scenario source not restored"
  assert_eq base "$(cat "$root/runtime/extensions-self")" "$scenario extensions not restored"
  assert_eq base "$(cat "$root/runtime/sub2api")" "$scenario main not restored"
  assert_eq failed_rolled_back "$(jq -r '.status' "$root/data/release-ledger/operations/$job.json")" "$scenario terminal status"
}

if [[ -n "${FIXTURE_ONLY:-}" ]]; then
  case "$FIXTURE_ONLY" in success|metadata-write-failure) run_success "$FIXTURE_ONLY" ;; extension-failure|main-failure|health-failure) run_post_failure "$FIXTURE_ONLY" ;; *) run_pre_refusal "$FIXTURE_ONLY" ;; esac
  printf 'apply-rollback=%s=PASS\n' "$FIXTURE_ONLY"; exit 0
fi
scenarios=(success expired pointer-drift high-water-drift target-record-drift artifact-drift backup-drift missing-image dirty-source extension-failure main-failure health-failure metadata-write-failure)
for ((offset=0;offset<${#scenarios[@]};offset+=5)); do pids=(); batch=(); for ((i=offset;i<offset+5&&i<${#scenarios[@]};i++)); do s="${scenarios[$i]}"; FIXTURE_ONLY="$s" bash "$0" > "$TMP_DIR/$s.log" 2>&1 & pids+=("$!"); batch+=("$s"); done; for i in "${!pids[@]}"; do wait "${pids[$i]}" || { cat "$TMP_DIR/${batch[$i]}.log" >&2; fail "${batch[$i]} failed"; }; done; done
printf 'apply-rollback=PASS\n'
