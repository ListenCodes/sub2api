#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

CURRENT_RELEASE_ID=release-current
CURRENT_COMMIT=cccccccccccccccccccccccccccccccccccccccc
TARGET_COMMIT=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
CURRENT_STABLE_COMMIT=d0bdd7e771636a8d315f542cafd39484f39bd60c
TARGET_STABLE_COMMIT=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
OLD_MAIN_DIGEST="sha256:$(printf '3%.0s' {1..64})"
OLD_EXT_DIGEST="sha256:$(printf '4%.0s' {1..64})"
MAIN_DIGEST="sha256:$(printf '1%.0s' {1..64})"
EXT_DIGEST="sha256:$(printf '2%.0s' {1..64})"

fail() { printf 'prepare release ledger test failed: %s\n' "$1" >&2; exit 1; }
assert_eq() { [[ "$1" == "$2" ]] || fail "$3 (expected=$1 actual=$2)"; }

fixture_compose_json() {
  local main="$1" ext="$2"
  jq -n --arg main "$main" --arg ext "$ext" '{name:"deploy",services:{sub2api:{image:$main,healthcheck:{test:["CMD","true"]},volumes:[{target:"/app/data",source:"sub2api_data",type:"volume",read_only:false},{target:"/app/scripts/sync-upstream.sh",source:"/opt/sub2api-custom/sync-trigger.sh",type:"bind",read_only:true}],networks:{"sub2api-network":{}}},"extensions-self":{image:$ext,healthcheck:{test:["CMD","true"]},networks:{"sub2api-network":{}}},postgres:{},redis:{},"risk-control-postgres":{}},volumes:{sub2api_data:{},postgres_data:{},redis_data:{},risk_control_postgres_data:{}}}'
}

