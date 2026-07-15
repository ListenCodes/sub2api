#!/usr/bin/env bash
set -Eeuo pipefail

FROM=''
TO=''
RECORD_DIR=''
ACTOR_ID=1
POLL_SECONDS="${ACCOUNT_MONITOR_BACKFILL_POLL_SECONDS:-5}"
MAX_POLLS="${ACCOUNT_MONITOR_BACKFILL_MAX_POLLS:-720}"

fail() {
  printf 'FAILED: %s\n' "$1" >&2
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --from) FROM="${2:-}"; shift 2 ;;
    --to) TO="${2:-}"; shift 2 ;;
    --record-dir) RECORD_DIR="${2:-}"; shift 2 ;;
    --actor-id) ACTOR_ID="${2:-}"; shift 2 ;;
    *) fail "unknown argument: $1" ;;
  esac
done

[[ -n "$FROM" && -n "$TO" && -n "$RECORD_DIR" ]] || fail 'usage: backfill-account-monitor.sh --from <RFC3339> --to <RFC3339> --record-dir <release-backup-dir> [--actor-id <id>]'
[[ "$ACTOR_ID" =~ ^[1-9][0-9]*$ ]] || fail 'actor ID must be a positive integer'
[[ -d "$RECORD_DIR" ]] || fail "record directory does not exist: $RECORD_DIR"
docker container inspect extensions-self >/dev/null 2>&1 || fail 'extensions-self container does not exist'

secret="$(docker exec extensions-self printenv RISK_CONTROL_INTERNAL_SECRET)" || fail 'could not read extensions-self signing secret'
[[ -n "$secret" ]] || fail 'extensions-self signing secret is empty'
jobs_file="$RECORD_DIR/backfill-jobs.tsv"
printf 'from\tto\tjob_id\tstatus\tprocessed_rows\terror\n' > "$jobs_file"

# Produce non-overlapping, contiguous segments with a strict 31-day maximum.
mapfile -t segments < <(FROM="$FROM" TO="$TO" python3 - <<'PY'
from datetime import datetime, timedelta, timezone
import os

def parse(value):
    parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    if parsed.tzinfo is None:
        raise SystemExit('timestamps must include a timezone')
    return parsed.astimezone(timezone.utc)

start = parse(os.environ['FROM'])
end = parse(os.environ['TO'])
if start >= end:
    raise SystemExit('from must be before to')
while start < end:
    segment_end = min(start + timedelta(days=31), end)
    print(start.isoformat().replace('+00:00', 'Z') + '\t' + segment_end.isoformat().replace('+00:00', 'Z'))
    start = segment_end
PY
)
[[ "${#segments[@]}" -gt 0 ]] || fail 'no backfill segments were generated'

signed_request() {
  local method="$1" path="$2" body="${3:-}" timestamp nonce signature
  timestamp="$(date +%s)"
  nonce="backfill-${timestamp}-${RANDOM}-${RANDOM}"
  signature="$(MONITOR_SECRET="$secret" MONITOR_TIMESTAMP="$timestamp" MONITOR_NONCE="$nonce" MONITOR_BODY="$body" python3 - <<'PY'
import hashlib, hmac, os
message = (os.environ['MONITOR_TIMESTAMP'] + '\n' + os.environ['MONITOR_NONCE'] + '\n' + os.environ.get('MONITOR_BODY', '')).encode()
print(hmac.new(os.environ['MONITOR_SECRET'].encode(), message, hashlib.sha256).hexdigest())
PY
)" || fail 'could not sign account monitor request'
  if [[ "$method" == POST ]]; then
    docker exec extensions-self wget -qO- -T 30 \
      --header="X-Risk-Timestamp: $timestamp" \
      --header="X-Risk-Nonce: $nonce" \
      --header="X-Risk-Signature: $signature" \
      --header="X-Risk-Actor-ID: $ACTOR_ID" \
      --header='Content-Type: application/json' \
      --post-data="$body" "http://extensions-self:8090$path"
  else
    docker exec extensions-self wget -qO- -T 30 \
      --header="X-Risk-Timestamp: $timestamp" \
      --header="X-Risk-Nonce: $nonce" \
      --header="X-Risk-Signature: $signature" \
      --header="X-Risk-Actor-ID: $ACTOR_ID" \
      "http://extensions-self:8090$path"
  fi
}

json_field() {
  JSON_INPUT="$1" JSON_FIELD="$2" python3 - <<'PY'
import json, os
value = json.loads(os.environ['JSON_INPUT'])
result = value.get(os.environ['JSON_FIELD'], '')
print('' if result is None else result)
PY
}

for segment in "${segments[@]}"; do
  IFS=$'\t' read -r segment_from segment_to <<< "$segment"
  body="$(FROM="$segment_from" TO="$segment_to" python3 - <<'PY'
import json, os
print(json.dumps({'from': os.environ['FROM'], 'to': os.environ['TO']}, separators=(',', ':')))
PY
)"
  response="$(signed_request POST /api/v1/admin/account-monitor/rebuild-jobs "$body")" || fail "could not submit segment $segment_from to $segment_to"
  job_id="$(json_field "$response" id)" || fail 'could not decode rebuild job ID'
  [[ "$job_id" =~ ^[1-9][0-9]*$ ]] || fail "invalid rebuild job response: $response"

  completed=false
  for ((poll=1; poll<=MAX_POLLS; poll++)); do
    response="$(signed_request GET "/api/v1/admin/account-monitor/rebuild-jobs/$job_id")" || fail "could not poll rebuild job $job_id"
    status="$(json_field "$response" status)"
    case "$status" in
      completed)
        processed_rows="$(json_field "$response" processed_rows)"
        printf '%s\t%s\t%s\tcompleted\t%s\t\n' "$segment_from" "$segment_to" "$job_id" "$processed_rows" >> "$jobs_file"
        completed=true
        break
        ;;
      failed)
        processed_rows="$(json_field "$response" processed_rows)"
        job_error="$(json_field "$response" error | tr '\t\r\n' '   ')"
        printf '%s\t%s\t%s\tfailed\t%s\t%s\n' "$segment_from" "$segment_to" "$job_id" "$processed_rows" "$job_error" >> "$jobs_file"
        fail "rebuild job $job_id failed: $job_error"
        ;;
      pending|running) sleep "$POLL_SECONDS" ;;
      *) fail "rebuild job $job_id returned unexpected status: $status" ;;
    esac
  done
  [[ "$completed" == true ]] || fail "rebuild job $job_id timed out"
done

quality_path="/api/v1/admin/account-monitor/data-quality?from=$FROM&to=$TO"
quality="$(signed_request GET "$quality_path")" || fail 'could not read post-backfill data quality'
printf '%s' "$quality" | python3 -m json.tool > "$RECORD_DIR/data-quality-after-backfill.json" || fail 'post-backfill data quality was not valid JSON'
{
  printf 'BACKFILL_STATUS=completed\n'
  printf 'BACKFILL_RANGE=%s..%s\n' "$FROM" "$TO"
  printf 'BACKFILL_SEGMENTS=%s\n' "${#segments[@]}"
} > "$RECORD_DIR/backfill-metadata.env"
sha256sum "$jobs_file" "$RECORD_DIR/data-quality-after-backfill.json" "$RECORD_DIR/backfill-metadata.env" > "$RECORD_DIR/BACKFILL-SHA256SUMS"
printf 'Backfill completed: range=%s..%s segments=%s records=%s\n' "$FROM" "$TO" "${#segments[@]}" "$jobs_file"
