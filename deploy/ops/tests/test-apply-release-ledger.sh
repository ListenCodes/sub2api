#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d)"
cleanup_fixture() {
  if [[ "${KEEP_FIXTURE:-0}" == 1 ]]; then
    printf 'kept apply fixture: %s\n' "$TMP_DIR" >&2
  else
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup_fixture EXIT

CURRENT_RELEASE_ID=release-current
TARGET_RELEASE_ID=release-candidate-fixture
CURRENT_COMMIT=cccccccccccccccccccccccccccccccccccccccc
TARGET_COMMIT=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
STABLE_COMMIT=d0bdd7e771636a8d315f542cafd39484f39bd60c
OTHER_COMMIT=ffffffffffffffffffffffffffffffffffffffff
OLD_MAIN_DIGEST="sha256:$(printf '3%.0s' {1..64})"
OLD_EXT_DIGEST="sha256:$(printf '4%.0s' {1..64})"
NEW_MAIN_DIGEST="sha256:$(printf '1%.0s' {1..64})"
NEW_EXT_DIGEST="sha256:$(printf '2%.0s' {1..64})"

fail() { printf 'apply release ledger test failed: %s\n' "$1" >&2; exit 1; }
assert_eq() { [[ "$1" == "$2" ]] || fail "$3 (expected=$1 actual=$2)"; }

fixture_compose_json() {
  local main="$1" ext="$2" monitor_enabled="${3:-false}"
  jq -c -n --arg main "$main" --arg ext "$ext" --arg enabled "$monitor_enabled" '{name:"deploy",services:{sub2api:{image:$main,healthcheck:{test:["CMD","true"]},volumes:[{target:"/app/data"},{target:"/repo"},{target:"/var/run/docker.sock"}],networks:{"sub2api-network":{}}},"extensions-self":{image:$ext,healthcheck:{test:["CMD","true"]},environment:{ACCOUNT_MONITOR_ENABLED:$enabled,RISK_CONTROL_INTERNAL_SECRET:"fixture-secret"},networks:{"sub2api-network":{}}},postgres:{},redis:{},"risk-control-postgres":{}},volumes:{sub2api_data:{},postgres_data:{},redis_data:{},risk_control_postgres_data:{}}}'
}

assert_inherited_lock_contract() {
  command -v /usr/bin/flock >/dev/null 2>&1 || return 0
  timeout 3 bash -c '
    set -e
    exec 9>"$1"
    /usr/bin/flock -n 9
    export SUB2API_RELEASE_LEDGER_LOCK_FILE="$1"
    source "$2"
    ledger_with_lock true
  ' _ "$TMP_DIR/inherited-release.lock" "$ROOT_DIR/deploy/ops/release-ledger.sh" \
    || fail 'ledger helper deadlocked while the orchestrator lock fd was inherited'
}

write_exact_manifests() {
  local backup="$1"
  (cd "$backup/target" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
  (cd "$backup" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)
}

make_fake_tools() {
  local root="$1"
  mkdir -p "$root/bin"
  cat > "$root/bin/git" <<'EOF'
#!/usr/bin/env bash
set -e
printf 'git %s\n' "$*" >> "$FIXTURE_CALLS"
[[ "${1:-}" == -C ]] && shift 2
case "${1:-}" in
  fetch|merge|reset) exit 97 ;;
  rev-parse)
    case "${2:-}" in
      HEAD) cat "$FIXTURE_REPO_STATE/head" ;;
      origin/*) cat "$FIXTURE_REPO_STATE/origin" ;;
      *) exit 1 ;;
    esac
    ;;
  symbolic-ref)
    [[ -s "$FIXTURE_REPO_STATE/ref" ]] || exit 1
    cat "$FIXTURE_REPO_STATE/ref"
    ;;
  cat-file)
    [[ "${2:-}" == -e ]] || exit 1
    exit 0
    ;;
  status)
    [[ ! -e "$FIXTURE_REPO_STATE/dirty" ]] || printf ' M deploy/.env\n'
    ;;
  branch)
    if [[ "${2:-}" == --show-current ]]; then
      cat "$FIXTURE_REPO_STATE/ref"
    elif [[ "${2:-}" == -f ]]; then
      printf '%s\n' "$4" > "$FIXTURE_REPO_STATE/branch"
    else
      exit 1
    fi
    ;;
  switch)
    if [[ "${2:-}" == --detach ]]; then
      printf '%s\n' "$3" > "$FIXTURE_REPO_STATE/head"
      : > "$FIXTURE_REPO_STATE/ref"
    else
      [[ "$2" == custom-release ]] || exit 1
      cat "$FIXTURE_REPO_STATE/branch" > "$FIXTURE_REPO_STATE/head"
      printf 'custom-release\n' > "$FIXTURE_REPO_STATE/ref"
    fi
    ;;
  *) exit 1 ;;
esac
EOF

  cat > "$root/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -e
printf 'docker %s\n' "$*" >> "$FIXTURE_CALLS"
case "${1:-}" in
  pull) exit 97 ;;
  exec)
    [[ " $* " != *' pg_dump '* && " $* " != *' pg_restore '* ]] || exit 97
    if [[ "$FIXTURE_SCENARIO" == rollback-health-config ]]; then
      if [[ "$*" == *'/api/v1/admin/account-monitor/data-quality'* ]]; then
        printf '{"source_connected":true,"missing_group_requests":0,"data_as_of":"2026-07-23T08:00:00Z"}\n'
      else
        printf 'ok\n'
      fi
      exit 0
    fi
    exit 97
    ;;
  image)
    [[ "${2:-}" == inspect ]] || exit 1
    reference="${3:-}"
    if [[ "$FIXTURE_SCENARIO" == missing-image && "$reference" == "$FIXTURE_NEW_MAIN_REF" ]]; then
      exit 1
    fi
    format=''
    args=("$@")
    for ((i=0; i<${#args[@]}; i++)); do
      [[ "${args[$i]}" != --format ]] || format="${args[$((i+1))]}"
    done
    case "$format" in
      *RepoDigests*) jq -n --arg reference "$reference" '[$reference]' ;;
      *Config.Labels*)
        revision="$FIXTURE_TARGET_COMMIT"
        version="$FIXTURE_TARGET_VERSION"
        if [[ "$reference" == "$FIXTURE_OLD_MAIN_REF" || "$reference" == "$FIXTURE_OLD_EXT_REF" ]]; then
          revision="$FIXTURE_CURRENT_COMMIT"
          version=0.1.163
        fi
        jq -n --arg revision "$revision" --arg version "$version" '{"org.opencontainers.image.revision":$revision,"org.opencontainers.image.version":$version,"org.opencontainers.image.source":"https://github.com/ListenCodes/sub2api"}'
        ;;
      *Architecture*) printf 'amd64\n' ;;
      *'{{.Id}}'*)
        case "$reference" in
          "$FIXTURE_NEW_MAIN_REF") printf 'sha256:new-main-id\n' ;;
          "$FIXTURE_NEW_EXT_REF") printf 'sha256:new-ext-id\n' ;;
          "$FIXTURE_OLD_MAIN_REF") printf 'sha256:old-main-id\n' ;;
          "$FIXTURE_OLD_EXT_REF") printf 'sha256:old-ext-id\n' ;;
          *) exit 1 ;;
        esac
        ;;
      *) printf '{}\n' ;;
    esac
    ;;
  compose)
    env_file=''
    format=''
    args=("$@")
    for ((i=0; i<${#args[@]}; i++)); do
      [[ "${args[$i]}" != --env-file ]] || env_file="${args[$((i+1))]}"
      [[ "${args[$i]}" != --format ]] || format="${args[$((i+1))]}"
    done
    [[ -n "$env_file" ]] || exit 96
    if [[ " $* " == *' config '* ]]; then
      if [[ "$format" == json ]]; then
        main="$(sed -n 's/^SUB2API_IMAGE=//p' "$env_file" | tail -n 1)"
        ext="$(sed -n 's/^EXTENSIONS_SELF_IMAGE=//p' "$env_file" | tail -n 1)"
        enabled="$(sed -n 's/^ACCOUNT_MONITOR_ENABLED=//p' "$env_file" | tail -n 1)"
        jq -c -n --arg main "$main" --arg ext "$ext" --arg enabled "${enabled:-false}" '{name:"deploy",services:{sub2api:{image:$main,healthcheck:{test:["CMD","true"]},volumes:[{target:"/app/data"},{target:"/repo"},{target:"/var/run/docker.sock"}],networks:{"sub2api-network":{}}},"extensions-self":{image:$ext,healthcheck:{test:["CMD","true"]},environment:{ACCOUNT_MONITOR_ENABLED:$enabled,RISK_CONTROL_INTERNAL_SECRET:"fixture-secret"},networks:{"sub2api-network":{}}},postgres:{},redis:{},"risk-control-postgres":{}},volumes:{sub2api_data:{},postgres_data:{},redis_data:{},risk_control_postgres_data:{}}}'
      fi
      exit 0
    fi
    if [[ " $* " == *' up '* ]]; then
      [[ " $* " == *' --pull never '* ]] || exit 95
      [[ " $* " != *' postgres '* && " $* " != *' redis '* && " $* " != *' risk-control-postgres '* ]] || exit 94
      service="${args[-1]}"
      target=false
      grep -qF "$FIXTURE_NEW_MAIN_REF" "$env_file" && target=true
      if [[ "$target" == true && "$FIXTURE_SCENARIO" == extension-failure && "$service" == extensions-self ]]; then exit 31; fi
      if [[ "$target" == true && "$FIXTURE_SCENARIO" == main-failure && "$service" == sub2api ]]; then exit 32; fi
      printf '%s\n' "$([[ "$target" == true ]] && printf target || printf base)" > "$FIXTURE_RUNTIME_DIR/$service"
      exit 0
    fi
    [[ " $* " == *' ps '* ]] && exit 0
    exit 1
    ;;
  inspect)
    container="${@: -1}"
    format=''
    args=("$@")
    for ((i=0; i<${#args[@]}; i++)); do
      [[ "${args[$i]}" != --format ]] || format="${args[$((i+1))]}"
    done
    runtime=base
    [[ ! -s "$FIXTURE_RUNTIME_DIR/$container" ]] || runtime="$(cat "$FIXTURE_RUNTIME_DIR/$container")"
    if [[ "$format" == *'.Config.Image'* ]]; then
      if [[ "$container" == sub2api ]]; then
        [[ "$runtime" == target ]] && printf '%s\n' "$FIXTURE_NEW_MAIN_REF" || printf '%s\n' "$FIXTURE_OLD_MAIN_REF"
      else
        [[ "$runtime" == target ]] && printf '%s\n' "$FIXTURE_NEW_EXT_REF" || printf '%s\n' "$FIXTURE_OLD_EXT_REF"
      fi
      [[ "$runtime" != other ]] || printf 'invalid-runtime-image\n'
      exit 0
    fi
    if [[ "$format" == *'{{.Image}}'* ]]; then
      if [[ "$container" == sub2api ]]; then
        [[ "$runtime" == target ]] && printf 'sha256:new-main-id\n' || printf 'sha256:old-main-id\n'
      else
        [[ "$runtime" == target ]] && printf 'sha256:new-ext-id\n' || printf 'sha256:old-ext-id\n'
      fi
      [[ "$runtime" != other ]] || printf 'sha256:other-id\n'
      exit 0
    fi
    if [[ "$FIXTURE_SCENARIO" == health-failure && "$runtime" == target && "$container" == sub2api ]]; then
      printf 'unhealthy\n'
    elif [[ "$FIXTURE_SCENARIO" == rollback-health-config && "$runtime" == target && "$container" == sub2api ]]; then
      printf 'unhealthy\n'
    else
      printf 'healthy\n'
    fi
    ;;
  *) exit 1 ;;
esac
EOF

cat > "$root/bin/curl" <<'EOF'
#!/usr/bin/env bash
printf 'curl %s\n' "$*" >> "$FIXTURE_CALLS"
[[ "$FIXTURE_SCENARIO" != rollback-health-config ]] || exit 0
exit 97
EOF
  cat > "$root/bin/flock" <<'EOF'
#!/usr/bin/env bash
printf 'flock %s\n' "$*" >> "$FIXTURE_CALLS"
exit 0
EOF
  cat > "$root/bin/sync" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$root/bin/git" "$root/bin/docker" "$root/bin/curl" "$root/bin/flock" "$root/bin/sync"
}

seed_case() {
  local scenario="$1" root="$TMP_DIR/$scenario" job_id="update-apply-$scenario"
  local backup="$root/data/release-backups/prepared" base_artifact="$root/data/release-backups/base-artifact"
  mkdir -p "$root/data/release-ledger/releases" "$root/data/release-ledger/operations" "$root/data/release-prepared/$job_id" \
    "$backup/target" "$base_artifact" "$root/repo/deploy" "$root/repo-state" "$root/runtime"
  : > "$root/calls"
  printf '%s\n' "$CURRENT_COMMIT" > "$root/repo-state/head"
  printf '%s\n' "$TARGET_COMMIT" > "$root/repo-state/origin"
  printf '%s\n' "$CURRENT_COMMIT" > "$root/repo-state/branch"
  printf 'custom-release\n' > "$root/repo-state/ref"
  printf 'base\n' > "$root/runtime/extensions-self"
  printf 'base\n' > "$root/runtime/sub2api"

  printf 'services:\n  sub2api:\n    image: ${SUB2API_IMAGE}\n# current base\n' > "$root/repo/deploy/docker-compose.yml"
  printf 'services:\n  extensions-self:\n    image: ${EXTENSIONS_SELF_IMAGE}\n# current custom\n' > "$root/repo/deploy/docker-compose.custom.yml"
  base_monitor_enabled=false
  [[ "$scenario" != rollback-health-config ]] || base_monitor_enabled=true
  printf 'SUB2API_IMAGE=ghcr.io/listencodes/sub2api-custom@%s\nEXTENSIONS_SELF_IMAGE=ghcr.io/listencodes/sub2api-extensions@%s\nACCOUNT_MONITOR_ENABLED=%s\nKEEP=value\n' \
    "$OLD_MAIN_DIGEST" "$OLD_EXT_DIGEST" "$base_monitor_enabled" > "$root/repo/deploy/.env"
  cp -p "$root/repo/deploy/docker-compose.yml" "$backup/docker-compose.yml"
  cp -p "$root/repo/deploy/docker-compose.custom.yml" "$backup/docker-compose.custom.yml"
  cp -p "$root/repo/deploy/.env" "$backup/.env"

  printf 'services:\n  sub2api:\n    image: ${SUB2API_IMAGE}\n# target base\n' > "$backup/target/docker-compose.yml"
  printf 'services:\n  extensions-self:\n    image: ${EXTENSIONS_SELF_IMAGE}\n# target custom\n' > "$backup/target/docker-compose.custom.yml"
  printf 'SUB2API_IMAGE=ghcr.io/listencodes/sub2api-custom@%s\nEXTENSIONS_SELF_IMAGE=ghcr.io/listencodes/sub2api-extensions@%s\nACCOUNT_MONITOR_ENABLED=false\nKEEP=value\n' \
    "$NEW_MAIN_DIGEST" "$NEW_EXT_DIGEST" > "$backup/target/.env"
  fixture_compose_json "ghcr.io/listencodes/sub2api-custom@$NEW_MAIN_DIGEST" \
    "ghcr.io/listencodes/sub2api-extensions@$NEW_EXT_DIGEST" false > "$backup/target/rendered-compose.json"

  base_hash="$(sha256sum "$root/repo/deploy/docker-compose.yml" | awk '{print $1}')"
  custom_hash="$(sha256sum "$root/repo/deploy/docker-compose.custom.yml" | awk '{print $1}')"
  env_hash="$(sha256sum "$root/repo/deploy/.env" | awk '{print $1}')"
  current_rendered_hash="$(fixture_compose_json "ghcr.io/listencodes/sub2api-custom@$OLD_MAIN_DIGEST" "ghcr.io/listencodes/sub2api-extensions@$OLD_EXT_DIGEST" "$base_monitor_enabled" | sha256sum | awk '{print $1}')"

  jq -n --arg release "$CURRENT_RELEASE_ID" --arg operation "$job_id" \
    '{schema_version:1,current_release_id:$release,custom_version_high_water:4,active_operation_id:$operation,updated_at:"2026-07-23T08:00:00Z"}' \
    > "$root/data/release-ledger/state.json"
  jq -n --arg release "$CURRENT_RELEASE_ID" --arg official_commit "$STABLE_COMMIT" --arg custom_commit "$CURRENT_COMMIT" \
    --arg main "$OLD_MAIN_DIGEST" --arg ext "$OLD_EXT_DIGEST" --arg backup "$base_artifact" \
    --arg base_hash "$base_hash" --arg custom_hash "$custom_hash" --arg rendered_hash "$current_rendered_hash" --arg env_hash "$env_hash" \
    '{schema_version:1,release_id:$release,official_version:"v0.1.163",official_commit:$official_commit,custom_version:"v1.0.4",custom_version_sequence:4,custom_commit:$custom_commit,main_digest:$main,extensions_digest:$ext,base_compose_sha256:$base_hash,custom_compose_sha256:$custom_hash,rendered_compose_sha256:$rendered_hash,env_sha256:$env_hash,backup_dir:$backup,backup_manifest_sha256:("9"*64),published_at:"2026-07-23T08:00:00Z",source_kind:"custom",operation_id:"update-previous"}' \
    > "$root/data/release-ledger/releases/$CURRENT_RELEASE_ID.json"
  jq -n --arg production "$CURRENT_COMMIT" --arg stable_commit "$STABLE_COMMIT" --arg main "$OLD_MAIN_DIGEST" --arg ext "$OLD_EXT_DIGEST" --arg backup "$base_artifact" \
    '{production_commit:$production,stable_release_tag:"v0.1.163",stable_release_commit:$stable_commit,main_digest:$main,extensions_digest:$ext,published_at:"2026-07-23T08:00:00Z",backup_dir:$backup,release_id:"release-current",official_version:"v0.1.163",custom_version:"v1.0.4",custom_version_sequence:4}' \
    > "$root/data/release-state.json"
  cp -p "$root/data/release-state.json" "$backup/release-state.json"

  for file in container-metadata.json image-metadata.txt rollback-tags.txt sub2api_db.dump sub2api_db.list \
    risk_control_db.dump risk_control_db.list docker-containers.txt docker-images.txt; do
    printf 'fixture %s\n' "$file" > "$backup/$file"
  done
  printf '/etc/nginx/sites-available/sub2api.conf\n' > "$backup/nginx-vhost.path"
  printf '/etc/nginx/ssl/origin.crt\n' > "$backup/origin-cert.path"
  printf '/etc/nginx/ssl/origin.key\n' > "$backup/origin-key.path"
  printf 'nginx\n' > "$backup/sub2api.conf"
  printf 'cert\n' > "$backup/origin.crt"
  printf 'key\n' > "$backup/origin.key"
  write_exact_manifests "$backup"

  target_base_hash="$(sha256sum "$backup/target/docker-compose.yml" | awk '{print $1}')"
  target_custom_hash="$(sha256sum "$backup/target/docker-compose.custom.yml" | awk '{print $1}')"
  target_rendered_hash="$(sha256sum "$backup/target/rendered-compose.json" | awk '{print $1}')"
  target_env_hash="$(sha256sum "$backup/target/.env" | awk '{print $1}')"
  target_manifest_hash="$(sha256sum "$backup/target/SHA256SUMS" | awk '{print $1}')"
  backup_manifest_hash="$(sha256sum "$backup/SHA256SUMS" | awk '{print $1}')"
  prepared_at="$(date -u -d '-1 minute' '+%Y-%m-%dT%H:%M:%SZ')"
  expires_at="$(date -u -d '+15 minutes' '+%Y-%m-%dT%H:%M:%SZ')"
  update_kind=custom
  target_official=v0.1.163
  target_custom_version=v1.0.5
  proposed_sequence=5
  advances=true
  target_stable_commit="$STABLE_COMMIT"
  target_custom_commit="$TARGET_COMMIT"
  if [[ "$scenario" == official-success ]]; then
    update_kind=official
    target_official=v0.1.164
    target_custom_version=v1.0.4
    proposed_sequence=4
    advances=false
    target_stable_commit="$OTHER_COMMIT"
    target_custom_commit="$CURRENT_COMMIT"
  elif [[ "$scenario" == combined-success ]]; then
    update_kind=combined
    target_official=v0.1.164
    target_stable_commit="$OTHER_COMMIT"
  fi
  manifest="$root/data/release-prepared/$job_id/manifest.json"
  jq -n --arg base "$CURRENT_RELEASE_ID" --arg target_release "$TARGET_RELEASE_ID" \
    --arg update_kind "$update_kind" --arg target_official "$target_official" --arg target_custom_version "$target_custom_version" \
    --argjson proposed_sequence "$proposed_sequence" --argjson advances "$advances" \
    --arg source "$CURRENT_COMMIT" --arg target "$TARGET_COMMIT" --arg target_custom_commit "$target_custom_commit" --arg stable "$target_stable_commit" \
    --arg main "$NEW_MAIN_DIGEST" --arg ext "$NEW_EXT_DIGEST" --arg old_main "$OLD_MAIN_DIGEST" --arg old_ext "$OLD_EXT_DIGEST" \
    --arg current_base "$base_hash" --arg current_custom "$custom_hash" --arg target_base "$target_base_hash" \
    --arg target_custom "$target_custom_hash" --arg target_rendered "$target_rendered_hash" --arg target_env "$target_env_hash" \
    --arg target_manifest "$target_manifest_hash" --arg backup "$backup" --arg backup_manifest "$backup_manifest_hash" \
    --arg prepared "$prepared_at" --arg expires "$expires_at" \
    '{schema_version:1,operation_kind:"update",update_kind:$update_kind,custom_docs_only:false,base_release_id:$base,base_custom_high_water:4,target_release_id:$target_release,current_official_version:"v0.1.163",current_custom_version:"v1.0.4",target_official_version:$target_official,target_custom_version:$target_custom_version,proposed_custom_sequence:$proposed_sequence,advances_custom_version:$advances,source_commit:$source,target_commit:$target,target_custom_commit:$target_custom_commit,stable_release_tag:$target_official,stable_release_commit:$stable,main_digest:$main,extensions_digest:$ext,current_main_digest:$old_main,current_extensions_digest:$old_ext,current_base_compose_sha256:$current_base,current_custom_compose_sha256:$current_custom,target_base_compose_sha256:$target_base,target_custom_compose_sha256:$target_custom,target_rendered_compose_sha256:$target_rendered,target_env_sha256:$target_env,target_artifact_manifest_sha256:$target_manifest,backup_dir:$backup,backup_manifest_sha256:$backup_manifest,prepared_at:$prepared,expires_at:$expires,workflow_url:"https://github.com/ListenCodes/sub2api/actions/runs/1",images_verified:true,compose_contract:"deploy-explicit-pair-v1",backup_contract:"complete-paired-snapshot-v1"}' \
    > "$manifest"
  sha256sum "$manifest" > "$root/data/release-prepared/$job_id/manifest.sha256"

  operation_metadata="$(jq --arg job "$job_id" --arg manifest "$manifest" --arg manifest_sha "$(awk '{print $1}' "$root/data/release-prepared/$job_id/manifest.sha256")" \
    '. + {job_id:$job,action:"apply",status:"apply_queued",message:"queued",prepared_manifest:$manifest,prepared_manifest_sha256:$manifest_sha,updated_at:"2026-07-23T08:10:00Z",started_at:"2026-07-23T08:00:00Z",finished_at:null,published:false,production_changed:false}' "$manifest")"
  printf '%s\n' "$operation_metadata" > "$root/data/release-ledger/operations/$job_id.json"
  printf '%s\n' "$job_id" > "$root/data/release-current-job-id"

  make_fake_tools "$root"
  printf '%s\n' "$root"
}

seed_committed_target() {
  local root="$1" status="$2" job_id="update-apply-${root##*/}" manifest backup published record projection
  manifest="$root/data/release-prepared/$job_id/manifest.json"
  backup="$(jq -r '.backup_dir' "$manifest")"
  published=2026-07-23T08:20:00Z
  record="$(jq -n --arg release "$TARGET_RELEASE_ID" --arg official_commit "$STABLE_COMMIT" --arg custom_commit "$TARGET_COMMIT" \
    --arg main "$NEW_MAIN_DIGEST" --arg ext "$NEW_EXT_DIGEST" --arg backup "$backup" \
    --arg base_hash "$(jq -r '.target_base_compose_sha256' "$manifest")" --arg custom_hash "$(jq -r '.target_custom_compose_sha256' "$manifest")" \
    --arg rendered_hash "$(jq -r '.target_rendered_compose_sha256' "$manifest")" --arg env_hash "$(jq -r '.target_env_sha256' "$manifest")" \
    --arg backup_hash "$(jq -r '.backup_manifest_sha256' "$manifest")" --arg published "$published" --arg operation "$job_id" \
    '{schema_version:1,release_id:$release,official_version:"v0.1.163",official_commit:$official_commit,custom_version:"v1.0.5",custom_version_sequence:5,custom_commit:$custom_commit,main_digest:$main,extensions_digest:$ext,base_compose_sha256:$base_hash,custom_compose_sha256:$custom_hash,rendered_compose_sha256:$rendered_hash,env_sha256:$env_hash,backup_dir:$backup,backup_manifest_sha256:$backup_hash,published_at:$published,source_kind:"custom",operation_id:$operation}')"
  printf '%s\n' "$record" > "$root/data/release-ledger/releases/$TARGET_RELEASE_ID.json"
  projection="$(jq --argjson record "$record" '. + {production_commit:$record.custom_commit,stable_release_tag:$record.official_version,stable_release_commit:$record.official_commit,main_digest:$record.main_digest,extensions_digest:$record.extensions_digest,published_at:$record.published_at,backup_dir:$record.backup_dir,release_id:$record.release_id,official_version:$record.official_version,custom_version:$record.custom_version,custom_version_sequence:$record.custom_version_sequence}' "$root/data/release-state.json")"
  printf '%s\n' "$projection" > "$root/data/release-state.json"
  jq --arg release "$TARGET_RELEASE_ID" '.current_release_id=$release | .custom_version_high_water=5 | .active_operation_id=null' \
    "$root/data/release-ledger/state.json" > "$root/data/release-ledger/state.tmp"
  mv "$root/data/release-ledger/state.tmp" "$root/data/release-ledger/state.json"
  jq --arg status "$status" --arg published "$published" --arg commit "$TARGET_COMMIT" --arg backup "$backup" '
    .status=$status | .published_at=$published | .action="apply"
    | if $status == "success" then
        .finished_at=$published
        | .published=true
        | .published_commit=$commit
        | .production_changed=true
        | .artifact_path=$backup
        | .rollback={attempted:false,succeeded:false,message:""}
      else . end
  ' \
    "$root/data/release-ledger/operations/$job_id.json" > "$root/data/release-ledger/operations/$job_id.tmp"
  mv "$root/data/release-ledger/operations/$job_id.tmp" "$root/data/release-ledger/operations/$job_id.json"
  cp -p "$backup/target/docker-compose.yml" "$root/repo/deploy/docker-compose.yml"
  cp -p "$backup/target/docker-compose.custom.yml" "$root/repo/deploy/docker-compose.custom.yml"
  cp -p "$backup/target/.env" "$root/repo/deploy/.env"
  printf '%s\n' "$TARGET_COMMIT" > "$root/repo-state/head"
  : > "$root/repo-state/ref"
  printf 'target\n' > "$root/runtime/extensions-self"
  printf 'target\n' > "$root/runtime/sub2api"
}