make_fake_tools() {
  local root="$1"
  mkdir -p "$root/bin"
  cat > "$root/bin/git" <<'EOF'
#!/usr/bin/env bash
set -e
printf 'git %s\n' "$*" >> "$FIXTURE_CALLS"
args=("$@")
for ((i=0; i<${#args[@]}; i++)); do
  if [[ "${args[$i]}" == worktree && "${args[$((i+1))]:-}" == add ]]; then
    destination="${args[$((i+3))]}"
    mkdir -p "$destination"
    cp -R "$FIXTURE_TARGET_SOURCE/." "$destination/"
    exit 0
  fi
  if [[ "${args[$i]}" == worktree && "${args[$((i+1))]:-}" == remove ]]; then
    rm -rf "${args[-1]}"
    exit 0
  fi
done
exit 0
EOF
  cat > "$root/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -e
printf 'docker %s\n' "$*" >> "$FIXTURE_CALLS"
case " $* " in
  *' compose '*' up '*|*' compose '*' down '*|*' compose '*' restart '*|*' compose '*' stop '*|*' compose '*' kill '*) exit 97 ;;
esac
if [[ "${1:-}" == pull ]]; then exit 0; fi
if [[ "${1:-}" == compose ]]; then
  env_file=''
  format=''
  args=("$@")
  for ((i=0; i<${#args[@]}; i++)); do
    [[ "${args[$i]}" != --env-file ]] || env_file="${args[$((i+1))]}"
    [[ "${args[$i]}" != --format ]] || format="${args[$((i+1))]}"
  done
  [[ -n "$env_file" ]] || exit 96
  if [[ "$format" == json ]]; then
    main="$(sed -n 's/^SUB2API_IMAGE=//p' "$env_file" | tail -n 1)"
    ext="$(sed -n 's/^EXTENSIONS_SELF_IMAGE=//p' "$env_file" | tail -n 1)"
    jq -n --arg main "$main" --arg ext "$ext" '{name:"deploy",services:{sub2api:{image:$main,healthcheck:{test:["CMD","true"]},volumes:[{target:"/app/data",source:"sub2api_data",type:"volume",read_only:false},{target:"/app/scripts/sync-upstream.sh",source:"/opt/sub2api-custom/sync-trigger.sh",type:"bind",read_only:true}],networks:{"sub2api-network":{}}},"extensions-self":{image:$ext,healthcheck:{test:["CMD","true"]},networks:{"sub2api-network":{}}},postgres:{},redis:{},"risk-control-postgres":{}},volumes:{sub2api_data:{},postgres_data:{},redis_data:{},risk_control_postgres_data:{}}}'
  fi
  exit 0
fi
if [[ "${1:-}" == inspect ]]; then printf '{}\n'; exit 0; fi
if [[ "${1:-} ${2:-}" == 'image ls' ]]; then printf 'image metadata\n'; exit 0; fi
if [[ "${1:-} ${2:-}" == 'image inspect' ]]; then
  reference="${3:-}"
  format=''
  args=("$@")
  for ((i=0; i<${#args[@]}; i++)); do
    [[ "${args[$i]}" != --format ]] || format="${args[$((i+1))]}"
  done
  revision="$TARGET_COMMIT"
  [[ ! -e "${FIXTURE_LABEL_DRIFT_FILE:-}" ]] || revision=ffffffffffffffffffffffffffffffffffffffff
  case "$format" in
    *Config.Labels*) jq -n --arg revision "$revision" --arg version "${FIXTURE_STABLE_VERSION:-0.1.163}" '{"org.opencontainers.image.revision":$revision,"org.opencontainers.image.version":$version,"org.opencontainers.image.source":"https://github.com/ListenCodes/sub2api"}' ;;
    *Architecture*) printf 'amd64\n' ;;
    *RepoDigests*) jq -n --arg reference "$reference" '[$reference]' ;;
    *) printf '{}\n' ;;
  esac
  exit 0
fi
if [[ "${1:-} ${2:-}" == 'image tag' ]]; then exit 0; fi
if [[ "${1:-}" == exec ]]; then
  [[ " $* " != *' pg_restore --list '* ]] || { cat >/dev/null; printf 'restore list\n'; exit 0; }
  [[ " $* " != *' pg_dump '* ]] || { printf 'database dump\n'; exit 0; }
  exit 1
fi
if [[ "${1:-}" == ps || "${1:-}" == images ]]; then printf 'runtime metadata\n'; exit 0; fi
exit 1
EOF
  cat > "$root/bin/flock" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$root/bin/git" "$root/bin/docker" "$root/bin/flock"
}

make_helpers() {
  local root="$1"
  cat > "$root/sync.sh" <<'EOF'
#!/usr/bin/env bash
set -e
source "$SUB2API_RELEASE_STATE_HELPER"
job_id="${2:-}"
case "$FIXTURE_SCENARIO" in
  official|official-docs-only) kind=official; stable=v0.1.164; stable_commit="$TARGET_STABLE_COMMIT" ;;
  custom) kind=custom; stable=v0.1.163; stable_commit="$CURRENT_STABLE_COMMIT" ;;
  combined) kind=combined; stable=v0.1.164; stable_commit="$TARGET_STABLE_COMMIT" ;;
  docs-only) kind=docs-only; stable=v0.1.163; stable_commit="$CURRENT_STABLE_COMMIT" ;;
  none) kind=none; stable=v0.1.163; stable_commit="$CURRENT_STABLE_COMMIT" ;;
  docs-conflict) kind=docs-only; stable=v0.1.163; stable_commit="$CURRENT_STABLE_COMMIT" ;;
  runtime-scope-conflict) kind=official; stable=v0.1.164; stable_commit="$TARGET_STABLE_COMMIT" ;;
  none-conflict) kind=none; stable=v0.1.164; stable_commit="$TARGET_STABLE_COMMIT" ;;
  env-drift|compose-drift|env-race|compose-race|target-custom-drift) kind=custom; stable=v0.1.163; stable_commit="$CURRENT_STABLE_COMMIT" ;;
  official-custom-conflict) kind=official; stable=v0.1.164; stable_commit="$TARGET_STABLE_COMMIT" ;;
  combined-custom-conflict) kind=combined; stable=v0.1.164; stable_commit="$TARGET_STABLE_COMMIT" ;;
esac
base="$CURRENT_COMMIT"
case "$FIXTURE_SCENARIO" in
  custom|combined|official-docs-only|docs-only|docs-conflict|runtime-scope-conflict|env-drift|compose-drift|env-race|compose-race|official-custom-conflict)
    base="$TARGET_COMMIT"
    ;;
esac
target="$TARGET_COMMIT"
[[ "$FIXTURE_SCENARIO" != none ]] || target="$CURRENT_COMMIT"
[[ "$FIXTURE_SCENARIO" != target-custom-drift ]] || target="$CURRENT_COMMIT"
if [[ "$FIXTURE_SCENARIO" == official && -e "$FIXTURE_PROMOTED_FILE" ]]; then
  base="$TARGET_COMMIT"
  target="$TARGET_COMMIT"
fi
integration=''
if [[ "$FIXTURE_SCENARIO" == official || "$FIXTURE_SCENARIO" == official-docs-only ]] && [[ ! -e "$FIXTURE_PROMOTED_FILE" ]]; then
  integration=integration/release-v0.1.164-fixture
