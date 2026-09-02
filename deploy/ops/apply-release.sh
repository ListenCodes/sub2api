#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
COMMON_HELPER="${SUB2API_RELEASE_COMMON_HELPER:-$SCRIPT_DIR/release-common.sh}"
LEDGER_HELPER="${SUB2API_RELEASE_LEDGER_HELPER:-$SCRIPT_DIR/release-ledger.sh}"
REPO="${SUB2API_REPO:-/root/sub2api}"
BRANCH="${SUB2API_BRANCH:-custom-release}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"
INTERNAL_HEALTH_URL="${SUB2API_INTERNAL_HEALTH_URL:-http://127.0.0.1:8081/health}"
PUBLIC_HEALTH_URL="${SUB2API_PUBLIC_HEALTH_URL:-https://sub.ailisten.top/health}"
ADMIN_HEALTH_URL="${SUB2API_ADMIN_HEALTH_URL:-http://127.0.0.1:8081/admin}"
EXTENSION_ROUTE_URL="${SUB2API_EXTENSION_ROUTE_URL:-http://127.0.0.1:8081/admin/extensions/account-monitor}"
HOMEPAGE_HEALTH_URL="${SUB2API_HOMEPAGE_HEALTH_URL:-http://127.0.0.1:8081/api/v1/extensions-self/homepage/}"
HOMEPAGE_GROUPS_HEALTH_URL="${SUB2API_HOMEPAGE_GROUPS_HEALTH_URL:-http://127.0.0.1:8081/api/v1/extensions-self/homepage/api/public-groups}"
LOG="${SUB2API_SYNC_PUBLISH_LOG:-/var/log/sub2api-release.log}"
HEALTH_WAIT_TIMEOUT_SECONDS="${SUB2API_HEALTH_WAIT_TIMEOUT_SECONDS:-180}"
HEALTH_WAIT_INTERVAL_SECONDS="${SUB2API_HEALTH_WAIT_INTERVAL_SECONDS:-2}"

source "$STATE_HELPER"
source "$COMMON_HELPER"
source "$LEDGER_HELPER"

JOB_ID="${SUB2API_JOB_ID:-${2:-}}"
[[ "${1:-}" == --job-id && -n "$JOB_ID" ]] || { printf 'usage: apply-release.sh --job-id <job-id>\n' >&2; exit 2; }
release_valid_job_id "$JOB_ID" || { printf 'invalid job id\n' >&2; exit 2; }
[[ "$JOB_ID" == update-* ]] || { printf 'apply-release accepts update operations only\n' >&2; exit 2; }
JOB_FILE="$(release_job_path "$JOB_ID")"
[[ -r "$JOB_FILE" ]] || { printf 'release operation is missing\n' >&2; exit 1; }
[[ "$(jq -r '.operation_kind // empty' "$JOB_FILE")" == update && "$(jq -r '.action // empty' "$JOB_FILE")" == apply ]] \
  || { release_job_fail "$JOB_ID" LEGACY_SINGLE_PHASE_UNSUPPORTED 'Legacy or non-update apply operation rejected'; exit 1; }

mkdir -p "$DATA_DIR" "$(dirname "$LOG")"
touch "$LOG"

fail_before_mutation() {
  local message="$1" code="${2:-APPLY_VALIDATION_FAILED}" status="${3:-drifted}"
  local metadata
  metadata="$(jq -n --arg code "$code" '{error_code:$code,published:false,production_changed:false,rollback:{attempted:false,succeeded:false,message:""}}')"
  if ! ledger_settle_pre_mutation_failure "$JOB_ID" "$status" "$message" "$metadata"; then
    printf '%s\n' "$message; release ledger refused terminal settlement" >&2
    exit 1
  fi
  printf '%s\n' "$message" >&2
  exit 1
}

operation_status="$(jq -r '.status // empty' "$JOB_FILE")"
if release_terminal_status "$operation_status" && [[ "$operation_status" != success ]]; then
  if ledger_recover_pre_mutation_terminal "$JOB_ID"; then
    printf 'release operation is already terminal: %s\n' "$operation_status" >&2
  else
    printf 'terminal release operation contradicts the active ledger state\n' >&2
  fi
  exit 1
fi

manifest="$(release_manifest_path "$JOB_ID")"
manifest_check=0
manifest_expired=false
release_manifest_valid "$JOB_ID" || manifest_check=$?
if [[ "$manifest_check" -eq 2 ]]; then
  manifest_expired=true
elif [[ "$manifest_check" -ne 0 ]]; then
  fail_before_mutation 'Prepared manifest is missing, corrupt, or inconsistent' PREPARED_MANIFEST_INVALID failed
fi

BASE_RELEASE_ID="$(jq -r '.base_release_id' "$manifest")"
BASE_CUSTOM_HIGH_WATER="$(jq -r '.base_custom_high_water' "$manifest")"
TARGET_RELEASE_ID="$(jq -r '.target_release_id' "$manifest")"
SOURCE_COMMIT="$(jq -r '.source_commit' "$manifest")"
TARGET_COMMIT="$(jq -r '.target_commit' "$manifest")"
TARGET_CUSTOM_COMMIT="$(jq -r '.target_custom_commit' "$manifest")"
UPDATE_KIND="$(jq -r '.update_kind' "$manifest")"
IDENTITY_TRANSITION="$(jq -r '.identity_transition // empty' "$manifest")"
CURRENT_OFFICIAL_VERSION="$(jq -r '.current_official_version' "$manifest")"
TARGET_OFFICIAL_VERSION="$(jq -r '.target_official_version' "$manifest")"
TARGET_CUSTOM_VERSION="$(jq -r '.target_custom_version' "$manifest")"
PROPOSED_CUSTOM_SEQUENCE="$(jq -r '.proposed_custom_sequence' "$manifest")"
ADVANCES_CUSTOM_VERSION="$(jq -r '.advances_custom_version' "$manifest")"
MAIN_DIGEST="$(jq -r '.main_digest' "$manifest")"
EXTENSIONS_DIGEST="$(jq -r '.extensions_digest' "$manifest")"
CURRENT_MAIN_DIGEST="$(jq -r '.current_main_digest' "$manifest")"
CURRENT_EXTENSIONS_DIGEST="$(jq -r '.current_extensions_digest' "$manifest")"
BACKUP_DIR="$(jq -r '.backup_dir' "$manifest")"
TARGET_DIR="$BACKUP_DIR/target"
STATE_PATH="$(ledger_state_path)"
BASE_RECORD_PATH="$(ledger_release_path "$BASE_RELEASE_ID")"
TARGET_RECORD_PATH="$(ledger_release_path "$TARGET_RELEASE_ID")"

