#!/usr/bin/env bash
set -Eeuo pipefail
printf 'verify %s\n' "$*" >> "${BOOTSTRAP_COMMAND_LOG:?}"
[[ "${1:-}" == --no-pull ]] || { printf 'fixture requires registry-only verification\n' >&2; exit 1; }
shift
if [[ "${FAKE_INVALID_DIGESTS:-0}" == 1 ]]; then
  printf 'main_digest=mutable\nextensions_digest=mutable\n'
else
  printf 'main_digest=sha256:%064d\nextensions_digest=sha256:%064d\n' 1 2
fi