invoke_apply() {
  local root="$1" scenario="$2" job_id="update-apply-$scenario" failpoint='' skip_external=1
  local -a apply_command=("$ROOT_DIR/deploy/ops/apply-release.sh")
  [[ "${DEBUG_APPLY:-0}" != 1 ]] || apply_command=(bash -x "$ROOT_DIR/deploy/ops/apply-release.sh")
  [[ "$scenario" != projection-write-failure ]] || failpoint=before_projection
  [[ "$scenario" != state-write-failure ]] || failpoint=before_state
  [[ "$scenario" != rollback-health-config ]] || skip_external=0
  PATH="$root/bin:$PATH" FIXTURE_CALLS="$root/calls" FIXTURE_SCENARIO="$scenario" \
    FIXTURE_REPO_STATE="$root/repo-state" FIXTURE_RUNTIME_DIR="$root/runtime" \
    FIXTURE_NEW_MAIN_REF="ghcr.io/listencodes/sub2api-custom@$NEW_MAIN_DIGEST" FIXTURE_TARGET_COMMIT="$TARGET_COMMIT" \
    FIXTURE_NEW_EXT_REF="ghcr.io/listencodes/sub2api-extensions@$NEW_EXT_DIGEST" \
    FIXTURE_OLD_MAIN_REF="ghcr.io/listencodes/sub2api-custom@$OLD_MAIN_DIGEST" \
    FIXTURE_OLD_EXT_REF="ghcr.io/listencodes/sub2api-extensions@$OLD_EXT_DIGEST" FIXTURE_CURRENT_COMMIT="$CURRENT_COMMIT" \
    FIXTURE_TARGET_VERSION="$(jq -r '.target_official_version | ltrimstr("v")' "$root/data/release-prepared/$job_id/manifest.json")" \
    SUB2API_DATA_DIR="$root/data" SUB2API_REPO="$root/repo" SUB2API_ENV_FILE="$root/repo/deploy/.env" \
    SUB2API_COMPOSE_BASE="$root/repo/deploy/docker-compose.yml" SUB2API_COMPOSE_CUSTOM="$root/repo/deploy/docker-compose.custom.yml" \
    SUB2API_RELEASE_BACKUP_ROOT="$root/data/release-backups" SUB2API_RELEASE_LEDGER_ROOT="$root/data/release-ledger" \
    SUB2API_RELEASE_OPERATIONS_DIR="$root/data/release-ledger/operations" SUB2API_PREPARED_ROOT="$root/data/release-prepared" \
    SUB2API_CURRENT_RELEASE_JOB_FILE="$root/data/release-current-job-id" SUB2API_RELEASE_STATE_FILE="$root/data/release-state.json" \
    SUB2API_RELEASE_LEDGER_LOCK_FILE="$root/data/release.lock" SUB2API_LEDGER_COMMIT_FAILPOINT="$failpoint" \
    SUB2API_RELEASE_STATE_HELPER="$ROOT_DIR/deploy/ops/release-state.sh" SUB2API_RELEASE_COMMON_HELPER="$ROOT_DIR/deploy/ops/release-common.sh" \
    SUB2API_RELEASE_LEDGER_HELPER="$ROOT_DIR/deploy/ops/release-ledger.sh" SUB2API_SYNC_PUBLISH_LOG="$root/release.log" \
    SUB2API_SKIP_EXTERNAL_HEALTH_CHECKS="$skip_external" SUB2API_HEALTH_WAIT_TIMEOUT_SECONDS=2 SUB2API_HEALTH_WAIT_INTERVAL_SECONDS=0 \
    "${apply_command[@]}" --job-id "$job_id"
}

