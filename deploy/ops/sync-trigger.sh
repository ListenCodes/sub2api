#!/bin/sh
set -eu

DATA_DIR=${SUB2API_DATA_DIR:-/app/data}
JOB_ID_FILE=$DATA_DIR/release-current-job-id
JOBS_DIR=$DATA_DIR/release-jobs
TRIGGER_FILE=$DATA_DIR/release-trigger

job_id=$(tr -d '[:space:]' < "$JOB_ID_FILE" 2>/dev/null || true)
if [ -z "$job_id" ]; then
  echo "release job id is missing" >&2
  exit 1
fi
case "$job_id" in
  update-*) job_suffix=${job_id#update-} ;;
  *) echo "release job id is invalid" >&2; exit 1 ;;
esac
case "$job_suffix" in
  ''|*[!A-Za-z0-9-]*) echo "release job id is invalid" >&2; exit 1 ;;
esac
[ "${#job_id}" -le 128 ] || { echo "release job id is invalid" >&2; exit 1; }
[ -f "$JOBS_DIR/$job_id.json" ] || { echo "release job file is missing" >&2; exit 1; }

mkdir -p "$DATA_DIR"
tmp_file="$TRIGGER_FILE.tmp.$$"
printf '%s\n' "$job_id" > "$tmp_file"
mv -f "$tmp_file" "$TRIGGER_FILE"
echo "release triggered: $job_id"