ledger_validate_state "$STATE_PATH" || fail_before_mutation 'release ledger state is invalid' LEDGER_INCONSISTENT failed
ledger_validate_release "$BASE_RECORD_PATH" || fail_before_mutation 'base release record is invalid' LEDGER_INCONSISTENT failed
BASE_RECORD="$(cat "$BASE_RECORD_PATH")"
BASE_PROJECTION="$(ledger_projection_for_release "$BASE_RECORD")" \
  || fail_before_mutation 'base compatibility projection cannot be constructed' LEDGER_INCONSISTENT failed
ledger_validate_backup_contract "$BACKUP_DIR" \
  || fail_before_mutation 'prepared backup or target artifact contract drifted' BACKUP_DRIFT

wait_container_healthy() {
  local container="$1" deadline status
  deadline=$((SECONDS + HEALTH_WAIT_TIMEOUT_SECONDS))
  while (( SECONDS < deadline )); do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container" 2>/dev/null || true)"
    case "$status" in
      healthy) return 0 ;;
      unhealthy|exited|dead) return 1 ;;
    esac
    sleep "$HEALTH_WAIT_INTERVAL_SECONDS"
  done
  return 1
}

check_data_quality() {
  local rendered="$1" enabled secret timestamp nonce signature quality
  [[ -r "$rendered" ]] || return 1
  enabled="$(jq -r '.services["extensions-self"].environment.ACCOUNT_MONITOR_ENABLED // "false" | ascii_downcase' "$rendered")"
  [[ "$enabled" == false ]] && return 0
  [[ "$enabled" == true ]] || return 1
  secret="$(jq -r '.services["extensions-self"].environment.RISK_CONTROL_INTERNAL_SECRET // empty' "$rendered")"
  [[ -n "$secret" ]] || return 1
  timestamp="$(date +%s)"
  nonce="apply-$JOB_ID-$timestamp"
  signature="$(MONITOR_SECRET="$secret" MONITOR_TIMESTAMP="$timestamp" MONITOR_NONCE="$nonce" python3 -c '
import hashlib, hmac, os
message = (os.environ["MONITOR_TIMESTAMP"] + "\n" + os.environ["MONITOR_NONCE"] + "\n").encode()
print(hmac.new(os.environ["MONITOR_SECRET"].encode(), message, hashlib.sha256).hexdigest())
')" || return 1
  quality="$(docker exec extensions-self wget -qO- -T 10 \
    --header="X-Risk-Timestamp: $timestamp" --header="X-Risk-Nonce: $nonce" \
    --header="X-Risk-Signature: $signature" --header='X-Risk-Actor-ID: 1' \
    http://extensions-self:8090/api/v1/admin/account-monitor/data-quality)" || return 1
  jq -e '.source_connected == true and .missing_group_requests == 0 and (.data_as_of | type == "string" and length > 0)' <<< "$quality" >/dev/null
}

refresh_account_monitor_source_views() {
  local source_sql="${SUB2API_ACCOUNT_MONITOR_SOURCE_SQL:-$REPO/extensions-self/account-monitor/sql/main_source_views.sql}"
  [[ -r "$source_sql" ]] || return 1
  docker exec -i sub2api-postgres sh -ec '
    database_user="${POSTGRES_USER:-postgres}"
    database_name="${POSTGRES_DB:-$database_user}"
    exec psql -X -U "$database_user" -d "$database_name" --single-transaction -v ON_ERROR_STOP=1
  ' < "$source_sql"
}

run_complete_health() {
  local rendered="$1" container
  docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" ps --status running >/dev/null \
    || return 1
  for container in extensions-self sub2api sub2api-postgres sub2api-redis risk-control-postgres; do
    wait_container_healthy "$container" || return 1
  done
  if [[ "${SUB2API_SKIP_EXTERNAL_HEALTH_CHECKS:-0}" != 1 ]]; then
    curl -fsS --max-time 15 "$INTERNAL_HEALTH_URL" >/dev/null || return 1
    docker exec extensions-self wget -qO- -T 5 http://extensions-self:8090/healthz >/dev/null || return 1
    curl -fsS --max-time 15 "$HOMEPAGE_HEALTH_URL" >/dev/null || return 1
    curl -fsS --max-time 15 "$HOMEPAGE_GROUPS_HEALTH_URL" >/dev/null || return 1
    curl -fsS --max-time 15 "$PUBLIC_HEALTH_URL" >/dev/null || return 1
    curl -fsS --max-time 15 "$ADMIN_HEALTH_URL" >/dev/null || return 1
    curl -fsS --max-time 15 "$EXTENSION_ROUTE_URL" >/dev/null || return 1
    check_data_quality "$rendered" || return 1
  fi
}

container_env_value() {
  local container="$1" key="$2"
  docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$container" \
    | awk -v key="$key" 'index($0,key "=")==1 {count++; value=substr($0,length(key)+2)} END {if (count!=1) exit 1; print value}'
}

