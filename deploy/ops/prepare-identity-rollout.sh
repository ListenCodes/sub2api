#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
COMMON_HELPER="${SUB2API_RELEASE_COMMON_HELPER:-$SCRIPT_DIR/release-common.sh}"
LEDGER_HELPER="${SUB2API_RELEASE_LEDGER_HELPER:-$SCRIPT_DIR/release-ledger.sh}"
REPO="${SUB2API_REPO:-/root/sub2api}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
NGINX_VHOST="${SUB2API_NGINX_VHOST:-/etc/nginx/sites-available/sub.ailisten.top}"
ORIGIN_CERT="${SUB2API_ORIGIN_CERT:-/etc/nginx/ssl/ailisten.top.crt}"
ORIGIN_KEY="${SUB2API_ORIGIN_KEY:-/etc/nginx/ssl/ailisten.top.key}"
BACKUP_ROOT="${SUB2API_BACKUP_ROOT:-/var/lib/sub2api-release/backups}"
export SUB2API_RELEASE_BACKUP_ROOT="${SUB2API_RELEASE_BACKUP_ROOT:-$BACKUP_ROOT}"
IDENTITY_SECRET_FILE="${SUB2API_IDENTITY_SECRET_FILE:-/etc/sub2api/identity-secrets.env}"
MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"
LOG="${SUB2API_SYNC_PUBLISH_LOG:-/var/log/sub2api-release.log}"

source "$STATE_HELPER"
source "$COMMON_HELPER"
source "$LEDGER_HELPER"

JOB_ID="${SUB2API_JOB_ID:-${2:-}}"
[[ "${1:-}" == --job-id && -n "$JOB_ID" ]] || { printf 'usage: prepare-identity-rollout.sh --job-id <job-id>\n' >&2; exit 2; }
release_valid_job_id "$JOB_ID" || { printf 'invalid job id\n' >&2; exit 2; }
JOB_FILE="$(release_job_path "$JOB_ID")"
[[ -r "$JOB_FILE" ]] || { printf 'release job file is missing\n' >&2; exit 1; }

fail_prepare() {
	local message="$1" code="${2:-IDENTITY_PREPARE_FAILED}" evidence="${3:-}" metadata
	[[ -n "$evidence" ]] || evidence='{}'
  metadata="$(jq -cn --arg code "$code" --argjson evidence "$evidence" '$evidence + {error_code:$code,published:false,production_changed:false}')"
  ledger_settle_pre_mutation_failure "$JOB_ID" failed "$message" "$metadata" || true
  printf '%s\n' "$message" >&2
  exit 1
}
trap 'fail_prepare "unexpected identity prepare failure at line $LINENO" UNEXPECTED_IDENTITY_PREPARE_ERROR' ERR
CURRENT_RENDERED_JSON=''
MANIFEST_SOURCE=''
cleanup_prepare() {
  [[ -z "$CURRENT_RENDERED_JSON" ]] || rm -f "$CURRENT_RENDERED_JSON"
  [[ -z "$MANIFEST_SOURCE" ]] || rm -f "$MANIFEST_SOURCE"
}
trap cleanup_prepare EXIT

env_value() {
  local file="$1" key="$2"
  awk -v key="$key" 'index($0,key "=")==1 {count++; value=substr($0,length(key)+2)} END {if (count>1) exit 2; if (count==1) print value}' "$file"
}

env_bool() {
  local value
  value="$(env_value "$1" "$2")" || return 1
  [[ -z "$value" || "$value" == false ]] && { printf 'false\n'; return 0; }
  [[ "$value" == true ]] && { printf 'true\n'; return 0; }
  return 1
}

set_env_value() {
  local file="$1" key="$2" value="$3" temporary
  temporary="$(mktemp "$(dirname "$file")/.identity-env.XXXXXX")" || return 1
  awk -v key="$key" -v value="$value" '
    BEGIN {written=0}
    index($0,key "=")==1 {if (!written) {print key "=" value; written=1}; next}
    {print}
    END {if (!written) print key "=" value}
  ' "$file" > "$temporary" || { rm -f "$temporary"; return 1; }
  chmod 0600 "$temporary" || { rm -f "$temporary"; return 1; }
  mv -f "$temporary" "$file"
}

