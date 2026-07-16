# Extensions-Self Migration Design

## Goal

Move all currently custom-operated services into the repository-owned
`extensions-self/` namespace. The existing risk-control API and the current
custom homepage will run from one `extensions-self` container while the
risk-control PostgreSQL service remains independent.

## Repository Layout

```text
extensions-self/
  Dockerfile
  homepage/
    index.html
  risk-control/
    go.mod
    go.sum
    *.go
    schema.sql
```

The existing top-level `risk-control/` directory moves without a functional
rewrite into `extensions-self/risk-control/`. The parent Dockerfile builds the
Go service and copies the homepage files into the runtime image.

## Runtime Architecture

The Compose service, container, and network hostname are all named
`extensions-self`. The image is named `deploy-extensions-self`. One Go process
continues to serve the signed risk-control APIs on port 8090 and also serves
the static homepage under `/homepage/` without internal-signature
authentication.

The main application continues to use the existing `RISK_CONTROL_URL`
configuration key for compatibility, but its value becomes
`http://extensions-self:8090`. Renaming the application-side client is outside
this migration because it would create unrelated code churn.

The main application exposes a narrow public proxy at
`/api/v1/extensions-self/homepage/`. It permits only `GET` and `HEAD`, forwards
only the homepage path, applies a response-size limit, and does not expose any
risk-control API. The configured iframe URL is therefore
`https://sub.ailisten.top/api/v1/extensions-self/homepage/`.

## Homepage Migration

The current production `home_content` is preserved visually. Its HTML/CSS
fragment is wrapped in a complete HTML document and committed as
`extensions-self/homepage/index.html`. No redesign or new interaction is part
of this change.

After the new endpoint is healthy, the production `home_content` setting can
be changed from inline HTML to the absolute iframe URL. The previous inline
value must be included in the release backup before that database update.

## Deployment And Rollback

The publisher builds `deploy-extensions-self`, recreates `sub2api` and
`extensions-self`, and checks the main health endpoint, extension health
endpoint, and public homepage endpoint. It never recreates or removes
`risk-control-postgres`.

During the first release, the existing `risk-control` container is retained
until `extensions-self` is healthy. It is removed only after all checks pass.
Rollback restores the matching Compose/configuration backup, the previous
inline `home_content`, and the recorded application and extension images.

## Failure Handling

- A missing homepage file makes the extension health check fail.
- An unavailable extension returns `503` through the public homepage proxy.
- Unsupported public methods return `405`.
- Traversal or non-homepage paths are rejected and never forwarded.
- A failed build or health check leaves the old `risk-control` container and
  database untouched.

## Verification

- Go tests cover public homepage serving without signatures and unchanged
  signed risk-control behavior.
- Backend tests cover proxy path allowlisting, response headers, size limits,
  and unavailable-upstream behavior.
- Deployment contract tests ensure the old top-level source path and old
  runtime service name are no longer active.
- Docker Compose rendering, image builds, container health, the public iframe
  URL, and desktop/mobile rendering are checked before production release.

## Non-Goals

- Redesigning the homepage.
- Renaming `RISK_CONTROL_*` configuration keys or database objects.
- Moving `risk-control-postgres` or its volume.
- Creating a separate Git repository.