validate_identity_runtime() {
	local key expected actual health expected_enabled expected_admin expected_geo_source expected_composite_enforcement
	[[ "$UPDATE_KIND" == identity-config ]] || return 0
	if [[ "$IDENTITY_TRANSITION" != stage0-safe-reset ]]; then
		expected="$(awk -F= '$1=="SERVER_TRUSTED_PROXIES" {value=substr($0,length($1)+2)} END {print value}' "$TARGET_DIR/.env")"
		[[ "$expected" =~ ^[^,]+/(32|128),173\.245\.48\.0/20, ]] || return 1
		actual="$(container_env_value sub2api SERVER_TRUSTED_PROXIES)" || return 1
		[[ "$actual" == "$expected" ]] || return 1
	fi
  for key in RISK_IDENTITY_V2_ENABLED RISK_IDENTITY_IP_COLLECTION_ENABLED RISK_IDENTITY_DEVICE_COLLECTION_ENABLED RISK_IDENTITY_COMPOSITE_ENFORCEMENT_ENABLED RISK_IDENTITY_DELIVERY_ENABLED RISK_IDENTITY_TRUST_CLOUDFLARE_HEADERS; do
    expected="$(awk -F= -v key="$key" '$1==key {value=substr($0,length(key)+2)} END {print value}' "$TARGET_DIR/.env")"
    [[ "$expected" == true || "$expected" == false ]] || return 1
    actual="$(container_env_value sub2api "$key")" || return 1
    [[ "$actual" == "$expected" ]] || return 1
  done
  for key in RISK_IDENTITY_V2_ENABLED RISK_IDENTITY_IP_COLLECTION_ENABLED RISK_IDENTITY_DEVICE_COLLECTION_ENABLED \
    RISK_IDENTITY_ADMIN_ENABLED RISK_IDENTITY_RULES_ENABLED RISK_IDENTITY_IP_RULES_ENABLED \
	RISK_IDENTITY_DEVICE_RULES_ENABLED RISK_IDENTITY_COMPOSITE_RULES_ENABLED RISK_IDENTITY_COMPOSITE_ENFORCEMENT_ENABLED \
	RISK_IDENTITY_CURRENT_SCORE_ENABLED RISK_IDENTITY_CASES_ENABLED RISK_IDENTITY_EXPLAIN_ENABLED RISK_IDENTITY_DELIVERY_ENABLED; do
    expected="$(awk -F= -v key="$key" '$1==key {value=substr($0,length(key)+2)} END {print value}' "$TARGET_DIR/.env")"
    [[ "$expected" == true || "$expected" == false ]] || return 1
    actual="$(container_env_value extensions-self "$key")" || return 1
    [[ "$actual" == "$expected" ]] || return 1
  done
	expected_geo_source="$(awk -F= '$1=="RISK_IDENTITY_GEO_SOURCE" {value=substr($0,length($1)+2)} END {print value}' "$TARGET_DIR/.env")"
	[[ "$expected_geo_source" == cloudflare_or_local || "$expected_geo_source" == cloudflare_verified ]] || return 1
	actual="$(container_env_value extensions-self RISK_IDENTITY_GEO_SOURCE)" || return 1
	[[ "$actual" == "$expected_geo_source" ]] || return 1
  health="$(docker exec extensions-self wget -qO- -T 5 http://extensions-self:8090/healthz)" || return 1
  expected_enabled="$(awk -F= '$1=="RISK_IDENTITY_V2_ENABLED" {value=$2} END {print value}' "$TARGET_DIR/.env")"
  expected_admin="$(awk -F= '$1=="RISK_IDENTITY_ADMIN_ENABLED" {value=$2} END {print value}' "$TARGET_DIR/.env")"
	expected_composite_enforcement="$(awk -F= '$1=="RISK_IDENTITY_COMPOSITE_ENFORCEMENT_ENABLED" {value=$2} END {print value}' "$TARGET_DIR/.env")"
	jq -e --argjson enabled "$expected_enabled" --argjson admin "$expected_admin" --argjson composite_enforcement "$expected_composite_enforcement" --arg geo_source "$expected_geo_source" '
    .identity.enabled == $enabled and .identity.admin_enabled == $admin
	and .identity.mode == (if $composite_enforcement then "enforce" else "shadow" end)
	and .identity.features.composite_enforcement == $composite_enforcement
	and .identity.schema == "v2"
	and .identity.geo_source == $geo_source
	' <<< "$health" >/dev/null
	if [[ "$IDENTITY_TRANSITION" == stage3-rules || "$IDENTITY_TRANSITION" == stage4-geo || "$IDENTITY_TRANSITION" == stage5-composite-enforcement ]]; then
		jq -e '
			.identity.features.current_score and .identity.features.cases
			and .identity.features.explain and .identity.features.delivery
			and .identity.configured_rule_count >= 5
			and .identity.domains.ip == "healthy"
			and .identity.domains.device == "healthy"
			and .identity.domains.composite == "healthy"
			and .identity.processing.pending == 0
			and .identity.processing.retry == 0
			and .identity.processing.failed == 0
			and .identity.delivery.sources > 0
			and .identity.delivery.gap_sources == 0
			and .identity.delivery.stale_sources == 0
			and .identity.delivery.queue_depth == 0
			and .identity.delivery.dropped == 0
			and .identity.delivery.failed == 0
		' <<< "$health" >/dev/null || return 1
		jq -e '.identity.effective_rule_count >= 5' <<< "$health" >/dev/null || return 1
	fi
	if [[ "$IDENTITY_TRANSITION" == stage5-composite-enforcement ]]; then
		jq -e '.identity.mode == "enforce" and .identity.features.composite_enforcement' <<< "$health" >/dev/null || return 1
	fi
	if [[ "$IDENTITY_TRANSITION" == stage0-safe-reset ]]; then
		jq -e '
			.identity.domains.ip == "disabled"
			and .identity.domains.device == "disabled"
			and .identity.domains.composite == "disabled"
			and .identity.effective_rule_count == 0
			and (.identity.features.current_score | not)
			and (.identity.features.cases | not)
			and (.identity.features.explain | not)
			and (.identity.features.delivery | not)
			and (.identity.features.composite_enforcement | not)
		' <<< "$health" >/dev/null || return 1
	fi
}

validate_identity_pre_switch() {
  local health shadow_until
  [[ "$UPDATE_KIND" == identity-config ]] || return 0
  [[ "$IDENTITY_TRANSITION" == stage3-rules || "$IDENTITY_TRANSITION" == stage4-geo || "$IDENTITY_TRANSITION" == stage5-composite-enforcement ]] || return 0
  health="$(docker exec extensions-self wget -qO- -T 5 http://extensions-self:8090/healthz)" || return 1
  jq -e '
    .status == "ok"
    and .identity.enabled and .identity.admin_enabled
    and .identity.mode == "shadow" and .identity.schema == "v2"
    and .identity.quality_domains.ip == "healthy"
    and .identity.quality_domains.device == "healthy"
    and .identity.quality_domains.composite == "healthy"
    and .identity.configured_rule_count >= 5
    and .identity.prospective_rule_count >= 5
    and .identity.features.delivery and .identity.features.explain
    and .identity.processing.pending == 0
    and .identity.processing.retry == 0
    and .identity.processing.failed == 0
    and .identity.delivery.sources > 0
    and .identity.delivery.gap_sources == 0
    and .identity.delivery.stale_sources == 0
    and .identity.delivery.queue_depth == 0
    and .identity.delivery.dropped == 0
    and .identity.delivery.failed == 0
  ' <<< "$health" >/dev/null || return 1
  if [[ "$IDENTITY_TRANSITION" == stage3-rules ]]; then
    shadow_until="$(jq -er '.identity.shadow_until' <<< "$health")" || return 1
    [[ "$(date -u -d "$shadow_until" +%s)" -ge "$(date -u -d '+14 days' +%s)" ]] || return 1
    jq -e '
      .identity.domains.ip == "disabled"
      and .identity.domains.device == "disabled"
      and .identity.domains.composite == "disabled"
      and .identity.effective_rule_count == 0
      and (.identity.features.current_score | not)
      and (.identity.features.cases | not)
    ' <<< "$health" >/dev/null
  else
    jq -e '
      .identity.domains.ip == "healthy"
      and .identity.domains.device == "healthy"
      and .identity.domains.composite == "healthy"
      and .identity.effective_rule_count >= 5
      and .identity.features.current_score
      and .identity.features.cases
    ' <<< "$health" >/dev/null
  fi
	if [[ "$IDENTITY_TRANSITION" == stage5-composite-enforcement ]]; then
		jq -e '
			.identity.geo_source == "cloudflare_verified"
			and (.identity.features.composite_enforcement | not)
		' <<< "$health" >/dev/null
	fi
}

render_and_match() {
  local base="$1" custom="$2" env="$3" expected_hash="$4" expected_main="$5" expected_extensions="$6"
  local rendered
  rendered="$(mktemp "$DATA_DIR/.apply-compose-$JOB_ID.XXXXXX")" || return 1
  if ! release_render_explicit_compose "$base" "$custom" "$env" "$rendered" "$LOG" \
    || ! release_validate_rendered_compose "$rendered" "$expected_main" "$expected_extensions" \
    || [[ "$(sha256sum "$rendered" | awk '{print $1}')" != "$expected_hash" ]]; then
    rm -f "$rendered"
    return 1
  fi
  rm -f "$rendered"
}