require_flags() {
  while [[ "$#" -gt 0 ]]; do
    [[ "$(env_bool "$ENV_FILE" "$1")" == "$2" ]] || return 1
    shift 2
  done
}

valid_base64_key() {
  local value="$1"
  [[ -n "$value" ]] || return 1
  [[ "$(printf '%s' "$value" | base64 -d 2>/dev/null | wc -c | tr -d ' ')" == 32 ]]
}

trusted_proxy_gateway() {
  local gateway
  gateway="$(docker network inspect deploy_sub2api-network --format '{{range .IPAM.Config}}{{println .Gateway}}{{end}}' \
    | awk 'NF {if (++count > 1) exit 2; value=$0} END {if (count==1) print value}')" || return 1
	if [[ "$gateway" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
	  awk -F. 'NF==4 {for (i=1;i<=4;i++) if ($i !~ /^[0-9]+$/ || $i<0 || $i>255) exit 1}' <<< "$gateway" || return 1
	  printf '%s/32\n' "$gateway"
	  return 0
	fi
  [[ "$gateway" =~ ^[0-9A-Fa-f:]+$ && "$gateway" == *:* ]] && { printf '%s/128\n' "$gateway"; return 0; }
  return 1
}

trusted_proxy_chain() {
	local gateway
	gateway="$(trusted_proxy_gateway)" || return 1
	printf '%s\n' "$gateway,173.245.48.0/20,103.21.244.0/22,103.22.200.0/22,103.31.4.0/22,141.101.64.0/18,108.162.192.0/18,190.93.240.0/20,188.114.96.0/20,197.234.240.0/22,198.41.128.0/17,162.158.0.0/15,104.16.0.0/13,104.24.0.0/14,172.64.0.0/13,131.0.72.0/22,2400:cb00::/32,2606:4700::/32,2803:f800::/32,2405:b500::/32,2405:8100::/32,2a06:98c0::/29,2c0f:f248::/32"
}

ensure_identity_secrets() {
  local secret_dir temporary stamp hmac_key encryption_key cookie_key
  secret_dir="$(dirname "$IDENTITY_SECRET_FILE")"
  release_path_chain_has_no_symlink "$secret_dir" || return 1
  release_path_ancestors_not_writable_by_non_owner "$(dirname "$secret_dir")" || return 1
  if [[ ! -e "$secret_dir" ]]; then
    mkdir -p -m 0700 "$secret_dir" || return 1
  fi
  [[ -d "$secret_dir" && ! -L "$secret_dir" ]] || return 1
  release_path_chain_has_no_symlink "$secret_dir" || return 1
  release_path_ancestors_not_writable_by_non_owner "$secret_dir" || return 1
  [[ "$(stat -c '%u' "$secret_dir")" == 0 ]] || return 1
  [[ "$(stat -c '%a' "$secret_dir")" == 700 ]] || return 1
  if [[ ! -e "$IDENTITY_SECRET_FILE" && ! -L "$IDENTITY_SECRET_FILE" ]]; then
    hmac_key="$(openssl rand -base64 32 | tr -d '\n')" || return 1
    encryption_key="$(openssl rand -base64 32 | tr -d '\n')" || return 1
    cookie_key="$(openssl rand -base64 32 | tr -d '\n')" || return 1
    [[ "$hmac_key" != "$encryption_key" && "$hmac_key" != "$cookie_key" && "$encryption_key" != "$cookie_key" ]] || return 1
    stamp="$(date -u '+%Y%m%dT%H%M%SZ')"
    temporary="$(mktemp "$secret_dir/.identity-secrets.XXXXXX")" || return 1
    chmod 0600 "$temporary" || { rm -f "$temporary"; return 1; }
    printf '%s\n' \
      "RISK_IDENTITY_HMAC_KEY=$hmac_key" \
      "RISK_IDENTITY_ENCRYPTION_KEY=$encryption_key" \
      "RISK_IDENTITY_ENCRYPTION_KEY_ID=aes-$stamp" \
      "RISK_DEVICE_COOKIE_SIGNING_KEY=$cookie_key" \
      "RISK_DEVICE_COOKIE_SIGNING_KEY_ID=cookie-$stamp" > "$temporary" \
      || { rm -f "$temporary"; return 1; }
    mv -f "$temporary" "$IDENTITY_SECRET_FILE" || { rm -f "$temporary"; return 1; }
  fi
  release_path_chain_has_no_symlink "$IDENTITY_SECRET_FILE" || return 1
  [[ -f "$IDENTITY_SECRET_FILE" && ! -L "$IDENTITY_SECRET_FILE" ]] || return 1
  [[ "$(stat -c '%a' "$IDENTITY_SECRET_FILE")" == 600 ]] || return 1
	[[ "$(stat -c '%u' "$IDENTITY_SECRET_FILE")" == 0 ]] || return 1
  awk -F= '
    NF < 2 {exit 1}
    $1 !~ /^(RISK_IDENTITY_HMAC_KEY|RISK_IDENTITY_ENCRYPTION_KEY|RISK_IDENTITY_ENCRYPTION_KEY_ID|RISK_DEVICE_COOKIE_SIGNING_KEY|RISK_DEVICE_COOKIE_SIGNING_KEY_ID)$/ {exit 1}
    END {if (NR != 5) exit 1}
  ' "$IDENTITY_SECRET_FILE" || return 1
  hmac_key="$(env_value "$IDENTITY_SECRET_FILE" RISK_IDENTITY_HMAC_KEY)" || return 1
  encryption_key="$(env_value "$IDENTITY_SECRET_FILE" RISK_IDENTITY_ENCRYPTION_KEY)" || return 1
  cookie_key="$(env_value "$IDENTITY_SECRET_FILE" RISK_DEVICE_COOKIE_SIGNING_KEY)" || return 1
  valid_base64_key "$hmac_key" || return 1
  valid_base64_key "$encryption_key" || return 1
  valid_base64_key "$cookie_key" || return 1
  [[ "$hmac_key" != "$encryption_key" && "$hmac_key" != "$cookie_key" && "$encryption_key" != "$cookie_key" ]] || return 1
  [[ "$(env_value "$IDENTITY_SECRET_FILE" RISK_IDENTITY_ENCRYPTION_KEY_ID)" =~ ^[A-Za-z0-9._-]{1,40}$ ]] || return 1
  [[ "$(env_value "$IDENTITY_SECRET_FILE" RISK_DEVICE_COOKIE_SIGNING_KEY_ID)" =~ ^[A-Za-z0-9._-]{1,40}$ ]]
}

apply_transition() {
  local target="$1" transition="$2" key value shadow_until
  case "$transition" in
    stage1-v2)
      require_flags \
        RISK_IDENTITY_V2_ENABLED false RISK_IDENTITY_IP_COLLECTION_ENABLED false \
        RISK_IDENTITY_DEVICE_COLLECTION_ENABLED false RISK_IDENTITY_ADMIN_ENABLED false \
        RISK_IDENTITY_RULES_ENABLED false RISK_IDENTITY_IP_RULES_ENABLED false \
        RISK_IDENTITY_DEVICE_RULES_ENABLED false RISK_IDENTITY_COMPOSITE_RULES_ENABLED false \
        || return 1
      [[ -z "$(env_value "$ENV_FILE" RISK_IDENTITY_SHADOW_UNTIL)" ]] || return 1
      ensure_identity_secrets || return 1
	  for key in RISK_IDENTITY_HMAC_KEY RISK_IDENTITY_ENCRYPTION_KEY RISK_IDENTITY_ENCRYPTION_KEY_ID \
	    RISK_DEVICE_COOKIE_SIGNING_KEY RISK_DEVICE_COOKIE_SIGNING_KEY_ID; do
	    value="$(env_value "$IDENTITY_SECRET_FILE" "$key")" || return 1
	    set_env_value "$target" "$key" "$value" || return 1
	  done
      set_env_value "$target" RISK_IDENTITY_TRUST_CLOUDFLARE_HEADERS false || return 1
	  set_env_value "$target" SERVER_TRUSTED_PROXIES "$(trusted_proxy_chain)" || return 1
	  set_env_value "$target" RISK_IDENTITY_IP_COLLECTION_ENABLED false || return 1
	  set_env_value "$target" RISK_IDENTITY_DEVICE_COLLECTION_ENABLED false || return 1
	  set_env_value "$target" RISK_IDENTITY_ADMIN_ENABLED false || return 1
	  set_env_value "$target" RISK_IDENTITY_RULES_ENABLED false || return 1
	  set_env_value "$target" RISK_IDENTITY_IP_RULES_ENABLED false || return 1
	  set_env_value "$target" RISK_IDENTITY_DEVICE_RULES_ENABLED false || return 1
	  set_env_value "$target" RISK_IDENTITY_COMPOSITE_RULES_ENABLED false || return 1
	  set_env_value "$target" RISK_IDENTITY_SHADOW_UNTIL '' || return 1
	  set_env_value "$target" RISK_IDENTITY_V2_ENABLED true
      ;;
    stage1-ip)
      require_flags RISK_IDENTITY_V2_ENABLED true RISK_IDENTITY_IP_COLLECTION_ENABLED false \
        RISK_IDENTITY_DEVICE_COLLECTION_ENABLED false RISK_IDENTITY_ADMIN_ENABLED false RISK_IDENTITY_RULES_ENABLED false \
        || return 1
      set_env_value "$target" RISK_IDENTITY_IP_COLLECTION_ENABLED true
      ;;
    stage1-device)
      require_flags RISK_IDENTITY_V2_ENABLED true RISK_IDENTITY_IP_COLLECTION_ENABLED true \
        RISK_IDENTITY_DEVICE_COLLECTION_ENABLED false RISK_IDENTITY_ADMIN_ENABLED false RISK_IDENTITY_RULES_ENABLED false \
        || return 1
      set_env_value "$target" RISK_IDENTITY_DEVICE_COLLECTION_ENABLED true
      ;;
    stage2-admin)
      require_flags RISK_IDENTITY_V2_ENABLED true RISK_IDENTITY_IP_COLLECTION_ENABLED true \
        RISK_IDENTITY_DEVICE_COLLECTION_ENABLED true RISK_IDENTITY_ADMIN_ENABLED false \
        RISK_IDENTITY_RULES_ENABLED false RISK_IDENTITY_IP_RULES_ENABLED false \
        RISK_IDENTITY_DEVICE_RULES_ENABLED false RISK_IDENTITY_COMPOSITE_RULES_ENABLED false \
        || return 1
      set_env_value "$target" RISK_IDENTITY_ADMIN_ENABLED true
      ;;
    stage3-shadow-window)
      require_flags RISK_IDENTITY_V2_ENABLED true RISK_IDENTITY_IP_COLLECTION_ENABLED true \
        RISK_IDENTITY_DEVICE_COLLECTION_ENABLED true RISK_IDENTITY_ADMIN_ENABLED true \
        RISK_IDENTITY_RULES_ENABLED false RISK_IDENTITY_IP_RULES_ENABLED false \
        RISK_IDENTITY_DEVICE_RULES_ENABLED false RISK_IDENTITY_COMPOSITE_RULES_ENABLED false \
        || return 1
      [[ -z "$(env_value "$ENV_FILE" RISK_IDENTITY_SHADOW_UNTIL)" ]] || return 1
      shadow_until="$(date -u -d '+15 days' '+%Y-%m-%dT%H:%M:%SZ')" || return 1
      set_env_value "$target" RISK_IDENTITY_SHADOW_UNTIL "$shadow_until"
      ;;
    stage3-rules)
      require_flags RISK_IDENTITY_V2_ENABLED true RISK_IDENTITY_IP_COLLECTION_ENABLED true \
        RISK_IDENTITY_DEVICE_COLLECTION_ENABLED true RISK_IDENTITY_ADMIN_ENABLED true \
        RISK_IDENTITY_RULES_ENABLED false RISK_IDENTITY_IP_RULES_ENABLED false \
        RISK_IDENTITY_DEVICE_RULES_ENABLED false RISK_IDENTITY_COMPOSITE_RULES_ENABLED false \
        || return 1
      shadow_until="$(env_value "$ENV_FILE" RISK_IDENTITY_SHADOW_UNTIL)" || return 1
      [[ -n "$shadow_until" && "$(date -u -d "$shadow_until" +%s)" -ge "$(date -u -d '+14 days' +%s)" ]] || return 1
      set_env_value "$target" RISK_IDENTITY_RULES_ENABLED true || return 1
      set_env_value "$target" RISK_IDENTITY_IP_RULES_ENABLED true || return 1
      set_env_value "$target" RISK_IDENTITY_DEVICE_RULES_ENABLED true || return 1
      set_env_value "$target" RISK_IDENTITY_COMPOSITE_RULES_ENABLED true
      ;;
    *) return 1 ;;
  esac
}

