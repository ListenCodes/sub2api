#!/usr/bin/env bash
set -euo pipefail

# Scheduled jobs use the same conflict-gated publish flow as the admin trigger.
exec /opt/sub2api-custom/sync-and-publish.sh --scheduled
