#!/usr/bin/env bash
set -euo pipefail

# Scheduled jobs may check upstream, but they must not publish production.
exec /opt/sub2api-custom/sync-upstream.sh --scheduled