assert_local_only_contract() {
  local root="$1"
  ! grep -Eq 'git .* (fetch|merge|reset)( |$)|docker pull|pg_dump|pg_restore|api\.github\.com|wait-for-actions|verify-release-images' "$root/calls" \
    || fail "${root##*/} used a forbidden remote, pull, reset/merge, or database command"
  while IFS= read -r command; do
    [[ "$command" == *' --pull never '* ]] || fail "${root##*/} used Compose up without --pull never"
    [[ "$command" != *' postgres '* && "$command" != *' redis '* && "$command" != *' risk-control-postgres '* ]] \
      || fail "${root##*/} lifecycle-managed a database or Redis"
  done < <(grep 'docker compose .* up ' "$root/calls" || true)
}

apply_scenario_mutation() {
  local root="$1" scenario="$2" job_id manifest
  job_id="update-apply-$scenario"
  manifest="$root/data/release-prepared/$job_id/manifest.json"
  case "$scenario" in
    expired)
      jq '.expires_at="2020-01-01T00:00:00Z"' "$manifest" > "$manifest.tmp" && mv "$manifest.tmp" "$manifest"
      sha256sum "$manifest" > "$root/data/release-prepared/$job_id/manifest.sha256"
      ;;
    current-release-drift) jq '.current_release_id="release-other"' "$root/data/release-ledger/state.json" > "$root/state.tmp" && mv "$root/state.tmp" "$root/data/release-ledger/state.json" ;;
    high-water-drift) jq '.custom_version_high_water=5' "$root/data/release-ledger/state.json" > "$root/state.tmp" && mv "$root/state.tmp" "$root/data/release-ledger/state.json" ;;
    origin-drift) printf '%s\n' "$OTHER_COMMIT" > "$root/repo-state/origin" ;;
    compose-drift) printf '# drift\n' >> "$root/repo/deploy/docker-compose.custom.yml" ;;
    env-drift) printf 'DRIFT=1\n' >> "$root/repo/deploy/.env" ;;
    digest-drift) jq --arg digest "sha256:$(printf '8%.0s' {1..64})" '.main_digest=$digest' "$root/data/release-state.json" > "$root/projection.tmp" && mv "$root/projection.tmp" "$root/data/release-state.json" ;;
    backup-drift) printf 'undeclared drift\n' > "$(jq -r '.backup_dir' "$manifest")/undeclared.txt" ;;
    dirty-source) touch "$root/repo-state/dirty" ;;
    running-container-drift) printf 'other\n' > "$root/runtime/sub2api" ;;
    kind-semantic-drift)
      jq --arg commit "$OTHER_COMMIT" '.target_official_version="v0.1.164" | .stable_release_tag="v0.1.164" | .stable_release_commit=$commit' \
        "$manifest" > "$manifest.tmp" && mv "$manifest.tmp" "$manifest"
      sha256sum "$manifest" > "$root/data/release-prepared/$job_id/manifest.sha256"
      ;;
    duplicate) seed_committed_target "$root" success ;;
    recovery) seed_committed_target "$root" health_checking ;;
    partial-record)
      cp -p "$root/data/release-ledger/state.json" "$root/base-state.json"
      cp -p "$root/data/release-state.json" "$root/base-projection.json"
      seed_committed_target "$root" health_checking
      mv "$root/base-state.json" "$root/data/release-ledger/state.json"
      mv "$root/base-projection.json" "$root/data/release-state.json"
      ;;
    partial-projection)
      cp -p "$root/data/release-ledger/state.json" "$root/base-state.json"
      seed_committed_target "$root" health_checking
      mv "$root/base-state.json" "$root/data/release-ledger/state.json"
      ;;
    partial-expired)
      cp -p "$root/data/release-ledger/state.json" "$root/base-state.json"
      cp -p "$root/data/release-state.json" "$root/base-projection.json"
      seed_committed_target "$root" health_checking
      mv "$root/base-state.json" "$root/data/release-ledger/state.json"
      mv "$root/base-projection.json" "$root/data/release-state.json"
      jq '.expires_at="2020-01-01T00:00:00Z"' "$manifest" > "$manifest.tmp" && mv "$manifest.tmp" "$manifest"
      sha256sum "$manifest" > "$root/data/release-prepared/$job_id/manifest.sha256"
      ;;
    runtime-only|runtime-only-expired)
      cp -p "$root/data/release-ledger/state.json" "$root/base-state.json"
      cp -p "$root/data/release-state.json" "$root/base-projection.json"
      seed_committed_target "$root" health_checking
      rm "$root/data/release-ledger/releases/$TARGET_RELEASE_ID.json"
      mv "$root/base-state.json" "$root/data/release-ledger/state.json"
      mv "$root/base-projection.json" "$root/data/release-state.json"
      if [[ "$scenario" == runtime-only-expired ]]; then
        jq '.expires_at="2020-01-01T00:00:00Z"' "$manifest" > "$manifest.tmp" && mv "$manifest.tmp" "$manifest"
        sha256sum "$manifest" > "$root/data/release-prepared/$job_id/manifest.sha256"
      fi
      ;;
    runtime-partial)
      cp -p "$root/data/release-ledger/state.json" "$root/base-state.json"
      cp -p "$root/data/release-state.json" "$root/base-projection.json"
      seed_committed_target "$root" switching_main
      rm "$root/data/release-ledger/releases/$TARGET_RELEASE_ID.json"
      printf 'base\n' > "$root/runtime/sub2api"
      mv "$root/base-state.json" "$root/data/release-ledger/state.json"
      mv "$root/base-projection.json" "$root/data/release-state.json"
      ;;
    runtime-partial-extensions)
      cp -p "$root/data/release-ledger/state.json" "$root/base-state.json"
      cp -p "$root/data/release-state.json" "$root/base-projection.json"
      seed_committed_target "$root" switching_extensions
      rm "$root/data/release-ledger/releases/$TARGET_RELEASE_ID.json"
      printf 'base\n' > "$root/runtime/extensions-self"
      printf 'base\n' > "$root/runtime/sub2api"
      mv "$root/base-state.json" "$root/data/release-ledger/state.json"
      mv "$root/base-projection.json" "$root/data/release-state.json"
      ;;
    runtime-partial-rollback)
      cp -p "$root/data/release-ledger/state.json" "$root/base-state.json"
      cp -p "$root/data/release-state.json" "$root/base-projection.json"
      seed_committed_target "$root" rolling_back
      rm "$root/data/release-ledger/releases/$TARGET_RELEASE_ID.json"
      printf 'base\n' > "$root/runtime/extensions-self"
      mv "$root/base-state.json" "$root/data/release-ledger/state.json"
      mv "$root/base-projection.json" "$root/data/release-state.json"
      ;;
    terminal-active)
      jq '.status="drifted" | .message="pre-mutation drift" | .error_code="TEST_DRIFT"
        | .published=false | .production_changed=false | .finished_at="2026-07-23T08:10:00Z"' \
        "$root/data/release-ledger/operations/$job_id.json" > "$root/operation.tmp"
      mv "$root/operation.tmp" "$root/data/release-ledger/operations/$job_id.json"
      ;;
    malformed-terminal-recovery)
      cp -p "$root/data/release-ledger/state.json" "$root/base-state.json"
      cp -p "$root/data/release-state.json" "$root/base-projection.json"
      seed_committed_target "$root" success
      mv "$root/base-state.json" "$root/data/release-ledger/state.json"
      mv "$root/base-projection.json" "$root/data/release-state.json"
      jq --arg commit "$OTHER_COMMIT" '.published_commit=$commit' \
        "$root/data/release-ledger/operations/$job_id.json" > "$root/operation.tmp"
      mv "$root/operation.tmp" "$root/data/release-ledger/operations/$job_id.json"
      ;;
  esac
}