fi
release_job_update "$job_id" resolving_target 'fixture target resolved' "$(jq -n --arg kind "$kind" --arg base "$base" --arg target "$target" --arg tag "$stable" --arg commit "$stable_commit" --arg integration "$integration" '{update_kind:$kind,base_commit:$base,target_commit:$target,release_tag:$tag,release_commit:$commit,integration_branch:$integration}')"
EOF
  cat > "$root/scope.sh" <<'EOF'
#!/usr/bin/env bash
[[ "$FIXTURE_SCENARIO" == docs-only || "$FIXTURE_SCENARIO" == runtime-scope-conflict ]] \
  && printf 'docs_only=true\n' || printf 'docs_only=false\n'
EOF
cat > "$root/wait.sh" <<'EOF'
#!/usr/bin/env bash
case "$FIXTURE_SCENARIO" in
  env-race) printf 'UNAUTHORIZED_REMOTE_GATE_DRIFT=1\n' >> "$SUB2API_ENV_FILE" ;;
  compose-race) printf '# unauthorized remote gate drift\n' >> "$SUB2API_COMPOSE_CUSTOM" ;;
esac
printf 'wait %s\n' "$*" >> "$FIXTURE_CALLS"
jq -cn --arg url 'https://github.com/ListenCodes/sub2api/actions/runs/1' '{
  ok:true,message:"all required GitHub Actions checks succeeded",error_code:"",
  failed_check:"",check_url:"",conclusion:"success",workflow_url:$url,
  production_changed:false
}'
EOF
  cat > "$root/verify.sh" <<EOF
#!/usr/bin/env bash
printf 'verify %s\n' "\$*" >> "\$FIXTURE_CALLS"
rm -f "\${FIXTURE_LABEL_DRIFT_FILE:-}"
printf 'main_digest=$MAIN_DIGEST\nextensions_digest=$EXT_DIGEST\n'
EOF
cat > "$root/promote.sh" <<'EOF'
#!/usr/bin/env bash
printf 'promote %s\n' "$*" >> "$FIXTURE_CALLS"
touch "$FIXTURE_PROMOTED_FILE"
EOF
  chmod +x "$root/sync.sh" "$root/scope.sh" "$root/wait.sh" "$root/verify.sh" "$root/promote.sh"
}

