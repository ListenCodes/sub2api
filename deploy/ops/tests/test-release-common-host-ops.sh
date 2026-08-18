#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
FIXTURE_REPO="$TMP_DIR/repository"
INSTALL_ROOT="$TMP_DIR/installed"
mkdir -p "$FIXTURE_REPO/deploy/ops" "$INSTALL_ROOT"

git init -q "$FIXTURE_REPO"
git -C "$FIXTURE_REPO" config user.name 'Host Ops Fixture'
git -C "$FIXTURE_REPO" config user.email 'host-ops-fixture@example.com'
printf '#!/usr/bin/env bash\nprintf "ok\\n"\n' > "$FIXTURE_REPO/deploy/ops/example.sh"
printf '.check_runs\n' > "$FIXTURE_REPO/deploy/ops/actions-check-result.jq"
git -C "$FIXTURE_REPO" add deploy/ops
git -C "$FIXTURE_REPO" commit -q -m 'host ops fixture'
COMMIT="$(git -C "$FIXTURE_REPO" rev-parse HEAD)"

install -m 0755 "$FIXTURE_REPO/deploy/ops/example.sh" "$INSTALL_ROOT/example.sh"
install -m 0644 "$FIXTURE_REPO/deploy/ops/actions-check-result.jq" "$INSTALL_ROOT/actions-check-result.jq"
source "$ROOT_DIR/deploy/ops/release-common.sh"

release_validate_installed_ops_at_commit "$FIXTURE_REPO" "$COMMIT" "$INSTALL_ROOT"

printf 'tampered\n' >> "$INSTALL_ROOT/example.sh"
if release_validate_installed_ops_at_commit "$FIXTURE_REPO" "$COMMIT" "$INSTALL_ROOT"; then
  printf 'tampered host script unexpectedly passed validation\n' >&2
  exit 1
fi
install -m 0755 "$FIXTURE_REPO/deploy/ops/example.sh" "$INSTALL_ROOT/example.sh"

rm -f "$INSTALL_ROOT/actions-check-result.jq"
if release_validate_installed_ops_at_commit "$FIXTURE_REPO" "$COMMIT" "$INSTALL_ROOT"; then
  printf 'missing host contract unexpectedly passed validation\n' >&2
  exit 1
fi
install -m 0644 "$FIXTURE_REPO/deploy/ops/actions-check-result.jq" "$INSTALL_ROOT/actions-check-result.jq"

printf '#!/usr/bin/env bash\n' > "$INSTALL_ROOT/health-monitor.sh"
chmod 0755 "$INSTALL_ROOT/health-monitor.sh"
release_validate_installed_ops_at_commit "$FIXTURE_REPO" "$COMMIT" "$INSTALL_ROOT"

printf '#!/usr/bin/env bash\n' > "$INSTALL_ROOT/obsolete.sh"
chmod 0755 "$INSTALL_ROOT/obsolete.sh"
if release_validate_installed_ops_at_commit "$FIXTURE_REPO" "$COMMIT" "$INSTALL_ROOT"; then
  printf 'obsolete host script unexpectedly passed validation\n' >&2
  exit 1
fi
rm -f "$INSTALL_ROOT/obsolete.sh"

chmod 0644 "$INSTALL_ROOT/example.sh"
if [[ "$(stat -c '%a' "$INSTALL_ROOT/example.sh")" == 644 ]]; then
  if release_validate_installed_ops_at_commit "$FIXTURE_REPO" "$COMMIT" "$INSTALL_ROOT"; then
    printf 'wrong host script mode unexpectedly passed validation\n' >&2
    exit 1
  fi
fi
chmod 0755 "$INSTALL_ROOT/example.sh"

mv "$INSTALL_ROOT/example.sh" "$INSTALL_ROOT/example.real"
if ln -s "$INSTALL_ROOT/example.real" "$INSTALL_ROOT/example.sh" 2>/dev/null \
  && [[ -L "$INSTALL_ROOT/example.sh" ]]; then
  if release_validate_installed_ops_at_commit "$FIXTURE_REPO" "$COMMIT" "$INSTALL_ROOT"; then
    printf 'symlinked host script unexpectedly passed validation\n' >&2
    exit 1
  fi
  rm -f "$INSTALL_ROOT/example.sh"
  mv "$INSTALL_ROOT/example.real" "$INSTALL_ROOT/example.sh"
else
  rm -f "$INSTALL_ROOT/example.sh"
  mv "$INSTALL_ROOT/example.real" "$INSTALL_ROOT/example.sh"
fi

release_validate_installed_ops_at_commit "$FIXTURE_REPO" "$COMMIT" "$INSTALL_ROOT"

mkdir -p "$TMP_DIR/failing-bin"
printf '#!/usr/bin/env bash\nexit 23\n' > "$TMP_DIR/failing-bin/find"
chmod +x "$TMP_DIR/failing-bin/find"
if PATH="$TMP_DIR/failing-bin:$PATH" release_validate_installed_ops_at_commit "$FIXTURE_REPO" "$COMMIT" "$INSTALL_ROOT"; then
  printf 'failed host file enumeration unexpectedly passed validation\n' >&2
  exit 1
fi
printf 'release-common host ops fixture: PASS\n'
