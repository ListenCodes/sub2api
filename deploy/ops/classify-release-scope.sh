#!/usr/bin/env bash
set -Eeuo pipefail

REPO="${SUB2API_REPO:-/root/sub2api}"
PRODUCTION_COMMIT="${1:-}"
TARGET_COMMIT="${2:-}"

fail() {
  printf 'Release scope classification failed: %s\n' "$1" >&2
  exit 1
}

[[ "$PRODUCTION_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail 'production commit must be a full SHA'
[[ "$TARGET_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail 'target commit must be a full SHA'
git -C "$REPO" rev-parse --verify "$PRODUCTION_COMMIT^{commit}" >/dev/null 2>&1 \
  || fail 'production commit is not available in the repository'
git -C "$REPO" rev-parse --verify "$TARGET_COMMIT^{commit}" >/dev/null 2>&1 \
  || fail 'target commit is not available in the repository'

mapfile -t changed_files < <(git -C "$REPO" diff --name-only "$PRODUCTION_COMMIT" "$TARGET_COMMIT")
docs_only=false
if ((${#changed_files[@]} > 0)); then
  docs_only=true
  for path in "${changed_files[@]}"; do
    case "$path" in
      *.md|AGENTS.md|*/AGENTS.md|.gitignore|*/.gitignore) ;;
      *) docs_only=false; break ;;
    esac
  done
fi

printf 'docs_only=%s\n' "$docs_only"