run_success_case() {
  local scenario="$1" root job_id manifest record
  root="$(seed_case "$scenario")"
  job_id="update-apply-$scenario"
  manifest="$root/data/release-prepared/$job_id/manifest.json"
  apply_scenario_mutation "$root" "$scenario"
  invoke_apply "$root" "$scenario" || fail "$scenario apply failed: status=$(jq -r '.status' "$root/data/release-ledger/operations/$job_id.json") code=$(jq -r '.error_code // empty' "$root/data/release-ledger/operations/$job_id.json") cause=$(jq -r '.cause_error_code // empty' "$root/data/release-ledger/operations/$job_id.json") message=$(jq -r '.message // empty' "$root/data/release-ledger/operations/$job_id.json")"
  assert_local_only_contract "$root"
  assert_eq "$TARGET_RELEASE_ID" "$(jq -r '.current_release_id' "$root/data/release-ledger/state.json")" "$scenario current release mismatch"
  expected_sequence="$(jq -r '.proposed_custom_sequence' "$manifest")"
  expected_custom_version="$(jq -r '.target_custom_version' "$manifest")"
  assert_eq "$expected_sequence" "$(jq -r '.custom_version_high_water' "$root/data/release-ledger/state.json")" "$scenario high-water mismatch"
  assert_eq null "$(jq -r '.active_operation_id' "$root/data/release-ledger/state.json")" "$scenario active operation not cleared"
  record="$root/data/release-ledger/releases/$TARGET_RELEASE_ID.json"
  [[ -s "$record" ]] || fail "$scenario did not create the immutable release record"
  assert_eq "$expected_custom_version" "$(jq -r '.custom_version' "$record")" "$scenario record custom version mismatch"
  assert_eq "$TARGET_COMMIT" "$(jq -r '.production_commit' "$root/data/release-state.json")" "$scenario projection commit mismatch"
  assert_eq "$expected_custom_version" "$(jq -r '.custom_version' "$root/data/release-state.json")" "$scenario projection custom version mismatch"
  assert_eq success "$(jq -r '.status' "$root/data/release-ledger/operations/$job_id.json")" "$scenario operation did not settle success"
  assert_eq true "$(jq -r '.published' "$root/data/release-ledger/operations/$job_id.json")" "$scenario operation did not record publication"
  assert_eq true "$(jq -r '.production_changed' "$root/data/release-ledger/operations/$job_id.json")" "$scenario operation did not record production change"
  assert_eq "$TARGET_COMMIT" "$(jq -r '.published_commit' "$root/data/release-ledger/operations/$job_id.json")" "$scenario published commit mismatch"
  assert_eq "$TARGET_COMMIT" "$(cat "$root/repo-state/head")" "$scenario source did not switch"
  cmp -s "$root/repo/deploy/docker-compose.yml" "$(jq -r '.backup_dir' "$manifest")/target/docker-compose.yml" || fail "$scenario did not install target base Compose"
  cmp -s "$root/repo/deploy/.env" "$(jq -r '.backup_dir' "$manifest")/target/.env" || fail "$scenario did not install target environment"
  if [[ "$scenario" == duplicate || "$scenario" == recovery || "$scenario" == partial-record || "$scenario" == partial-projection || "$scenario" == partial-expired || "$scenario" == runtime-only || "$scenario" == runtime-only-expired ]]; then
    [[ -z "$(grep 'docker compose .* up ' "$root/calls" || true)" ]] || fail "$scenario repeated container lifecycle"
  else
    extensions_line="$(grep -n 'docker compose .* up .* extensions-self$' "$root/calls" | head -n 1 | cut -d: -f1)"
    main_line="$(grep -n 'docker compose .* up .* sub2api$' "$root/calls" | head -n 1 | cut -d: -f1)"
    [[ -n "$extensions_line" && -n "$main_line" && "$extensions_line" -lt "$main_line" ]] || fail "$scenario did not switch extensions before main"
  fi
}

