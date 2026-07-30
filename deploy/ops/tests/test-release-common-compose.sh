#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
  printf 'release common compose test failed: %s\n' "$1" >&2
  exit 1
}

render_fixture() {
  local output="$1"
  shift
  local targets='[]'
  local target

  for target in "$@"; do
    targets="$(jq -c --arg target "${target#/}" '
      ("/" + $target) as $full |
      . + [{
        target:$full,
        source:(
          if $full == "/app/data" then "sub2api_data"
          elif $full == "/app/scripts/sync-upstream.sh" then "/opt/sub2api-custom/sync-trigger.sh"
          else "fixture"
          end
        ),
        type:(if $full == "/app/data" then "volume" else "bind" end),
        read_only:($full == "/app/scripts/sync-upstream.sh")
      }]
    ' <<< "$targets")"
  done

  jq -n --arg main "$MAIN_IMAGE" --arg ext "$EXTENSIONS_IMAGE" --argjson targets "$targets" '{
    name:"deploy",
    services:{
      sub2api:{
        image:$main,
        healthcheck:{test:["CMD","true"]},
        volumes:$targets,
        networks:{"sub2api-network":{}}
      },
      "extensions-self":{
        image:$ext,
        healthcheck:{test:["CMD","true"]},
        networks:{"sub2api-network":{}}
      },
      postgres:{},
      redis:{},
      "risk-control-postgres":{}
    },
    volumes:{
      sub2api_data:{},
      postgres_data:{},
      redis_data:{},
      risk_control_postgres_data:{}
    }
  }' > "$output"
}

expect_valid() {
  local fixture="$1" message="$2"
  release_validate_rendered_compose "$fixture" "$MAIN_IMAGE" "$EXTENSIONS_IMAGE" \
    || fail "$message"
}

expect_invalid() {
  local fixture="$1" message="$2"
  if release_validate_rendered_compose "$fixture" "$MAIN_IMAGE" "$EXTENSIONS_IMAGE"; then
    fail "$message"
  fi
}

export SUB2API_DATA_DIR="$TMP_DIR/data"
source "$ROOT_DIR/deploy/ops/release-common.sh"

MAIN_IMAGE="ghcr.io/listencodes/sub2api-custom@sha256:$(printf '1%.0s' {1..64})"
EXTENSIONS_IMAGE="ghcr.io/listencodes/sub2api-extensions@sha256:$(printf '2%.0s' {1..64})"

render_fixture "$TMP_DIR/legacy.json" \
  /app/data /app/scripts/sync-upstream.sh /repo /var/run/docker.sock /usr/bin/docker
render_fixture "$TMP_DIR/reduced.json" \
  /app/data /app/scripts/sync-upstream.sh
jq '(.services.sub2api.volumes[] | select(.target == "/app/data")) |=
  (.source = "/etc" | .type = "bind")' \
  "$TMP_DIR/reduced.json" > "$TMP_DIR/data-bind.json"
jq '(.services.sub2api.volumes[] | select(.target == "/app/data")).read_only = true' \
  "$TMP_DIR/reduced.json" > "$TMP_DIR/data-read-only.json"
render_fixture "$TMP_DIR/hybrid-repo.json" \
  /app/data /app/scripts/sync-upstream.sh /repo
render_fixture "$TMP_DIR/hybrid-docker.json" \
  /app/data /app/scripts/sync-upstream.sh /var/run/docker.sock

expect_invalid "$TMP_DIR/legacy.json" 'legacy compose was accepted'
expect_valid "$TMP_DIR/reduced.json" 'reduced compose was rejected'
expect_invalid "$TMP_DIR/data-bind.json" 'host bind was accepted for application data'
expect_invalid "$TMP_DIR/data-read-only.json" 'read-only named volume was accepted for application data'
expect_invalid "$TMP_DIR/hybrid-repo.json" 'repo-only hybrid compose was accepted'
expect_invalid "$TMP_DIR/hybrid-docker.json" 'Docker-only hybrid compose was accepted'

printf 'release common compose fixture tests: PASS\n'