mkdir -p "$DATA_DIR" "$(dirname "$LOG")"
release_ensure_backup_root || fail_prepare 'root-owned identity backup directory is unsafe' BACKUP_PATH_UNSAFE
touch "$LOG"
[[ "$(jq -r '.operation_kind // empty' "$JOB_FILE")" == update && "$(jq -r '.action // empty' "$JOB_FILE")" == prepare \
  && "$(jq -r '.update_kind // empty' "$JOB_FILE")" == identity-config ]] \
  || fail_prepare 'identity rollout job contract is invalid' INVALID_IDENTITY_OPERATION
IDENTITY_TRANSITION="$(jq -r '.identity_transition // empty' "$JOB_FILE")"
[[ "$IDENTITY_TRANSITION" =~ ^stage(1-(v2|ip|device)|2-admin|3-(shadow-window|rules))$ ]] \
  || fail_prepare 'identity rollout transition is invalid' INVALID_IDENTITY_TRANSITION

LEDGER_STATE_PATH="$(ledger_state_path)"
ledger_validate_state "$LEDGER_STATE_PATH" || fail_prepare 'release ledger state is invalid' LEDGER_INCONSISTENT
BASE_RELEASE_ID="$(jq -r '.current_release_id' "$LEDGER_STATE_PATH")"
BASE_CUSTOM_HIGH_WATER="$(jq -r '.custom_version_high_water' "$LEDGER_STATE_PATH")"
[[ "$(jq -r '.active_operation_id // empty' "$LEDGER_STATE_PATH")" == "$JOB_ID" ]] \
  || fail_prepare 'release ledger does not own this operation' LEDGER_OPERATION_DRIFT