seed_case() {
  local scenario="$1" root="$TMP_DIR/$scenario" job_id="update-$scenario"
  mkdir -p "$root/data/release-ledger/releases" "$root/data/release-ledger/operations" \
    "$root/data/release-backups/current" "$root/repo/deploy" "$root/target-source/deploy" "$root/nginx"
  : > "$root/calls"
  printf 'services:\n  sub2api:\n    image: ${SUB2API_IMAGE}\n# production base\n' > "$root/repo/deploy/docker-compose.yml"
  printf 'services:\n  extensions-self:\n    image: ${EXTENSIONS_SELF_IMAGE}\n# production custom\n' > "$root/repo/deploy/docker-compose.custom.yml"
  printf 'SUB2API_IMAGE=ghcr.io/listencodes/sub2api-custom@%s\nEXTENSIONS_SELF_IMAGE=ghcr.io/listencodes/sub2api-extensions@%s\nKEEP=value\n' "$OLD_MAIN_DIGEST" "$OLD_EXT_DIGEST" > "$root/repo/deploy/.env"
  printf 'services:\n  sub2api:\n    image: ${SUB2API_IMAGE}\n# target base\n' > "$root/target-source/deploy/docker-compose.yml"
  printf 'services:\n  extensions-self:\n    image: ${EXTENSIONS_SELF_IMAGE}\n# target custom\n' > "$root/target-source/deploy/docker-compose.custom.yml"
  printf 'nginx\n' > "$root/nginx/sub2api.conf"
  printf 'certificate\n' > "$root/nginx/origin.crt"
  printf 'private key\n' > "$root/nginx/origin.key"

  base_hash="$(sha256sum "$root/repo/deploy/docker-compose.yml" | awk '{print $1}')"
  custom_hash="$(sha256sum "$root/repo/deploy/docker-compose.custom.yml" | awk '{print $1}')"
  env_hash="$(sha256sum "$root/repo/deploy/.env" | awk '{print $1}')"
  rendered_hash="$(fixture_compose_json "ghcr.io/listencodes/sub2api-custom@$OLD_MAIN_DIGEST" "ghcr.io/listencodes/sub2api-extensions@$OLD_EXT_DIGEST" | sha256sum | awk '{print $1}')"

  jq -n --arg release "$CURRENT_RELEASE_ID" --arg operation "$job_id" \
    '{schema_version:1,current_release_id:$release,custom_version_high_water:4,active_operation_id:$operation,updated_at:"2026-07-23T08:00:00Z"}' \
    > "$root/data/release-ledger/state.json"
  jq -n --arg release "$CURRENT_RELEASE_ID" --arg official_commit "$CURRENT_STABLE_COMMIT" --arg custom_commit "$CURRENT_COMMIT" \
    --arg main "$OLD_MAIN_DIGEST" --arg ext "$OLD_EXT_DIGEST" --arg backup "$root/data/release-backups/current" \
    --arg base_hash "$base_hash" --arg custom_hash "$custom_hash" --arg rendered_hash "$rendered_hash" --arg env_hash "$env_hash" \
    '{schema_version:1,release_id:$release,official_version:"v0.1.163",official_commit:$official_commit,custom_version:"v1.0.4",custom_version_sequence:4,custom_commit:$custom_commit,main_digest:$main,extensions_digest:$ext,base_compose_sha256:$base_hash,custom_compose_sha256:$custom_hash,rendered_compose_sha256:$rendered_hash,env_sha256:$env_hash,backup_dir:$backup,backup_manifest_sha256:("9"*64),published_at:"2026-07-23T08:00:00Z",source_kind:"custom",operation_id:"update-previous"}' \
    > "$root/data/release-ledger/releases/$CURRENT_RELEASE_ID.json"
  jq -n --arg production "$CURRENT_COMMIT" --arg stable_commit "$CURRENT_STABLE_COMMIT" --arg main "$OLD_MAIN_DIGEST" --arg ext "$OLD_EXT_DIGEST" \
    '{production_commit:$production,stable_release_tag:"v0.1.163",stable_release_commit:$stable_commit,main_digest:$main,extensions_digest:$ext,published_at:"2026-07-23T08:00:00Z",backup_dir:"/previous",release_id:"release-current",official_version:"v0.1.163",custom_version:"v1.0.4",custom_version_sequence:4}' \
    > "$root/data/release-state.json"

  target_custom="$CURRENT_COMMIT"
  case "$scenario" in
    custom|combined|official-docs-only|docs-only|docs-conflict|runtime-scope-conflict|env-drift|compose-drift|env-race|compose-race|official-custom-conflict|target-custom-drift)
      target_custom="$TARGET_COMMIT"
      ;;
  esac
  custom_docs_only=false
  [[ "$scenario" != official-docs-only ]] || custom_docs_only=true
  operation_metadata="$(jq -n --arg target "$target_custom" --argjson custom_docs_only "$custom_docs_only" '{action:"prepare",target_custom_commit:$target,custom_docs_only:$custom_docs_only}')"
  SUB2API_DATA_DIR="$root/data" SUB2API_RELEASE_OPERATIONS_DIR="$root/data/release-ledger/operations" \
    SUB2API_CURRENT_RELEASE_JOB_FILE="$root/data/release-current-job-id" \
    bash -c 'source "$1"; release_job_init "$2"; release_job_update "$2" resolving_target queued "$3"' _ \
      "$ROOT_DIR/deploy/ops/release-state.sh" "$job_id" "$operation_metadata"
  make_fake_tools "$root"
  make_helpers "$root"
  printf '%s\n' "$root"
}

