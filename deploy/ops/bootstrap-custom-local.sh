#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="${CUSTOM_LOCAL_ENV_FILE:-$DEPLOY_DIR/.env.local}"
DOCKER_BIN="${CUSTOM_LOCAL_DOCKER_BIN:-docker}"

if [[ -e "$ENV_FILE" ]]; then
  printf 'Refusing to overwrite existing custom local environment: %s\n' "$ENV_FILE" >&2
  exit 1
fi

generate_secret() {
  openssl rand -hex 32
}

POSTGRES_PASSWORD="${CUSTOM_LOCAL_POSTGRES_PASSWORD:-$(generate_secret)}"
JWT_SECRET="${CUSTOM_LOCAL_JWT_SECRET:-$(generate_secret)}"
TOTP_ENCRYPTION_KEY="${CUSTOM_LOCAL_TOTP_ENCRYPTION_KEY:-$(generate_secret)}"
RISK_CONTROL_INTERNAL_SECRET="${CUSTOM_LOCAL_RISK_CONTROL_INTERNAL_SECRET:-$(generate_secret)}"
RISK_CONTROL_POSTGRES_PASSWORD="${CUSTOM_LOCAL_RISK_CONTROL_POSTGRES_PASSWORD:-$(generate_secret)}"

mkdir -p "$(dirname "$ENV_FILE")"
umask 077
TEMP_ENV="$(mktemp "${ENV_FILE}.tmp.XXXXXX")"
trap 'rm -f "$TEMP_ENV"' EXIT

{
  printf 'POSTGRES_PASSWORD=%s\n' "$POSTGRES_PASSWORD"
  printf 'JWT_SECRET=%s\n' "$JWT_SECRET"
  printf 'TOTP_ENCRYPTION_KEY=%s\n' "$TOTP_ENCRYPTION_KEY"
  printf 'RISK_CONTROL_INTERNAL_SECRET=%s\n' "$RISK_CONTROL_INTERNAL_SECRET"
  printf 'RISK_CONTROL_POSTGRES_PASSWORD=%s\n' "$RISK_CONTROL_POSTGRES_PASSWORD"
} >"$TEMP_ENV"
chmod 600 "$TEMP_ENV"
mv "$TEMP_ENV" "$ENV_FILE"
trap - EXIT

"$DOCKER_BIN" compose \
  -f "$DEPLOY_DIR/docker-compose.local.yml" \
  -f "$DEPLOY_DIR/docker-compose.custom.local.yml" \
  --env-file "$ENV_FILE" \
  up -d >/dev/null

printf 'Custom local environment created at %s\n' "$ENV_FILE"
printf 'Generated 5 secret values; values were not printed\n'