BASE_RECORD_PATH="$(ledger_release_path "$BASE_RELEASE_ID")"
ledger_validate_release "$BASE_RECORD_PATH" || fail_prepare 'current release record is invalid' LEDGER_INCONSISTENT
BASE_RECORD="$(cat "$BASE_RECORD_PATH")"
CURRENT_OFFICIAL_VERSION="$(jq -r '.official_version' <<< "$BASE_RECORD")"
CURRENT_CUSTOM_VERSION="$(jq -r '.custom_version' <<< "$BASE_RECORD")"
CURRENT_CUSTOM_SEQUENCE="$(jq -r '.custom_version_sequence' <<< "$BASE_RECORD")"
PRODUCTION_COMMIT="$(jq -r '.custom_commit' <<< "$BASE_RECORD")"
STABLE_COMMIT="$(jq -r '.official_commit' <<< "$BASE_RECORD")"
CURRENT_MAIN_DIGEST="$(jq -r '.main_digest' <<< "$BASE_RECORD")"
CURRENT_EXTENSIONS_DIGEST="$(jq -r '.extensions_digest' <<< "$BASE_RECORD")"

jq -e --argjson record "$BASE_RECORD" '
  .release_id == $record.release_id and .production_commit == $record.custom_commit
  and .stable_release_tag == $record.official_version and .stable_release_commit == $record.official_commit
  and .main_digest == $record.main_digest and .extensions_digest == $record.extensions_digest
  and .official_version == $record.official_version and .custom_version == $record.custom_version
  and .custom_version_sequence == $record.custom_version_sequence