invoke_prepare() {
  local root="$1" scenario="$2" job_id="$3"
  local stable_version=0.1.163
  [[ "$scenario" != official && "$scenario" != official-docs-only && "$scenario" != combined && "$scenario" != official-custom-conflict \
    && "$scenario" != combined-custom-conflict && "$scenario" != runtime-scope-conflict && "$scenario" != none-conflict ]] \
    || stable_version=0.1.164
  PATH="$root/bin:$PATH" FIXTURE_CALLS="$root/calls" FIXTURE_TARGET_SOURCE="$root/target-source" FIXTURE_SCENARIO="$scenario" \
    FIXTURE_LABEL_DRIFT_FILE="$root/label-drift" FIXTURE_PROMOTED_FILE="$root/promoted" \
    FIXTURE_STABLE_VERSION="$stable_version" \
    CURRENT_COMMIT="$CURRENT_COMMIT" TARGET_COMMIT="$TARGET_COMMIT" \
    CURRENT_STABLE_COMMIT="$CURRENT_STABLE_COMMIT" TARGET_STABLE_COMMIT="$TARGET_STABLE_COMMIT" \
    SUB2API_DATA_DIR="$root/data" SUB2API_REPO="$root/repo" SUB2API_ENV_FILE="$root/repo/deploy/.env" \
    SUB2API_COMPOSE_BASE="$root/repo/deploy/docker-compose.yml" SUB2API_COMPOSE_CUSTOM="$root/repo/deploy/docker-compose.custom.yml" \
    SUB2API_NGINX_VHOST="$root/nginx/sub2api.conf" SUB2API_ORIGIN_CERT="$root/nginx/origin.crt" SUB2API_ORIGIN_KEY="$root/nginx/origin.key" \
    SUB2API_RELEASE_BACKUP_ROOT="$root/data/release-backups" SUB2API_BACKUP_ROOT="$root/data/release-backups" \
    SUB2API_LEGACY_RELEASE_BACKUP_ROOT="$root/data/legacy-release-backups" \
    SUB2API_PREPARED_ROOT="$root/data/release-prepared" \
    SUB2API_RELEASE_LEDGER_ROOT="$root/data/release-ledger" SUB2API_RELEASE_OPERATIONS_DIR="$root/data/release-ledger/operations" \
    SUB2API_RELEASE_LEDGER_LOCK_FILE="$root/data/release.lock" \
    SUB2API_CURRENT_RELEASE_JOB_FILE="$root/data/release-current-job-id" SUB2API_RELEASE_STATE_FILE="$root/data/release-state.json" \
    SUB2API_RELEASE_STATE_HELPER="$ROOT_DIR/deploy/ops/release-state.sh" SUB2API_RELEASE_COMMON_HELPER="$ROOT_DIR/deploy/ops/release-common.sh" \
    SUB2API_RELEASE_LEDGER_HELPER="$ROOT_DIR/deploy/ops/release-ledger.sh" SUB2API_SYNC_SCRIPT="$root/sync.sh" \
    SUB2API_SCOPE_SCRIPT="$root/scope.sh" SUB2API_WAIT_ACTIONS_SCRIPT="$root/wait.sh" SUB2API_VERIFY_IMAGES_SCRIPT="$root/verify.sh" \
    SUB2API_PROMOTE_SCRIPT="$root/promote.sh" SUB2API_SYNC_PUBLISH_LOG="$root/release.log" \
    "$ROOT_DIR/deploy/ops/prepare-release.sh" --job-id "$job_id"
}

