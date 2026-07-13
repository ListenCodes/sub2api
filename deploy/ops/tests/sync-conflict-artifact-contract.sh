#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SYNC_SCRIPT="$ROOT_DIR/deploy/ops/sync-upstream.sh"

grep -Fq 'conflict_snapshot_dir="$CONFLICT_DIR/$JOB_ID"' "$SYNC_SCRIPT"
grep -Fq 'CONFLICT_LOG="$conflict_snapshot_dir/metadata.json"' "$SYNC_SCRIPT"
if grep -Fq 'CONFLICT_LOG_PREFIX="${SUB2API_SYNC_CONFLICT_LOG_PREFIX:-/app/data/sync-conflicts}"' "$SYNC_SCRIPT"; then
  echo 'conflict artifact status must use the host snapshot path' >&2
  exit 1
fi

echo 'sync-conflict-artifact-contract=PASS'