' "$PRODUCTION_RELEASE_STATE_FILE" >/dev/null || fail_prepare 'compatibility projection drifted from the ledger' LEDGER_PROJECTION_DRIFT
[[ "$(git -C "$REPO" rev-parse HEAD)" == "$PRODUCTION_COMMIT" && -z "$(git -C "$REPO" status --porcelain --untracked-files=all)" ]] \
  || fail_prepare 'production source is dirty or drifted' SOURCE_WORKTREE_DRIFT
[[ "$(sha256sum "$COMPOSE_BASE" | awk '{print $1}')" == "$(jq -r '.base_compose_sha256' <<< "$BASE_RECORD")" ]] \
  || fail_prepare 'production base Compose drifted' CURRENT_COMPOSE_DRIFT
[[ "$(sha256sum "$COMPOSE_CUSTOM" | awk '{print $1}')" == "$(jq -r '.custom_compose_sha256' <<< "$BASE_RECORD")" ]] \
  || fail_prepare 'production custom Compose drifted' CURRENT_COMPOSE_DRIFT
[[ "$(sha256sum "$ENV_FILE" | awk '{print $1}')" == "$(jq -r '.env_sha256' <<< "$BASE_RECORD")" ]] \
  || fail_prepare 'production environment drifted' CURRENT_ENV_DRIFT