record_matches_manifest() {
  local record="$1"
  jq -e --argjson manifest "$(cat "$manifest")" --arg operation "$JOB_ID" '
    .release_id == $manifest.target_release_id
    and .official_version == $manifest.target_official_version
    and .official_commit == $manifest.stable_release_commit
    and .custom_version == $manifest.target_custom_version
    and .custom_version_sequence == $manifest.proposed_custom_sequence
    and .custom_commit == $manifest.target_commit
    and .main_digest == $manifest.main_digest
    and .extensions_digest == $manifest.extensions_digest
    and .base_compose_sha256 == $manifest.target_base_compose_sha256
    and .custom_compose_sha256 == $manifest.target_custom_compose_sha256
    and .rendered_compose_sha256 == $manifest.target_rendered_compose_sha256
    and .env_sha256 == $manifest.target_env_sha256
    and .backup_dir == $manifest.backup_dir
    and .backup_manifest_sha256 == $manifest.backup_manifest_sha256
    and .source_kind == $manifest.update_kind
    and .operation_id == $operation
	and (.identity_transition // "") == ($manifest.identity_transition // "")
  ' <<< "$record" >/dev/null
}

target_artifact_identity_matches() {
  [[ "$(git -C "$REPO" rev-parse HEAD 2>/dev/null || true)" == "$TARGET_COMMIT" ]] || return 1
  [[ "$(sha256sum "$COMPOSE_BASE" | awk '{print $1}')" == "$(jq -r '.target_base_compose_sha256' "$manifest")" ]] || return 1
  [[ "$(sha256sum "$COMPOSE_CUSTOM" | awk '{print $1}')" == "$(jq -r '.target_custom_compose_sha256' "$manifest")" ]] || return 1
  [[ "$(sha256sum "$ENV_FILE" | awk '{print $1}')" == "$(jq -r '.target_env_sha256' "$manifest")" ]] || return 1
  release_env_matches_digest_pair "$ENV_FILE" "$MAIN_REPOSITORY@$MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" || return 1
  release_verify_local_image_identity "$MAIN_REPOSITORY" "$MAIN_DIGEST" "$TARGET_COMMIT" "${TARGET_OFFICIAL_VERSION#v}" || return 1
  release_verify_local_image_identity "$EXTENSIONS_REPOSITORY" "$EXTENSIONS_DIGEST" "$TARGET_COMMIT" "${TARGET_OFFICIAL_VERSION#v}" || return 1
  render_and_match "$COMPOSE_BASE" "$COMPOSE_CUSTOM" "$ENV_FILE" "$(jq -r '.target_rendered_compose_sha256' "$manifest")" \
    "$MAIN_REPOSITORY@$MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST"
}

live_target_identity_matches() {
  target_artifact_identity_matches || return 1
  release_running_container_matches_image sub2api "$MAIN_REPOSITORY@$MAIN_DIGEST" || return 1
  release_running_container_matches_image extensions-self "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" || return 1
  wait_container_healthy sub2api || return 1
	wait_container_healthy extensions-self || return 1
	validate_identity_runtime
}

validate_update_identity_contract() {
  jq -e -n --argjson base "$BASE_RECORD" --argjson manifest "$(cat "$manifest")" '
    $manifest.current_official_version == $base.official_version
    and ($manifest.custom_docs_only | type == "boolean")
    and $manifest.current_custom_version == $base.custom_version
    and $manifest.source_commit == $base.custom_commit
    and $manifest.stable_release_tag == $manifest.target_official_version
	and (if $manifest.update_kind == "identity-config" then
	  $manifest.custom_docs_only == false
	  and $manifest.advances_custom_version == false
	  and $manifest.proposed_custom_sequence == $base.custom_version_sequence
	  and $manifest.target_official_version == $base.official_version
	  and $manifest.stable_release_commit == $base.official_commit
	  and $manifest.target_custom_version == $base.custom_version
	  and $manifest.target_custom_commit == $base.custom_commit
	  and $manifest.target_commit == $base.custom_commit
	  and $manifest.main_digest == $base.main_digest
	  and $manifest.extensions_digest == $base.extensions_digest
	  and $manifest.current_env_sha256 == $base.env_sha256
	  and $manifest.target_env_sha256 != $base.env_sha256
	  and ($manifest.identity_transition | IN("stage0-safe-reset", "stage1-v2", "stage1-ip", "stage1-device", "stage2-admin", "stage3-shadow-window", "stage3-rules", "stage4-geo", "stage5-composite-enforcement"))
	elif $manifest.update_kind == "official" then
      $manifest.advances_custom_version == false
      and $manifest.proposed_custom_sequence == $base.custom_version_sequence
      and $manifest.target_custom_version == $base.custom_version
      and (if $manifest.custom_docs_only then
        $manifest.target_custom_commit != $base.custom_commit
      else $manifest.target_custom_commit == $base.custom_commit end)
      and $manifest.target_official_version != $base.official_version
      and $manifest.stable_release_commit != $base.official_commit
    elif $manifest.update_kind == "custom" then
      $manifest.custom_docs_only == false
      and $manifest.advances_custom_version == true
      and $manifest.target_official_version == $base.official_version
      and $manifest.stable_release_commit == $base.official_commit
      and $manifest.target_custom_commit == $manifest.target_commit
      and $manifest.target_custom_commit != $base.custom_commit
    elif $manifest.update_kind == "combined" then
      $manifest.custom_docs_only == false
      and $manifest.advances_custom_version == true
      and $manifest.target_official_version != $base.official_version
      and $manifest.stable_release_commit != $base.official_commit
      and $manifest.target_custom_commit != $base.custom_commit
    else false end)
  ' >/dev/null
}

build_release_record() {
  local published_at="$1"
  jq -n \
    --arg release "$TARGET_RELEASE_ID" --arg official_version "$TARGET_OFFICIAL_VERSION" \
    --arg official_commit "$(jq -r '.stable_release_commit' "$manifest")" --arg custom_version "$TARGET_CUSTOM_VERSION" \
    --argjson custom_sequence "$PROPOSED_CUSTOM_SEQUENCE" --arg custom_commit "$TARGET_COMMIT" \
    --arg main "$MAIN_DIGEST" --arg extensions "$EXTENSIONS_DIGEST" \
    --arg base_sha "$(jq -r '.target_base_compose_sha256' "$manifest")" \
    --arg custom_sha "$(jq -r '.target_custom_compose_sha256' "$manifest")" \
    --arg rendered_sha "$(jq -r '.target_rendered_compose_sha256' "$manifest")" \
    --arg env_sha "$(jq -r '.target_env_sha256' "$manifest")" --arg backup "$BACKUP_DIR" \
    --arg backup_sha "$(jq -r '.backup_manifest_sha256' "$manifest")" --arg published "$published_at" \
	--arg source_kind "$UPDATE_KIND" --arg operation "$JOB_ID" --arg identity_transition "$IDENTITY_TRANSITION" '
      {schema_version:1,release_id:$release,official_version:$official_version,official_commit:$official_commit,
      custom_version:$custom_version,custom_version_sequence:$custom_sequence,custom_commit:$custom_commit,
      main_digest:$main,extensions_digest:$extensions,base_compose_sha256:$base_sha,custom_compose_sha256:$custom_sha,
      rendered_compose_sha256:$rendered_sha,env_sha256:$env_sha,backup_dir:$backup,
	  backup_manifest_sha256:$backup_sha,published_at:$published,source_kind:$source_kind,operation_id:$operation}
	  + (if $identity_transition == "" then {} else {identity_transition:$identity_transition} end)
    '
}

restore_base_runtime() {
  local source_head="$1" source_ref="$2" rendered=''
  release_restore_source_snapshot "$source_head" "$source_ref" >> "$LOG" 2>&1 || return 1
  release_install_snapshot_artifacts "$BACKUP_DIR" || return 1
  release_env_matches_digest_pair "$ENV_FILE" "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" || return 1
  release_verify_local_image_identity "$MAIN_REPOSITORY" "$CURRENT_MAIN_DIGEST" "$SOURCE_COMMIT" "${CURRENT_OFFICIAL_VERSION#v}" || return 1
  release_verify_local_image_identity "$EXTENSIONS_REPOSITORY" "$CURRENT_EXTENSIONS_DIGEST" "$SOURCE_COMMIT" "${CURRENT_OFFICIAL_VERSION#v}" || return 1
  rendered="$(mktemp "$DATA_DIR/.rollback-compose-$JOB_ID.XXXXXX")" || return 1
  if ! release_render_explicit_compose "$COMPOSE_BASE" "$COMPOSE_CUSTOM" "$ENV_FILE" "$rendered" "$LOG" \
    || ! release_validate_rendered_compose "$rendered" "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
    || [[ "$(sha256sum "$rendered" | awk '{print $1}')" != "$(jq -r '.rendered_compose_sha256' <<< "$BASE_RECORD")" ]] \
    || ! SUB2API_IMAGE="$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
      docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" \
      up -d --pull never --no-deps --force-recreate extensions-self >> "$LOG" 2>&1 \
    || ! wait_container_healthy extensions-self \
    || ! SUB2API_IMAGE="$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
      docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" \
      up -d --pull never --no-deps --force-recreate sub2api >> "$LOG" 2>&1 \
    || ! wait_container_healthy sub2api \
    || ! run_complete_health "$rendered"; then
    rm -f "$rendered"
    return 1
  fi
  rm -f "$rendered"
}

restore_base_before_main_switch() {
  local source_head="$1" source_ref="$2" rendered=''
  release_restore_source_snapshot "$source_head" "$source_ref" >> "$LOG" 2>&1 || return 1
  release_install_snapshot_artifacts "$BACKUP_DIR" || return 1
  release_env_matches_digest_pair "$ENV_FILE" "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" || return 1
  release_verify_local_image_identity "$MAIN_REPOSITORY" "$CURRENT_MAIN_DIGEST" "$SOURCE_COMMIT" "${CURRENT_OFFICIAL_VERSION#v}" || return 1
  release_verify_local_image_identity "$EXTENSIONS_REPOSITORY" "$CURRENT_EXTENSIONS_DIGEST" "$SOURCE_COMMIT" "${CURRENT_OFFICIAL_VERSION#v}" || return 1
  release_running_container_matches_image sub2api "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" || return 1
  wait_container_healthy sub2api || return 1
  rendered="$(mktemp "$DATA_DIR/.rollback-compose-$JOB_ID.XXXXXX")" || return 1
  if ! release_render_explicit_compose "$COMPOSE_BASE" "$COMPOSE_CUSTOM" "$ENV_FILE" "$rendered" "$LOG" \
    || ! release_validate_rendered_compose "$rendered" "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
    || [[ "$(sha256sum "$rendered" | awk '{print $1}')" != "$(jq -r '.rendered_compose_sha256' <<< "$BASE_RECORD")" ]] \
    || ! SUB2API_IMAGE="$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
      docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" \
      up -d --pull never --no-deps --force-recreate extensions-self >> "$LOG" 2>&1 \
    || ! wait_container_healthy extensions-self \
    || ! run_complete_health "$rendered"; then
    rm -f "$rendered"
    return 1
  fi
  rm -f "$rendered"
}

restore_interrupted_base_runtime() {
  local source_head="$1" source_ref="$2"
  if [[ "$operation_status" == switching_extensions \
    || ("$UPDATE_KIND" == identity-config && "$IDENTITY_TRANSITION" != stage0-safe-reset && "$IDENTITY_TRANSITION" != stage1-* && "$IDENTITY_TRANSITION" != stage4-geo && "$IDENTITY_TRANSITION" != stage5-composite-enforcement) ]]; then
    restore_base_before_main_switch "$source_head" "$source_ref"
  else
    restore_base_runtime "$source_head" "$source_ref"
  fi
}

# A base runtime can be fully restored even when a transient external probe
# makes the post-restore complete health suite return non-zero. Verify the
# durable identities independently before deciding that rollback failed.
base_runtime_identity_matches() {
  release_source_snapshot || return 1
  [[ "$SOURCE_HEAD" == "$SOURCE_COMMIT" ]] || return 1
  [[ "$(sha256sum "$COMPOSE_BASE" | awk '{print $1}')" == "$(jq -r '.base_compose_sha256' <<< "$BASE_RECORD")" ]] || return 1
  [[ "$(sha256sum "$COMPOSE_CUSTOM" | awk '{print $1}')" == "$(jq -r '.custom_compose_sha256' <<< "$BASE_RECORD")" ]] || return 1
  [[ "$(sha256sum "$ENV_FILE" | awk '{print $1}')" == "$(jq -r '.env_sha256' <<< "$BASE_RECORD")" ]] || return 1
  release_env_matches_digest_pair "$ENV_FILE" "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" || return 1
  release_running_container_matches_image sub2api "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" || return 1
  release_running_container_matches_image extensions-self "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" || return 1
  wait_container_healthy sub2api || return 1
  wait_container_healthy extensions-self || return 1
  render_and_match "$COMPOSE_BASE" "$COMPOSE_CUSTOM" "$ENV_FILE" "$(jq -r '.rendered_compose_sha256' <<< "$BASE_RECORD")" \
    "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST"
}

expected_high_water="$BASE_CUSTOM_HIGH_WATER"
[[ "$ADVANCES_CUSTOM_VERSION" != true ]] || expected_high_water="$PROPOSED_CUSTOM_SEQUENCE"
advance_flag=0
[[ "$ADVANCES_CUSTOM_VERSION" != true ]] || advance_flag=1
state_release_id="$(jq -r '.current_release_id' "$STATE_PATH")"
state_high_water="$(jq -r '.custom_version_high_water' "$STATE_PATH")"
state_operation_id="$(jq -r '.active_operation_id // empty' "$STATE_PATH")"
validate_update_identity_contract \
  || fail_before_mutation 'prepared update kind contradicts the base and target identities' UPDATE_KIND_DRIFT failed

if [[ -e "$TARGET_RECORD_PATH" || -L "$TARGET_RECORD_PATH" ]]; then
  ledger_validate_release "$TARGET_RECORD_PATH" || fail_before_mutation 'target ledger record is invalid' LEDGER_INCONSISTENT failed
  TARGET_RECORD="$(cat "$TARGET_RECORD_PATH")"
  record_matches_manifest "$TARGET_RECORD" || fail_before_mutation 'target record contradicts prepared manifest' LEDGER_INCONSISTENT failed
  live_target_identity_matches || fail_before_mutation 'running target identity contradicts committed ledger state' LEDGER_INCONSISTENT failed
  TARGET_PROJECTION="$(ledger_projection_for_release "$TARGET_RECORD")" \
    || fail_before_mutation 'target projection cannot be reconstructed' LEDGER_INCONSISTENT failed
  if [[ "$operation_status" == health_checking ]]; then
	  run_complete_health "$TARGET_DIR/rendered-compose.json" && validate_identity_runtime \
      || fail_before_mutation 'partial ledger recovery failed the complete health suite' LEDGER_INCONSISTENT failed
  elif [[ "$operation_status" != success ]]; then
    fail_before_mutation 'partial ledger recovery has an invalid operation phase' LEDGER_INCONSISTENT failed
  fi
  release_attach_source_branch "$TARGET_COMMIT" "$BRANCH" \
    || fail_before_mutation 'target source could not be attached to the production branch' SOURCE_BRANCH_ATTACH_FAILED failed

  if [[ "$state_release_id" == "$TARGET_RELEASE_ID" ]]; then
    ledger_recover_or_refuse "$TARGET_RECORD" "$TARGET_PROJECTION" "$expected_high_water" "$JOB_ID" \
      || fail_before_mutation 'exact committed release recovery was refused' LEDGER_INCONSISTENT failed
  elif [[ "$state_release_id" == "$BASE_RELEASE_ID" && "$state_high_water" -eq "$BASE_CUSTOM_HIGH_WATER" \
    && "$state_operation_id" == "$JOB_ID" ]]; then
    if [[ "$(jq -cS . "$PRODUCTION_RELEASE_STATE_FILE")" == "$(jq -cS . <<< "$TARGET_PROJECTION")" ]]; then
      ledger_recover_or_refuse "$TARGET_RECORD" "$TARGET_PROJECTION" "$expected_high_water" "$JOB_ID" \
        || fail_before_mutation 'partial ledger projection recovery was refused' LEDGER_INCONSISTENT failed
    elif [[ "$(jq -cS . "$PRODUCTION_RELEASE_STATE_FILE")" == "$(jq -cS . <<< "$BASE_PROJECTION")" ]]; then
      if ! ledger_commit_release "$TARGET_RECORD" "$advance_flag"; then
        ledger_recover_or_refuse "$TARGET_RECORD" "$TARGET_PROJECTION" "$expected_high_water" "$JOB_ID" \
          || fail_before_mutation 'partial release record recovery was refused' LEDGER_INCONSISTENT failed
      fi
    else
      fail_before_mutation 'partial release metadata contradicts both base and target projections' LEDGER_INCONSISTENT failed
    fi
  else
    fail_before_mutation 'partial release metadata contradicts the ledger pointer or operation owner' LEDGER_INCONSISTENT failed
  fi
  exit 0
fi

[[ "$state_release_id" != "$TARGET_RELEASE_ID" ]] \
  || fail_before_mutation 'current ledger target record is missing' LEDGER_INCONSISTENT failed

if [[ "$state_release_id" == "$BASE_RELEASE_ID" && "$state_high_water" -eq "$BASE_CUSTOM_HIGH_WATER" \
  && "$state_operation_id" == "$JOB_ID" && "$operation_status" == health_checking \
  && "$(jq -cS . "$PRODUCTION_RELEASE_STATE_FILE")" == "$(jq -cS . <<< "$BASE_PROJECTION")" ]] \
  && live_target_identity_matches; then
	run_complete_health "$TARGET_DIR/rendered-compose.json" && validate_identity_runtime \
    || fail_before_mutation 'interrupted target runtime failed the complete health suite' LEDGER_INCONSISTENT failed
  release_attach_source_branch "$TARGET_COMMIT" "$BRANCH" \
    || fail_before_mutation 'interrupted target source could not be attached to the production branch' SOURCE_BRANCH_ATTACH_FAILED failed
  published_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  RELEASE_RECORD="$(build_release_record "$published_at")" \
    || fail_before_mutation 'interrupted release record construction failed' LEDGER_INCONSISTENT failed
  if ! ledger_commit_release "$RELEASE_RECORD" "$advance_flag"; then
    TARGET_PROJECTION="$(ledger_projection_for_release "$RELEASE_RECORD")" \
      || fail_before_mutation 'interrupted target projection construction failed' LEDGER_INCONSISTENT failed
    ledger_recover_or_refuse "$RELEASE_RECORD" "$TARGET_PROJECTION" "$expected_high_water" "$JOB_ID" \
      || fail_before_mutation 'interrupted target runtime recovery was refused' LEDGER_INCONSISTENT failed
  fi
  exit 0
fi

interrupted_mutation=false
case "$operation_status" in
  switching_extensions|switching_main|rolling_back|health_checking) interrupted_mutation=true ;;
  validating_manifest)
    [[ "$(git -C "$REPO" rev-parse HEAD 2>/dev/null || true)" == "$SOURCE_COMMIT" ]] || interrupted_mutation=true
    ;;
