#!/usr/bin/env bash
set -Eeuo pipefail

printf '%s\n' 'publish-custom.sh is deprecated and cannot be used as a release entry point.' >&2
printf '%s\n' 'Use prepare-release.sh followed by an explicit apply-release.sh confirmation.' >&2
exit 64