release_env_matches_digest_pair "$ENV_FILE" "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
  || fail_prepare 'production digest pair drifted' CURRENT_DIGEST_DRIFT
CURRENT_RENDERED_JSON="$(mktemp "$DATA_DIR/.identity-current-compose-$JOB_ID.XXXXXX")"
release_render_explicit_compose "$COMPOSE_BASE" "$COMPOSE_CUSTOM" "$ENV_FILE" "$CURRENT_RENDERED_JSON" "$LOG" \
  || fail_prepare 'production Compose rendering failed' CURRENT_COMPOSE_DRIFT
release_validate_rendered_compose "$CURRENT_RENDERED_JSON" \
  "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
  || fail_prepare 'production Compose contract drifted' CURRENT_COMPOSE_DRIFT
[[ "$(sha256sum "$CURRENT_RENDERED_JSON" | awk '{print $1}')" == "$(jq -r '.rendered_compose_sha256' <<< "$BASE_RECORD")" ]] \
  || fail_prepare 'rendered production Compose drifted' CURRENT_COMPOSE_DRIFT
rm -f "$CURRENT_RENDERED_JSON"
CURRENT_RENDERED_JSON=''
release_verify_local_image_identity "$MAIN_REPOSITORY" "$CURRENT_MAIN_DIGEST" "$PRODUCTION_COMMIT" "${CURRENT_OFFICIAL_VERSION#v}" \
  || fail_prepare 'main image identity drifted' CURRENT_IMAGE_DRIFT
release_verify_local_image_identity "$EXTENSIONS_REPOSITORY" "$CURRENT_EXTENSIONS_DIGEST" "$PRODUCTION_COMMIT" "${CURRENT_OFFICIAL_VERSION#v}" \
  || fail_prepare 'extension image identity drifted' CURRENT_IMAGE_DRIFT