run_case() {
  local scenario="$1" root job_id job_file state_hash record_hash projection_hash source_hash exit_code
  root="$(seed_case "$scenario")"
  job_id="update-$scenario"
  job_file="$root/data/release-ledger/operations/$job_id.json"
  state_hash="$(sha256sum "$root/data/release-ledger/state.json")"
  record_hash="$(sha256sum "$root/data/release-ledger/releases/$CURRENT_RELEASE_ID.json")"
  projection_hash="$(sha256sum "$root/data/release-state.json")"
  source_hash="$(sha256sum "$root/repo/deploy/docker-compose.yml" "$root/repo/deploy/docker-compose.custom.yml" "$root/repo/deploy/.env")"

  set +e
  invoke_prepare "$root" "$scenario" "$job_id"
  exit_code=$?
  set -e
  [[ "$exit_code" -eq 0 ]] || fail "$scenario prepare exited $exit_code"

  assert_eq "$record_hash" "$(sha256sum "$root/data/release-ledger/releases/$CURRENT_RELEASE_ID.json")" "$scenario changed current record"
  assert_eq "$projection_hash" "$(sha256sum "$root/data/release-state.json")" "$scenario changed compatibility projection"
  assert_eq "$source_hash" "$(sha256sum "$root/repo/deploy/docker-compose.yml" "$root/repo/deploy/docker-compose.custom.yml" "$root/repo/deploy/.env")" "$scenario changed production source"
  ! grep -Eq 'docker compose .*[[:space:]](up|down|restart|stop|kill)([[:space:]]|$)' "$root/calls" || fail "$scenario changed container lifecycle"

  if [[ "$scenario" == docs-only || "$scenario" == none ]]; then
    [[ "$(jq -r '.status' "$job_file")" == success ]] || fail "$scenario did not settle without preparation"
    assert_eq "$CURRENT_RELEASE_ID" "$(jq -r '.current_release_id' "$root/data/release-ledger/state.json")" "$scenario changed release pointer"
    assert_eq 4 "$(jq -r '.custom_version_high_water' "$root/data/release-ledger/state.json")" "$scenario changed custom high-water"
    assert_eq null "$(jq -r '.active_operation_id' "$root/data/release-ledger/state.json")" "$scenario retained the active operation"
    [[ "$(jq -r '.prepared_manifest // empty' "$job_file")" == '' ]] || fail "$scenario created a prepared manifest"
    [[ -z "$(find "$root/data/release-backups" -mindepth 1 -maxdepth 1 -type d ! -name current -print -quit)" ]] || fail "$scenario created a backup"
    return
  fi

  assert_eq "$state_hash" "$(sha256sum "$root/data/release-ledger/state.json")" "$scenario changed ledger state"

  manifest="$(jq -r '.prepared_manifest' "$job_file")"
  [[ -s "$manifest" ]] || fail "$scenario manifest is missing"
  SUB2API_DATA_DIR="$root/data" SUB2API_RELEASE_BACKUP_ROOT="$root/data/release-backups" \
    SUB2API_LEGACY_RELEASE_BACKUP_ROOT="$root/data/legacy-release-backups" SUB2API_PREPARED_ROOT="$root/data/release-prepared" \
    bash -c 'source "$1"; release_manifest_valid "$2"' _ "$ROOT_DIR/deploy/ops/release-common.sh" "$job_id" \
    || fail "$scenario shared manifest validator rejected the prepared artifact"
  assert_eq update "$(jq -r '.operation_kind' "$manifest")" "$scenario operation kind mismatch"
  assert_eq "$CURRENT_RELEASE_ID" "$(jq -r '.base_release_id' "$manifest")" "$scenario base release mismatch"
  assert_eq 4 "$(jq -r '.base_custom_high_water' "$manifest")" "$scenario base high-water mismatch"
  assert_eq "$TARGET_COMMIT" "$(jq -r '.target_commit' "$manifest")" "$scenario target commit mismatch"
  expected_custom_commit="$CURRENT_COMMIT"
  [[ "$scenario" == official ]] || expected_custom_commit="$TARGET_COMMIT"
  assert_eq "$expected_custom_commit" "$(jq -r '.target_custom_commit' "$manifest")" "$scenario target custom commit mismatch"
  assert_eq "$MAIN_DIGEST" "$(jq -r '.main_digest' "$manifest")" "$scenario main digest mismatch"
  assert_eq "$EXT_DIGEST" "$(jq -r '.extensions_digest' "$manifest")" "$scenario extensions digest mismatch"
  [[ "$(jq -r '.target_release_id' "$manifest")" =~ ^release-candidate- ]] || fail "$scenario target release id is not opaque"
  for field in current_base_compose_sha256 current_custom_compose_sha256 target_base_compose_sha256 target_custom_compose_sha256 target_rendered_compose_sha256 target_env_sha256 backup_manifest_sha256; do
    [[ "$(jq -r --arg field "$field" '.[$field]' "$manifest")" =~ ^[0-9a-f]{64}$ ]] || fail "$scenario $field is invalid"
  done
  jq -e '.prepared_at | fromdateiso8601 > 0' "$manifest" >/dev/null || fail "$scenario prepared_at is invalid"
  jq -e '.expires_at | fromdateiso8601 > 0' "$manifest" >/dev/null || fail "$scenario expires_at is invalid"
  backup_dir="$(jq -r '.backup_dir' "$manifest")"
  [[ -s "$backup_dir/target/docker-compose.yml" && -s "$backup_dir/target/docker-compose.custom.yml" && -s "$backup_dir/target/.env" && -s "$backup_dir/target/rendered-compose.json" ]] || fail "$scenario target artifacts are incomplete"
  grep -q 'target base' "$backup_dir/target/docker-compose.yml" || fail "$scenario rendered the production base Compose"
  grep -q "SUB2API_IMAGE=ghcr.io/listencodes/sub2api-custom@$MAIN_DIGEST" "$backup_dir/target/.env" || fail "$scenario target env did not pin main digest"
  grep -q "EXTENSIONS_SELF_IMAGE=ghcr.io/listencodes/sub2api-extensions@$EXT_DIGEST" "$backup_dir/target/.env" || fail "$scenario target env did not pin extensions digest"
  SUB2API_DATA_DIR="$root/data" SUB2API_RELEASE_BACKUP_ROOT="$root/data/release-backups" \
    bash -c 'source "$1"; ledger_validate_backup_contract "$2"' _ "$ROOT_DIR/deploy/ops/release-ledger.sh" "$backup_dir" \
    || fail "$scenario backup contract is invalid"
  grep -q 'git .*worktree add --detach' "$root/calls" || fail "$scenario did not stage the target commit"

  case "$scenario" in
    official|official-docs-only)
      assert_eq v0.1.164 "$(jq -r '.target_official_version' "$manifest")" 'official target version mismatch'
      assert_eq v1.0.4 "$(jq -r '.target_custom_version' "$manifest")" 'official-only changed custom version'
      assert_eq 4 "$(jq -r '.proposed_custom_sequence' "$manifest")" 'official-only changed custom sequence'
      assert_eq false "$(jq -r '.advances_custom_version' "$manifest")" 'official-only advances custom version'
      ;;
    custom)
      assert_eq v0.1.163 "$(jq -r '.target_official_version' "$manifest")" 'custom target official version mismatch'
      assert_eq v1.0.5 "$(jq -r '.target_custom_version' "$manifest")" 'custom target version mismatch'
      assert_eq true "$(jq -r '.advances_custom_version' "$manifest")" 'custom did not advance version'
      ;;
    combined)
      assert_eq v0.1.164 "$(jq -r '.target_official_version' "$manifest")" 'combined target official version mismatch'
      assert_eq v1.0.5 "$(jq -r '.target_custom_version' "$manifest")" 'combined target version mismatch'
      assert_eq true "$(jq -r '.advances_custom_version' "$manifest")" 'combined did not advance version'
      ;;
  esac
  if [[ "$scenario" == custom ]]; then
    CUSTOM_ROOT="$root"
    CUSTOM_JOB_FILE="$job_file"
    CUSTOM_FIRST_BACKUP="$backup_dir"
  fi
  if [[ "$scenario" == official ]]; then
    OFFICIAL_ROOT="$root"
    OFFICIAL_JOB_FILE="$job_file"
    OFFICIAL_FIRST_BACKUP="$backup_dir"
  fi
}

