#!/usr/bin/env bash
set -Eeuo pipefail

MAIN_REPOSITORY="${SUB2API_MAIN_IMAGE_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_IMAGE_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"
SOURCE_LABEL='https://github.com/ListenCodes/sub2api'
ANONYMOUS_DOCKER_CONFIG="$(mktemp -d)"
printf '{"auths":{}}\n' > "$ANONYMOUS_DOCKER_CONFIG/config.json"
chmod 0700 "$ANONYMOUS_DOCKER_CONFIG"
chmod 0600 "$ANONYMOUS_DOCKER_CONFIG/config.json"
trap 'rm -rf "$ANONYMOUS_DOCKER_CONFIG"' EXIT

fail() {
  printf 'Release image verification failed: %s\n' "$1" >&2
  exit 1
}

anonymous_docker() {
  DOCKER_CONFIG="$ANONYMOUS_DOCKER_CONFIG" docker "$@"
}

[[ "${1:-}" =~ ^[0-9a-f]{40}$ ]] || fail 'commit must be a full SHA'
[[ "${2:-}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'version must be stable semver without v prefix'
COMMIT="$1"
VERSION="$2"

verify_image() {
  local repository="$1"
  local tag="$repository:custom-$COMMIT"
  local inspect_output digest labels architecture repo_digests canonical

  inspect_output="$(anonymous_docker buildx imagetools inspect "$tag")" || fail "could not inspect public manifest $tag"
  digest="$(awk '$1 == "Digest:" {print $2; exit}' <<< "$inspect_output")"
  [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || fail "manifest digest is invalid for $tag"
  grep -Eq 'Platform:[[:space:]]*linux/amd64([[:space:]]|$)' <<< "$inspect_output" \
    || fail "linux/amd64 manifest is missing for $tag"

  anonymous_docker pull "$tag" >/dev/null || fail "anonymous pull failed for $tag"
  labels="$(docker image inspect "$tag" --format '{{json .Config.Labels}}')" || fail "could not inspect labels for $tag"
  [[ "$(jq -r '.["org.opencontainers.image.revision"] // empty' <<< "$labels")" == "$COMMIT" ]] \
    || fail "revision label mismatch for $tag"
  [[ "$(jq -r '.["org.opencontainers.image.version"] // empty' <<< "$labels")" == "$VERSION" ]] \
    || fail "version label mismatch for $tag"
  [[ "$(jq -r '.["org.opencontainers.image.source"] // empty' <<< "$labels")" == "$SOURCE_LABEL" ]] \
    || fail "source label mismatch for $tag"

  architecture="$(docker image inspect "$tag" --format '{{.Architecture}}')" || fail "could not inspect architecture for $tag"
  [[ "$architecture" == amd64 ]] || fail "local image architecture mismatch for $tag"
  repo_digests="$(docker image inspect "$tag" --format '{{json .RepoDigests}}')" || fail "could not inspect RepoDigests for $tag"
  canonical="$repository@$digest"
  jq -e --arg canonical "$canonical" 'index($canonical) != null' <<< "$repo_digests" >/dev/null \
    || fail "local RepoDigest does not match registry digest for $tag"

  printf '%s\n' "$digest"
}

main_digest="$(verify_image "$MAIN_REPOSITORY")"
extensions_digest="$(verify_image "$EXTENSIONS_REPOSITORY")"
printf 'main_digest=%s\nextensions_digest=%s\n' "$main_digest" "$extensions_digest"