STAMP="$(date -u '+%Y%m%dT%H%M%S%NZ')"
TARGET_RELEASE_ID="release-config-$STAMP-${PRODUCTION_COMMIT:0:9}"
BACKUP_DIR="$(mktemp -d "$BACKUP_ROOT/$JOB_ID-$STAMP.XXXXXX")"
mkdir -p "$BACKUP_DIR/target"
cp -p "$COMPOSE_BASE" "$BACKUP_DIR/target/docker-compose.yml"
cp -p "$COMPOSE_CUSTOM" "$BACKUP_DIR/target/docker-compose.custom.yml"
release_stage_target_env "$ENV_FILE" "$BACKUP_DIR/target/.env" \
  "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
  || fail_prepare 'target environment staging failed' TARGET_ENV_FAILED
apply_transition "$BACKUP_DIR/target/.env" "$IDENTITY_TRANSITION" \
  || fail_prepare 'identity transition prerequisites or secret validation failed' IDENTITY_TRANSITION_CONFLICT

release_job_update "$JOB_ID" rendering_compose 'Rendering the identity configuration target' '{}'
release_render_explicit_compose "$BACKUP_DIR/target/docker-compose.yml" "$BACKUP_DIR/target/docker-compose.custom.yml" \
  "$BACKUP_DIR/target/.env" "$BACKUP_DIR/target/rendered-compose.json" "$LOG" \
  || fail_prepare 'target Compose rendering failed' COMPOSE_INVALID
release_validate_rendered_compose "$BACKUP_DIR/target/rendered-compose.json" \
  "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" \
  || fail_prepare 'target Compose contract failed' COMPOSE_CONTRACT_INVALID

release_job_update "$JOB_ID" backing_up 'Backing up production before identity configuration rollout' "$(jq -n --arg dir "$BACKUP_DIR" '{backup_dir:$dir}')"
release_create_complete_backup "$BACKUP_DIR" "$JOB_ID" "$LOG" \
  || fail_prepare 'complete production backup validation failed' BACKUP_CONTRACT_FAILED

CURRENT_BASE_COMPOSE_SHA256="$(sha256sum "$BACKUP_DIR/docker-compose.yml" | awk '{print $1}')"
CURRENT_CUSTOM_COMPOSE_SHA256="$(sha256sum "$BACKUP_DIR/docker-compose.custom.yml" | awk '{print $1}')"
TARGET_BASE_COMPOSE_SHA256="$(sha256sum "$BACKUP_DIR/target/docker-compose.yml" | awk '{print $1}')"
TARGET_CUSTOM_COMPOSE_SHA256="$(sha256sum "$BACKUP_DIR/target/docker-compose.custom.yml" | awk '{print $1}')"
TARGET_RENDERED_COMPOSE_SHA256="$(sha256sum "$BACKUP_DIR/target/rendered-compose.json" | awk '{print $1}')"
TARGET_ENV_SHA256="$(sha256sum "$BACKUP_DIR/target/.env" | awk '{print $1}')"
TARGET_ARTIFACT_MANIFEST_SHA256="$(sha256sum "$BACKUP_DIR/target/SHA256SUMS" | awk '{print $1}')"
BACKUP_MANIFEST_SHA256="$(sha256sum "$BACKUP_DIR/SHA256SUMS" | awk '{print $1}')"
prepared_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
expires_at="$(date -u -d '+60 minutes' '+%Y-%m-%dT%H:%M:%SZ')"
MANIFEST_DIR="$(release_prepare_manifest_dir "$JOB_ID")" \
  || fail_prepare 'root-owned prepared manifest directory is unsafe' PREPARED_PATH_UNSAFE
MANIFEST_SOURCE="$(mktemp "$MANIFEST_DIR/.identity-manifest-source.XXXXXX")" \
  || fail_prepare 'identity manifest temporary file could not be created' PREPARED_MANIFEST_INVALID
chmod 0600 "$MANIFEST_SOURCE" \
  || fail_prepare 'identity manifest temporary file permissions could not be secured' PREPARED_MANIFEST_INVALID