run_pre_mutation_refusal() {
  local scenario="$1" root state_before projection_before source_before files_before exit_code
  root="$(seed_case "$scenario")"
  apply_scenario_mutation "$root" "$scenario"
  state_before="$(jq -c '{current_release_id,custom_version_high_water}' "$root/data/release-ledger/state.json")"
  projection_before="$(sha256sum "$root/data/release-state.json")"
  source_before="$(cat "$root/repo-state/head")"
  files_before="$(sha256sum "$root/repo/deploy/docker-compose.yml" "$root/repo/deploy/docker-compose.custom.yml" "$root/repo/deploy/.env")"
  set +e
  invoke_apply "$root" "$scenario" >/dev/null 2>&1
  exit_code=$?
  set -e
  [[ "$exit_code" -ne 0 ]] || fail "$scenario was not rejected"
  assert_local_only_contract "$root"
  assert_eq "$state_before" "$(jq -c '{current_release_id,custom_version_high_water}' "$root/data/release-ledger/state.json")" "$scenario changed release pointer or high-water"
  assert_eq null "$(jq -r '.active_operation_id' "$root/data/release-ledger/state.json")" "$scenario did not release the active operation"
  assert_eq "$projection_before" "$(sha256sum "$root/data/release-state.json")" "$scenario changed compatibility projection"
  assert_eq "$source_before" "$(cat "$root/repo-state/head")" "$scenario changed source"
  assert_eq "$files_before" "$(sha256sum "$root/repo/deploy/docker-compose.yml" "$root/repo/deploy/docker-compose.custom.yml" "$root/repo/deploy/.env")" "$scenario changed production files"
  [[ -z "$(grep 'docker compose .* up ' "$root/calls" || true)" ]] || fail "$scenario reached container mutation"
}