prepare_scenarios=(official official-docs-only custom combined docs-only none)
[[ -z "${FIXTURE_ONLY:-}" ]] || prepare_scenarios=("$FIXTURE_ONLY")
for scenario in "${prepare_scenarios[@]}"; do
  run_case "$scenario"
done

if [[ -n "${FIXTURE_ONLY:-}" ]]; then
  printf 'prepare-release-ledger=%s=PASS\n' "$FIXTURE_ONLY"
  exit 0
fi

official_gate_call_count() {
  grep -Ec '^(wait |verify |docker pull )' "$OFFICIAL_ROOT/calls" || true
}

first_official_gate_count="$(official_gate_call_count)"
SUB2API_DATA_DIR="$OFFICIAL_ROOT/data" SUB2API_RELEASE_OPERATIONS_DIR="$OFFICIAL_ROOT/data/release-ledger/operations" \
  bash -c 'source "$1"; release_job_update update-official expired expired "{}"' _ "$ROOT_DIR/deploy/ops/release-state.sh"
set +e
invoke_prepare "$OFFICIAL_ROOT" official update-official
official_retry_exit=$?
set -e
[[ "$official_retry_exit" -eq 0 ]] || fail 'promoted official candidate could not be prepared again'
assert_eq "$first_official_gate_count" "$(official_gate_call_count)" 'promoted official candidate did not reuse exact verified evidence'
OFFICIAL_SECOND_BACKUP="$(jq -r '.backup_dir' "$OFFICIAL_JOB_FILE")"
[[ "$OFFICIAL_SECOND_BACKUP" != "$OFFICIAL_FIRST_BACKUP" ]] || fail 'promoted official candidate reused a stale backup directory'
assert_eq "$CURRENT_COMMIT" "$(jq -r '.target_custom_commit' "$(jq -r '.prepared_manifest' "$OFFICIAL_JOB_FILE")")" \
  'promoted official retry changed the detected custom target'