esac
if [[ "$interrupted_mutation" == true && "$state_release_id" == "$BASE_RELEASE_ID" \
  && "$state_high_water" -eq "$BASE_CUSTOM_HIGH_WATER" && "$state_operation_id" == "$JOB_ID" \
  && "$(jq -cS . "$PRODUCTION_RELEASE_STATE_FILE")" == "$(jq -cS . <<< "$BASE_PROJECTION")" ]]; then
  release_job_update "$JOB_ID" rolling_back 'Interrupted production switch detected; restoring the prepared base snapshot' \
    '{"cause_error_code":"INTERRUPTED_APPLY","production_changed":true,"rollback":{"attempted":true,"succeeded":false,"message":"automatic restoration resumed"}}' || true
  if restore_interrupted_base_runtime "$SOURCE_COMMIT" "$BRANCH" \
    && ledger_restore_failed_apply "$BASE_RELEASE_ID" "$BASE_CUSTOM_HIGH_WATER" "$JOB_ID" "$BASE_PROJECTION"; then
    release_job_update "$JOB_ID" failed_rolled_back 'Interrupted production switch restored to the base snapshot' \
      "$(jq -n --arg artifact "$BACKUP_DIR" '{error_code:"APPLY_FAILED_ROLLED_BACK",cause_error_code:"INTERRUPTED_APPLY",artifact_path:$artifact,published:false,production_changed:false,rollback:{attempted:true,succeeded:true,message:"automatic restoration completed"}}')" || true
  else
    release_job_update "$JOB_ID" rollback_failed 'Interrupted production switch could not be restored completely' \
      "$(jq -n --arg artifact "$BACKUP_DIR" '{error_code:"APPLY_ROLLBACK_FAILED",cause_error_code:"INTERRUPTED_APPLY",artifact_path:$artifact,published:false,production_changed:true,rollback:{attempted:true,succeeded:false,message:"automatic restoration failed; inspect the prepared backup"}}')" || true
  fi
  exit 1