run_post_mutation_failure() {
  local scenario="$1" root job_id exit_code backup
  root="$(seed_case "$scenario")"
  apply_scenario_mutation "$root" "$scenario"
  job_id="update-apply-$scenario"
  backup="$(jq -r '.backup_dir' "$root/data/release-prepared/$job_id/manifest.json")"
  set +e
  invoke_apply "$root" "$scenario" >/dev/null 2>&1
  exit_code=$?
  set -e
  [[ "$exit_code" -ne 0 ]] || fail "$scenario unexpectedly succeeded"
  assert_local_only_contract "$root"
  assert_eq "$CURRENT_RELEASE_ID" "$(jq -r '.current_release_id' "$root/data/release-ledger/state.json")" "$scenario changed current release"
  assert_eq 4 "$(jq -r '.custom_version_high_water' "$root/data/release-ledger/state.json")" "$scenario consumed a custom version"
  assert_eq "$CURRENT_COMMIT" "$(jq -r '.production_commit' "$root/data/release-state.json")" "$scenario did not restore compatibility projection"
  assert_eq "$CURRENT_COMMIT" "$(cat "$root/repo-state/head")" "$scenario did not restore source"
  cmp -s "$root/repo/deploy/docker-compose.yml" "$backup/docker-compose.yml" || fail "$scenario did not restore base Compose"
  cmp -s "$root/repo/deploy/.env" "$backup/.env" || fail "$scenario did not restore environment"
  assert_eq failed_rolled_back "$(jq -r '.status' "$root/data/release-ledger/operations/$job_id.json")" "$scenario rollback status mismatch"
  assert_eq false "$(jq -r '.production_changed' "$root/data/release-ledger/operations/$job_id.json")" "$scenario reported production changed after restoration"
  if [[ "$scenario" == rollback-health-config ]]; then
    grep -q '/api/v1/admin/account-monitor/data-quality' "$root/calls" || fail 'base restoration health used the target monitor configuration'
  fi
}

