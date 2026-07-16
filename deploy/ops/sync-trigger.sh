#!/bin/sh
set -eu

STATUS_FILE=/app/data/sync-status
JOB_ID_FILE=/app/data/sync-job-id
TRIGGER_FILE=/app/data/sync-trigger
RESULT_FILE=/app/data/sync-result

job_id=$(cat "$JOB_ID_FILE" 2>/dev/null || true)
if [ -z "$job_id" ]; then
  echo "sync job id is missing" >&2
  exit 1
fi

now=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
started_at=$(sed -n 's/.*"started_at":"\([^"]*\)".*/\1/p' "$STATUS_FILE" 2>/dev/null || true)
[ -n "$started_at" ] || started_at=$now
tmp_file="$STATUS_FILE.tmp.$$"
printf '{"job_id":"%s","status":"running","message":"sync triggered","ts":"%s","started_at":"%s","finished_at":null,"integration_branch":"","base_commit":"","release_tag":"","release_commit":"","release_published_at":"","conflict_files":[],"conflict_base":"","conflict_upstream":"","conflict_release":"","conflict_log":"","resolution_hint":"","need_restart":false,"published":false,"published_commit":""}\n' \
  "$job_id" "$now" "$started_at" > "$tmp_file"
mv -f "$tmp_file" "$STATUS_FILE"

rm -f "$RESULT_FILE"
touch "$TRIGGER_FILE"
echo "sync triggered: $job_id"

for i in $(seq 1 120); do
  sleep 5
  if [ -f "$RESULT_FILE" ]; then
    content=$(cat "$RESULT_FILE")
    rm -f "$RESULT_FILE"
    case "$content" in
      FAILED*)
        echo "$content" >&2
        exit 1
        ;;
      *)
        echo "$content"
        exit 0
        ;;
    esac
  fi
done

echo "TIMEOUT: upstream sync did not complete within 10 minutes" >&2
exit 1