fi

[[ "$manifest_expired" != true ]] \
  || fail_before_mutation 'Prepared update expired; prepare again' PREPARED_EXPIRED expired

[[ "$state_release_id" == "$BASE_RELEASE_ID" ]] \
  || fail_before_mutation 'current release changed since preparation' CURRENT_RELEASE_DRIFT
[[ "$state_high_water" -eq "$BASE_CUSTOM_HIGH_WATER" ]] \
  || fail_before_mutation 'custom version high-water changed since preparation' CUSTOM_HIGH_WATER_DRIFT
[[ "$state_operation_id" == "$JOB_ID" ]] \
  || fail_before_mutation 'release ledger no longer owns this operation' LEDGER_OPERATION_DRIFT

jq -e -n --argjson record "$BASE_RECORD" --argjson manifest "$(cat "$manifest")" '
  $record.release_id == $manifest.base_release_id
  and $record.official_version == $manifest.current_official_version
  and $record.custom_version == $manifest.current_custom_version
  and $record.custom_commit == $manifest.source_commit
  and $record.main_digest == $manifest.current_main_digest
  and $record.extensions_digest == $manifest.current_extensions_digest
' >/dev/null || fail_before_mutation 'base release record contradicts prepared manifest' LEDGER_INCONSISTENT failed
[[ "$(jq -cS . "$PRODUCTION_RELEASE_STATE_FILE")" == "$(jq -cS . <<< "$BASE_PROJECTION")" ]] \
  || fail_before_mutation 'compatibility projection drifted from the ledger' LEDGER_PROJECTION_DRIFT