jq -n \
  --arg operation_kind update --arg update_kind identity-config --arg transition "$IDENTITY_TRANSITION" \
  --arg base_release_id "$BASE_RELEASE_ID" --argjson base_high_water "$BASE_CUSTOM_HIGH_WATER" \
  --arg target_release_id "$TARGET_RELEASE_ID" --arg current_official "$CURRENT_OFFICIAL_VERSION" --arg current_custom "$CURRENT_CUSTOM_VERSION" \
  --arg source_commit "$PRODUCTION_COMMIT" --arg stable_commit "$STABLE_COMMIT" \
  --arg main_digest "$CURRENT_MAIN_DIGEST" --arg extensions_digest "$CURRENT_EXTENSIONS_DIGEST" \
  --arg current_base_sha "$CURRENT_BASE_COMPOSE_SHA256" --arg current_custom_sha "$CURRENT_CUSTOM_COMPOSE_SHA256" \
	--arg current_env_sha "$(jq -r '.env_sha256' <<< "$BASE_RECORD")" \
  --arg target_base_sha "$TARGET_BASE_COMPOSE_SHA256" --arg target_custom_sha "$TARGET_CUSTOM_COMPOSE_SHA256" \
  --arg target_rendered_sha "$TARGET_RENDERED_COMPOSE_SHA256" --arg target_env_sha "$TARGET_ENV_SHA256" \
  --arg target_manifest_sha "$TARGET_ARTIFACT_MANIFEST_SHA256" --arg backup_dir "$BACKUP_DIR" \
  --arg backup_manifest_sha "$BACKUP_MANIFEST_SHA256" --arg prepared_at "$prepared_at" --arg expires_at "$expires_at" \
  '{schema_version:1,operation_kind:$operation_kind,update_kind:$update_kind,identity_transition:$transition,custom_docs_only:false,
    base_release_id:$base_release_id,base_custom_high_water:$base_high_water,target_release_id:$target_release_id,
    current_official_version:$current_official,current_custom_version:$current_custom,target_official_version:$current_official,
	target_custom_version:$current_custom,proposed_custom_sequence:($current_custom | capture("^v1\\.0\\.(?<n>[0-9]+)$").n | tonumber),advances_custom_version:false,
    source_commit:$source_commit,target_commit:$source_commit,target_custom_commit:$source_commit,
    stable_release_tag:$current_official,stable_release_commit:$stable_commit,
    main_digest:$main_digest,extensions_digest:$extensions_digest,current_main_digest:$main_digest,current_extensions_digest:$extensions_digest,
    current_base_compose_sha256:$current_base_sha,current_custom_compose_sha256:$current_custom_sha,
	current_env_sha256:$current_env_sha,
    target_base_compose_sha256:$target_base_sha,target_custom_compose_sha256:$target_custom_sha,
    target_rendered_compose_sha256:$target_rendered_sha,target_env_sha256:$target_env_sha,
    target_artifact_manifest_sha256:$target_manifest_sha,backup_dir:$backup_dir,backup_manifest_sha256:$backup_manifest_sha,
    prepared_at:$prepared_at,expires_at:$expires_at,workflow_url:"identity-config-local",images_verified:true,
    compose_contract:"deploy-explicit-pair-v1",backup_contract:"complete-paired-snapshot-v1"}' > "$MANIFEST_SOURCE"
release_install_manifest_files "$MANIFEST_DIR" "$MANIFEST_SOURCE" \
  || fail_prepare 'identity manifest files could not be installed safely' PREPARED_PATH_UNSAFE
rm -f "$MANIFEST_SOURCE"
MANIFEST_SOURCE=''
manifest_sha="$(awk '{print $1}' "$MANIFEST_DIR/manifest.sha256")"
operation_metadata="$(jq --arg manifest "$MANIFEST_DIR/manifest.json" --arg sha "$manifest_sha" \
  '. + {prepared_manifest:$manifest,prepared_manifest_sha256:$sha,published:false,production_changed:false}' "$MANIFEST_DIR/manifest.json")"
release_job_update "$JOB_ID" validating_backup 'Identity rollout backup and manifest validated' "$operation_metadata"
release_job_update "$JOB_ID" prepared 'Identity rollout prepared; administrator confirmation is required' "$operation_metadata"
