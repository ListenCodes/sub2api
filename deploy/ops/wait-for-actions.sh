#!/usr/bin/env bash
set -Eeuo pipefail

REPOSITORY="${SUB2API_GITHUB_REPOSITORY:-ListenCodes/sub2api}"
POLL_SECONDS="${SUB2API_ACTIONS_POLL_SECONDS:-75}"
TIMEOUT_SECONDS="${SUB2API_ACTIONS_TIMEOUT_SECONDS:-5400}"
CHECKS_FIXTURE="${SUB2API_CHECKS_JSON_FILE:-}"
USER_AGENT='sub2api-release-actions-waiter/1'
EXPECTED_CHECKS='["backend","golangci","frontend","extensions","deployment","metadata","images"]'

fail() {
  printf 'Actions validation failed: %s\n' "$1" >&2
  exit 1
}

[[ "${1:-}" =~ ^[0-9a-f]{40}$ ]] || fail 'candidate commit must be a full SHA'
COMMIT="$1"

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
  checks_json="$(read_checks)" || fail 'GitHub checks API request failed'
  summary="$(jq -c --argjson expected "$EXPECTED_CHECKS" '
    .check_runs as $runs
    | [$expected[] as $name
      | ([$runs[] | select(.name == $name)] | last) as $run
      | if $run == null then
          {name:$name,status:"missing",conclusion:"",html_url:""}
        else
          {name:$name,status:$run.status,conclusion:($run.conclusion // ""),html_url:($run.html_url // "")}
        end]
  ' <<< "$checks_json")" || fail 'GitHub checks response was invalid'

  failed="$(jq -r '[.[] | select(.status == "completed" and .conclusion != "success")] | first | .name // empty' <<< "$summary")"
  [[ -z "$failed" ]] || fail "required check $failed did not succeed"

  complete_count="$(jq '[.[] | select(.status == "completed" and .conclusion == "success")] | length' <<< "$summary")"
  if [[ "$complete_count" -eq 7 ]]; then
    workflow_url="$(jq -r '.[] | select(.name == "images") | .html_url' <<< "$summary")"
    printf 'workflow_url=%s\n' "$workflow_url"
    exit 0
  fi

  [[ -z "$CHECKS_FIXTURE" ]] || fail 'fixture checks were incomplete'
  now="$(date +%s)"
  (( now - started < TIMEOUT_SECONDS )) || fail 'timed out waiting for required checks'
  sleep "$POLL_SECONDS"
done