ORIGIN_COMMIT="$(git -C "$REPO" rev-parse "origin/$BRANCH" 2>/dev/null || true)"
[[ "$ORIGIN_COMMIT" == "$TARGET_COMMIT" ]] \
  || fail_before_mutation 'origin/custom-release changed since preparation' ORIGIN_HEAD_DRIFT
release_source_snapshot || fail_before_mutation 'production source is dirty or unreadable' SOURCE_WORKTREE_DIRTY
[[ "$SOURCE_HEAD" == "$SOURCE_COMMIT" ]] \
  || fail_before_mutation 'production source HEAD changed since preparation' SOURCE_HEAD_DRIFT

[[ "$(sha256sum "$COMPOSE_BASE" | awk '{print $1}')" == "$(jq -r '.current_base_compose_sha256' "$manifest")" ]] \
  || fail_before_mutation 'production base Compose changed since preparation' CURRENT_COMPOSE_DRIFT
[[ "$(sha256sum "$COMPOSE_CUSTOM" | awk '{print $1}')" == "$(jq -r '.current_custom_compose_sha256' "$manifest")" ]] \
  || fail_before_mutation 'production custom Compose changed since preparation' CURRENT_COMPOSE_DRIFT
[[ "$(sha256sum "$ENV_FILE" | awk '{print $1}')" == "$(jq -r '.env_sha256' <<< "$BASE_RECORD")" ]] \
  || fail_before_mutation 'production environment changed since preparation' CURRENT_ENV_DRIFT
release_env_matches_digest_pair "$ENV_FILE" "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
  || fail_before_mutation 'production digest pair changed since preparation' CURRENT_DIGEST_DRIFT
render_and_match "$COMPOSE_BASE" "$COMPOSE_CUSTOM" "$ENV_FILE" "$(jq -r '.rendered_compose_sha256' <<< "$BASE_RECORD")" \
  "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
  || fail_before_mutation 'rendered production Compose changed since preparation' CURRENT_COMPOSE_DRIFT

release_env_matches_digest_pair "$TARGET_DIR/.env" "$MAIN_REPOSITORY@$MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  || fail_before_mutation 'target environment digest pair is invalid' TARGET_DIGEST_DRIFT
render_and_match "$TARGET_DIR/docker-compose.yml" "$TARGET_DIR/docker-compose.custom.yml" "$TARGET_DIR/.env" \
  "$(jq -r '.target_rendered_compose_sha256' "$manifest")" "$MAIN_REPOSITORY@$MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  || fail_before_mutation 'target Compose rendering drifted' TARGET_COMPOSE_DRIFT
release_verify_local_image_identity "$MAIN_REPOSITORY" "$MAIN_DIGEST" "$TARGET_COMMIT" "${TARGET_OFFICIAL_VERSION#v}" \
  || fail_before_mutation 'prepared main image is missing or has drifted locally' PREPARED_IMAGE_DRIFT
release_verify_local_image_identity "$EXTENSIONS_REPOSITORY" "$EXTENSIONS_DIGEST" "$TARGET_COMMIT" "${TARGET_OFFICIAL_VERSION#v}" \
  || fail_before_mutation 'prepared extensions image is missing or has drifted locally' PREPARED_IMAGE_DRIFT
release_verify_local_image_identity "$MAIN_REPOSITORY" "$CURRENT_MAIN_DIGEST" "$SOURCE_COMMIT" "$(jq -r '.current_official_version | ltrimstr("v")' "$manifest")" \
  || fail_before_mutation 'base main image is unavailable for automatic restoration' BASE_IMAGE_DRIFT
release_verify_local_image_identity "$EXTENSIONS_REPOSITORY" "$CURRENT_EXTENSIONS_DIGEST" "$SOURCE_COMMIT" "$(jq -r '.current_official_version | ltrimstr("v")' "$manifest")" \
  || fail_before_mutation 'base extensions image is unavailable for automatic restoration' BASE_IMAGE_DRIFT
release_running_container_matches_image sub2api "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" \
  || fail_before_mutation 'running main container no longer matches the base digest' CURRENT_RUNTIME_DRIFT
release_running_container_matches_image extensions-self "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
  || fail_before_mutation 'running extensions container no longer matches the base digest' CURRENT_RUNTIME_DRIFT
