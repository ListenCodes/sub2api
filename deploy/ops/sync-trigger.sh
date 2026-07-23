#!/bin/sh
set -eu

DATA_DIR=${SUB2API_DATA_DIR:-/app/data}
JOB_ID_FILE=$DATA_DIR/release-current-job-id
JOBS_DIR=${SUB2API_RELEASE_OPERATIONS_DIR:-$DATA_DIR/release-ledger/operations}
TRIGGER_FILE=$DATA_DIR/release-trigger

action=${1:-}
job_id=${2:-}
case "$action" in
  prepare|apply) ;;
  '') action='' ;;
  *)
    job_id="$action"
    action=''
    ;;
esac
if [ -z "$job_id" ]; then
  job_id=$(tr -d '[:space:]' < "$JOB_ID_FILE" 2>/dev/null || true)
fi
if [ -z "$job_id" ]; then
  echo "release job id is missing" >&2
  exit 1
fi
case "$job_id" in
  update-*) job_suffix=${job_id#update-} ;;
  rollback-*) job_suffix=${job_id#rollback-} ;;
  *) echo "release job id is invalid" >&2; exit 1 ;;
esac
case "$job_suffix" in
  ''|*[!A-Za-z0-9-]*) echo "release job id is invalid" >&2; exit 1 ;;
esac
[ "${#job_id}" -le 128 ] || { echo "release job id is invalid" >&2; exit 1; }
[ -f "$JOBS_DIR/$job_id.json" ] || { echo "release job file is missing" >&2; exit 1; }

if [ -z "$action" ]; then
  action=$(jq -r '.action // empty' "$JOBS_DIR/$job_id.json" 2>/dev/null || true)
fi
case "$action" in
  prepare|apply) ;;
  '')
    # Preserve the old trigger format for pre-two-phase jobs. The host
    # dispatcher fails these closed instead of reaching a publisher.
    action='legacy'
    ;;
  *) echo "release action is invalid" >&2; exit 1 ;;
esac

mkdir -p "$DATA_DIR"
tmp_file="$TRIGGER_FILE.tmp.$$"
if [ "$action" = legacy ]; then
  printf '%s\n' "$job_id" > "$tmp_file"
else
  printf '%s %s\n' "$action" "$job_id" > "$tmp_file"
fi
mv -f "$tmp_file" "$TRIGGER_FILE"
echo "release triggered: $job_id"