run_inconsistent_recovery_refusal() {
  local scenario="$1" root job_id exit_code state_before projection_before operation_before
  root="$(seed_case "$scenario")"
  job_id="update-apply-$scenario"
  apply_scenario_mutation "$root" "$scenario"
  state_before="$(sha256sum "$root/data/release-ledger/state.json")"
  projection_before="$(sha256sum "$root/data/release-state.json")"
  operation_before="$(sha256sum "$root/data/release-ledger/operations/$job_id.json")"
  set +e
  invoke_apply "$root" "$scenario" >/dev/null 2>&1
  exit_code=$?
  set -e
  [[ "$exit_code" -ne 0 ]] || fail "$scenario unexpectedly succeeded"
  assert_eq "$state_before" "$(sha256sum "$root/data/release-ledger/state.json")" "$scenario changed ledger state before refusing recovery"
  assert_eq "$projection_before" "$(sha256sum "$root/data/release-state.json")" "$scenario changed compatibility projection before refusing recovery"
  assert_eq "$operation_before" "$(sha256sum "$root/data/release-ledger/operations/$job_id.json")" "$scenario rewrote contradictory operation metadata"
  [[ -z "$(grep 'docker compose .* up ' "$root/calls" || true)" ]] || fail "$scenario repeated container lifecycle"
}

