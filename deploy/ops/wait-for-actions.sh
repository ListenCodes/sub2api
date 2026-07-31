#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPOSITORY="${SUB2API_GITHUB_REPOSITORY:-ListenCodes/sub2api}"
POLL_SECONDS="${SUB2API_ACTIONS_POLL_SECONDS:-75}"
TIMEOUT_SECONDS="${SUB2API_ACTIONS_TIMEOUT_SECONDS:-5400}"
CHECKS_FIXTURE="${SUB2API_CHECKS_JSON_FILE:-}"
RESULT_FILTER="${SUB2API_ACTIONS_RESULT_FILTER:-$SCRIPT_DIR/actions-check-result.jq}"
USER_AGENT='sub2api-release-actions-waiter/1'
EXPECTED_CHECKS='["backend","golangci","frontend","extensions","deployment","metadata","images"]'

fail() {
  local message="$1" code="${2:-ACTIONS_EVIDENCE_INVALID}"
  jq -cn --arg message "$message" --arg code "$code" '{
    ok:false,message:$message,error_code:$code,failed_check:"",check_url:"",
    conclusion:"",workflow_url:"",production_changed:false
  }'
  exit 1
}

[[ "${1:-}" =~ ^[0-9a-f]{40}$ ]] || fail 'candidate commit must be a full SHA' ACTIONS_INVALID_COMMIT
COMMIT="$1"
[[ -r "$RESULT_FILTER" ]] || fail 'Actions result filter is missing' ACTIONS_EVIDENCE_INVALID

read_checks() {
  if [[ -n "$CHECKS_FIXTURE" ]]; then
    cat -- "$CHECKS_FIXTURE"
    return
  fi
  curl -fsSL --retry 2 --connect-timeout 10 --max-time 30 \
    -H 'Accept: application/vnd.github+json' \
    -H 'X-GitHub-Api-Version: 2022-11-28' \
    -A "$USER_AGENT" \
    -- "https://api.github.com/repos/$REPOSITORY/commits/$COMMIT/check-runs?per_page=100"
}

started="$(date +%s)"
while true; do
  checks_json="$(read_checks)" || fail 'GitHub checks API request failed' ACTIONS_API_FAILED
  result="$(jq -c --argjson expected "$EXPECTED_CHECKS" -f "$RESULT_FILTER" <<< "$checks_json" 2>/dev/null)" \
    || fail 'GitHub checks response was invalid' ACTIONS_EVIDENCE_INVALID
  outcome="$(jq -r 'if .ok == true then "success" elif .ok == false then "failed" else "pending" end' <<< "$result")"

  if [[ "$outcome" == failed ]]; then
    printf '%s\n' "$result"
    exit 1
  fi
  if [[ "$outcome" == success ]]; then
    printf '%s\n' "$result"
    exit 0
  fi

  if [[ -n "$CHECKS_FIXTURE" ]]; then
    jq -c '.ok = false' <<< "$result"
    exit 1
  fi
  now="$(date +%s)"
  if (( now - started >= TIMEOUT_SECONDS )); then
    jq -c '.ok = false | .message = "timed out waiting for required checks: " + .message' <<< "$result"
    exit 1
  fi
  sleep "$POLL_SECONDS"
done