run_refusal_case() {
  local scenario="$1" root job_id before_state before_projection exit_code
  root="$(seed_case "$scenario")"
  job_id="update-$scenario"
  case "$scenario" in
    env-drift) printf 'UNAUTHORIZED_DRIFT=1\n' >> "$root/repo/deploy/.env" ;;
    compose-drift) printf '# unauthorized drift\n' >> "$root/repo/deploy/docker-compose.custom.yml" ;;
  esac
  before_state="$(sha256sum "$root/data/release-ledger/state.json")"
  before_projection="$(sha256sum "$root/data/release-state.json")"
  set +e
  invoke_prepare "$root" "$scenario" "$job_id" >/dev/null 2>&1
  exit_code=$?
  set -e
  [[ "$exit_code" -ne 0 ]] || fail "$scenario was not rejected"
  assert_eq "$CURRENT_RELEASE_ID" "$(jq -r '.current_release_id' "$root/data/release-ledger/state.json")" "$scenario changed release pointer"
  assert_eq 4 "$(jq -r '.custom_version_high_water' "$root/data/release-ledger/state.json")" "$scenario changed custom high-water"
  assert_eq null "$(jq -r '.active_operation_id' "$root/data/release-ledger/state.json")" "$scenario retained the failed active operation"
  assert_eq "$before_projection" "$(sha256sum "$root/data/release-state.json")" "$scenario changed compatibility projection"
  if [[ "$scenario" == env-race || "$scenario" == compose-race ]]; then
    grep -q '^wait ' "$root/calls" || fail "$scenario did not reach the injected remote gate"
    [[ "$(jq -r '.prepared_manifest // empty' "$root/data/release-ledger/operations/$job_id.json")" == '' ]] \
      || fail "$scenario created a prepared manifest from drifted production files"
  else
    [[ -z "$(grep -E '^(wait |verify |docker pull )' "$root/calls" || true)" ]] || fail "$scenario reached remote gates"
    [[ -z "$(find "$root/data/release-backups" -mindepth 1 -maxdepth 1 -type d ! -name current -print -quit)" ]] || fail "$scenario created a backup"
  fi
}

for scenario in env-drift compose-drift docs-conflict runtime-scope-conflict none-conflict \
  official-custom-conflict combined-custom-conflict target-custom-drift env-race compose-race; do
  run_refusal_case "$scenario"
done

rerun_custom_case() {
  local root="$CUSTOM_ROOT" job_id=update-custom
  invoke_prepare "$root" custom "$job_id"
}

gate_call_count() {
  grep -Ec '^(wait |verify |docker pull )' "$CUSTOM_ROOT/calls" || true
}

first_gate_count="$(gate_call_count)"
SUB2API_DATA_DIR="$CUSTOM_ROOT/data" SUB2API_RELEASE_OPERATIONS_DIR="$CUSTOM_ROOT/data/release-ledger/operations" \
  bash -c 'source "$1"; release_job_update update-custom expired expired "{}"' _ "$ROOT_DIR/deploy/ops/release-state.sh"
rerun_custom_case
second_gate_count="$(gate_call_count)"
assert_eq "$first_gate_count" "$second_gate_count" 'exact cached evidence was not reused'
CUSTOM_SECOND_BACKUP="$(jq -r '.backup_dir' "$CUSTOM_JOB_FILE")"
[[ "$CUSTOM_SECOND_BACKUP" != "$CUSTOM_FIRST_BACKUP" ]] || fail 'cached preparation reused a stale backup directory'
[[ -s "$CUSTOM_SECOND_BACKUP/SHA256SUMS" ]] || fail 'cached preparation did not create a fresh complete backup'

touch "$CUSTOM_ROOT/label-drift"
SUB2API_DATA_DIR="$CUSTOM_ROOT/data" SUB2API_RELEASE_OPERATIONS_DIR="$CUSTOM_ROOT/data/release-ledger/operations" \
  bash -c 'source "$1"; release_job_update update-custom expired expired "{}"' _ "$ROOT_DIR/deploy/ops/release-state.sh"
rerun_custom_case
label_gate_count="$(gate_call_count)"
[[ "$label_gate_count" -gt "$second_gate_count" ]] || fail 'drifted local OCI labels reused cached evidence'

SUB2API_DATA_DIR="$CUSTOM_ROOT/data" SUB2API_RELEASE_OPERATIONS_DIR="$CUSTOM_ROOT/data/release-ledger/operations" \
  bash -c 'source "$1"; release_job_update update-custom expired expired "{\"stable_release_commit\":\"ffffffffffffffffffffffffffffffffffffffff\"}"' _ \
    "$ROOT_DIR/deploy/ops/release-state.sh"
rerun_custom_case
third_gate_count="$(gate_call_count)"
[[ "$third_gate_count" -gt "$label_gate_count" ]] || fail 'drifted cached evidence bypassed Actions and image verification'
CUSTOM_THIRD_BACKUP="$(jq -r '.backup_dir' "$CUSTOM_JOB_FILE")"
[[ "$CUSTOM_THIRD_BACKUP" != "$CUSTOM_SECOND_BACKUP" ]] || fail 'drifted reprepare reused a stale backup directory'

printf 'prepare-release-ledger=PASS\n'