if [[ -n "${FIXTURE_ONLY:-}" ]]; then
  case "$FIXTURE_ONLY" in
    success|official-success|combined-success|projection-write-failure|state-write-failure|duplicate|recovery|partial-record|partial-projection|partial-expired|runtime-only|runtime-only-expired)
      run_success_case "$FIXTURE_ONLY"
      ;;
    expired|current-release-drift|high-water-drift|origin-drift|compose-drift|env-drift|digest-drift|backup-drift|missing-image|dirty-source|running-container-drift|kind-semantic-drift|terminal-active)
      run_pre_mutation_refusal "$FIXTURE_ONLY"
      ;;
    extension-failure|main-failure|health-failure|rollback-health-config|runtime-partial|runtime-partial-extensions|runtime-partial-rollback)
      run_post_mutation_failure "$FIXTURE_ONLY"
      ;;
    malformed-terminal-recovery) run_inconsistent_recovery_refusal "$FIXTURE_ONLY" ;;
    *) fail "unknown FIXTURE_ONLY scenario: $FIXTURE_ONLY" ;;
  esac
  printf 'apply-release-ledger=%s=PASS\n' "$FIXTURE_ONLY"
  exit 0
fi

assert_inherited_lock_contract
scenarios=(
  success official-success combined-success
  expired current-release-drift high-water-drift origin-drift compose-drift env-drift digest-drift
  backup-drift missing-image dirty-source running-container-drift kind-semantic-drift
  extension-failure main-failure health-failure rollback-health-config
  runtime-partial runtime-partial-extensions runtime-partial-rollback terminal-active malformed-terminal-recovery
  projection-write-failure state-write-failure duplicate recovery partial-record partial-projection partial-expired
  runtime-only runtime-only-expired
)
for ((offset=0; offset<${#scenarios[@]}; offset+=6)); do
  pids=()
  batch=()
  for ((index=offset; index<offset+6 && index<${#scenarios[@]}; index++)); do
    scenario="${scenarios[$index]}"
    FIXTURE_ONLY="$scenario" bash "$0" > "$TMP_DIR/$scenario.log" 2>&1 &
    pids+=("$!")
    batch+=("$scenario")
  done
  for index in "${!pids[@]}"; do
    if ! wait "${pids[$index]}"; then
      cat "$TMP_DIR/${batch[$index]}.log" >&2
      fail "${batch[$index]} parallel fixture failed"
    fi
  done
done

printf 'apply-release-ledger=PASS\n'