wait_container_healthy sub2api || fail_before_mutation 'running main container is not healthy' CURRENT_RUNTIME_DRIFT
wait_container_healthy extensions-self || fail_before_mutation 'running extensions container is not healthy' CURRENT_RUNTIME_DRIFT
validate_identity_pre_switch || fail_before_mutation 'identity data quality or Shadow prerequisites changed after preparation' IDENTITY_PREFLIGHT_DRIFT failed

rollback_started=false
main_switch_started=false
apply_failure_message=''
apply_failure_code=''

rollback_on_error() {
  local exit_code="${1:-$?}" rollback_ok=true
  trap - ERR
  if [[ "$rollback_started" == true ]]; then
    release_job_update "$JOB_ID" rolling_back 'Production switch failed; restoring the prepared base snapshot' \
      "$(jq -n --arg cause "${apply_failure_code:-APPLY_FAILED}" '{cause_error_code:$cause,production_changed:true,rollback:{attempted:true,succeeded:false,message:"automatic restoration started"}}')" || true
    if [[ "$main_switch_started" == true ]]; then
      restore_base_runtime "$SOURCE_HEAD" "$SOURCE_REF" || rollback_ok=false
    else
      restore_base_before_main_switch "$SOURCE_HEAD" "$SOURCE_REF" || rollback_ok=false
    fi
    if [[ "$rollback_ok" != true ]] && base_runtime_identity_matches; then
      log 'Base runtime identity is exact after rollback; treating external health probe failure as non-blocking'
      rollback_ok=true
    fi
    [[ "$rollback_ok" != true ]] || ledger_restore_failed_apply "$BASE_RELEASE_ID" "$BASE_CUSTOM_HIGH_WATER" "$JOB_ID" "$BASE_PROJECTION" || rollback_ok=false
    if [[ "$rollback_ok" == true ]]; then
      release_job_update "$JOB_ID" failed_rolled_back "${apply_failure_message:-production switch failed}; base snapshot restored" \
        "$(jq -n --arg cause "${apply_failure_code:-APPLY_FAILED}" --arg artifact "$BACKUP_DIR" '{error_code:"APPLY_FAILED_ROLLED_BACK",cause_error_code:$cause,artifact_path:$artifact,published:false,production_changed:false,rollback:{attempted:true,succeeded:true,message:"automatic restoration completed"}}')" || true
    else
      release_job_update "$JOB_ID" rollback_failed "${apply_failure_message:-production switch failed}; automatic restoration failed" \
        "$(jq -n --arg cause "${apply_failure_code:-APPLY_FAILED}" --arg artifact "$BACKUP_DIR" '{error_code:"APPLY_ROLLBACK_FAILED",cause_error_code:$cause,artifact_path:$artifact,published:false,production_changed:true,rollback:{attempted:true,succeeded:false,message:"automatic restoration failed; inspect the prepared backup"}}')" || true
    fi
  fi
  exit "$exit_code"
}

abort_apply() {
  apply_failure_message="$1"
  apply_failure_code="${2:-APPLY_FAILED}"
  rollback_on_error 1
}

trap 'apply_failure_message="unexpected apply error at line $LINENO"; apply_failure_code=UNEXPECTED_APPLY_ERROR; rollback_on_error 1' ERR
release_job_update "$JOB_ID" validating_manifest 'Prepared manifest and production ledger revalidated' '{}'
rollback_started=true
release_job_update "$JOB_ID" switching_extensions "Beginning extension switch to $EXTENSIONS_DIGEST" '{}'
release_checkout_exact_commit "$TARGET_COMMIT" >> "$LOG" 2>&1 || abort_apply 'target source checkout failed' SOURCE_CHECKOUT_FAILED
release_install_snapshot_artifacts "$TARGET_DIR" || abort_apply 'target Compose pair or environment installation failed' TARGET_ARTIFACT_INSTALL_FAILED
target_artifact_identity_matches || abort_apply 'installed target artifacts failed exact validation' TARGET_ARTIFACT_DRIFT
refresh_account_monitor_source_views >> "$LOG" 2>&1 \
  || abort_apply 'account-monitor source views could not be refreshed' SOURCE_VIEWS_FAILED

SUB2API_IMAGE="$MAIN_REPOSITORY@$MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" \
  up -d --pull never --no-deps --force-recreate extensions-self >> "$LOG" 2>&1 \
  || abort_apply 'extensions switch failed' EXTENSIONS_SWITCH_FAILED
wait_container_healthy extensions-self || abort_apply 'extensions health check failed' EXTENSIONS_HEALTH_FAILED

if [[ "$UPDATE_KIND" != identity-config || "$IDENTITY_TRANSITION" == stage0-safe-reset || "$IDENTITY_TRANSITION" == stage1-* || "$IDENTITY_TRANSITION" == stage4-geo || "$IDENTITY_TRANSITION" == stage5-composite-enforcement ]]; then
  release_job_update "$JOB_ID" switching_main "Switching main application to $MAIN_DIGEST" '{}'
  main_switch_started=true
  SUB2API_IMAGE="$MAIN_REPOSITORY@$MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
    docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" \
    up -d --pull never --no-deps --force-recreate sub2api >> "$LOG" 2>&1 \
    || abort_apply 'main application switch failed' MAIN_SWITCH_FAILED
  wait_container_healthy sub2api || abort_apply 'main application health check failed' MAIN_HEALTH_FAILED
fi

release_job_update "$JOB_ID" health_checking 'Checking internal, public, native admin, extension, and data-quality health' '{}'
run_complete_health "$TARGET_DIR/rendered-compose.json" || abort_apply 'complete production health suite failed' COMPLETE_HEALTH_FAILED
validate_identity_runtime || abort_apply 'identity runtime does not match the prepared transition' IDENTITY_RUNTIME_DRIFT
live_target_identity_matches || abort_apply 'running target identity failed exact validation' TARGET_RUNTIME_DRIFT
release_attach_source_branch "$TARGET_COMMIT" "$BRANCH" >> "$LOG" 2>&1 \
  || abort_apply 'target source could not be attached to the production branch' SOURCE_BRANCH_ATTACH_FAILED

published_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
RELEASE_RECORD="$(build_release_record "$published_at")" || abort_apply 'release record construction failed' RELEASE_RECORD_INVALID

if ! ledger_commit_release "$RELEASE_RECORD" "$advance_flag"; then
  TARGET_PROJECTION="$(ledger_projection_for_release "$RELEASE_RECORD")" || abort_apply 'target projection construction failed' LEDGER_COMMIT_FAILED
  if ledger_recover_or_refuse "$RELEASE_RECORD" "$TARGET_PROJECTION" "$expected_high_water" "$JOB_ID"; then
    exit 0
  fi
  if ledger_commit_release "$RELEASE_RECORD" "$advance_flag"; then
    exit 0
  fi
  abort_apply 'release ledger publication failed exact recovery' LEDGER_COMMIT_FAILED
fi

exit 0
