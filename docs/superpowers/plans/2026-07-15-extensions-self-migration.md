# Extensions-Self Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move risk control and the existing custom homepage into one repository-owned `extensions-self` container without changing the risk database.

**Architecture:** The existing Go risk service moves under `extensions-self/risk-control` and serves sibling static homepage files copied into the image. Sub2API exposes a narrow same-origin homepage proxy while continuing to call signed risk APIs through `RISK_CONTROL_URL=http://extensions-self:8090`.

**Tech Stack:** Go 1.26.5, Gin, Docker Compose, Node test runner, HTML/CSS

---

### Task 1: Lock The Deployment Contract

**Files:**
- Modify: `deploy/tests/risk-control-alias.test.mjs`
- Create: `deploy/tests/extensions-self-layout.test.mjs`

- [ ] **Step 1: Write failing layout assertions**

Assert that `extensions-self/risk-control`, `extensions-self/homepage/index.html`,
and `extensions-self/Dockerfile` exist; that top-level `risk-control` does not;
and that Compose contains service/container `extensions-self`, image
`deploy-extensions-self`, and URL `http://extensions-self:8090`.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `node --test deploy/tests/extensions-self-layout.test.mjs`

Expected: failure because the new layout and service do not exist.

- [ ] **Step 3: Commit the contract test**

```bash
git add deploy/tests/extensions-self-layout.test.mjs deploy/tests/risk-control-alias.test.mjs
git commit -m "test(deploy): define extensions-self layout"
```

### Task 2: Move Risk Control And Serve The Homepage

**Files:**
- Move: `risk-control/*` to `extensions-self/risk-control/*`
- Move/replace: `risk-control/Dockerfile` to `extensions-self/Dockerfile`
- Create: `extensions-self/homepage/index.html`
- Modify: `extensions-self/risk-control/config.go`
- Modify: `extensions-self/risk-control/http.go`
- Modify: `extensions-self/risk-control/main.go`
- Create: `extensions-self/risk-control/homepage_test.go`

- [ ] **Step 1: Add failing homepage server tests**

Create a temporary homepage directory containing `index.html`, construct
`HTTPServer` with `HomepageDir`, then assert `GET /homepage/` returns the marker,
`HEAD /homepage/` succeeds, `POST /homepage/` returns 405, and `/healthz`
identifies `extensions-self`.

- [ ] **Step 2: Run the focused Go test and confirm it fails**

Run: `go test ./...` from `extensions-self/risk-control`.

Expected: compile failure because `Config.HomepageDir` and homepage routing do
not exist.

- [ ] **Step 3: Move the source and add static serving**

Add `HomepageDir string` to `Config`, loaded from
`EXTENSIONS_SELF_HOMEPAGE_DIR` with `/app/homepage` as the runtime default.
Handle `/homepage` and `/homepage/*` before signed `/api/v1/*` dispatch. Only
allow GET/HEAD, set `X-Content-Type-Options: nosniff`, and serve from the
configured directory. Health must fail when `homepage/index.html` is missing.

- [ ] **Step 4: Build one parent image**

The parent Dockerfile downloads the child Go module, builds the existing binary,
copies it as `/app/extensions-self`, copies `homepage/` to `/app/homepage`, and
runs as UID 10001 with one entrypoint.

- [ ] **Step 5: Migrate the current homepage unchanged**

Wrap the production HTML/CSS fragment in `<!doctype html>`, `<html lang="zh-CN">`,
UTF-8/viewport metadata, `<title>Sub2API</title>`, and `<body>`. Preserve its
styles, text, anchors, and visual hierarchy.

- [ ] **Step 6: Run risk-control tests**

Run: `go test ./...` from `extensions-self/risk-control`.

Expected: all tests pass.

- [ ] **Step 7: Commit the service migration**

```bash
git add -A risk-control extensions-self
git commit -m "refactor(extensions): move risk control and homepage"
```

### Task 3: Add The Same-Origin Homepage Proxy

**Files:**
- Modify: `backend/internal/service/risk_control_client.go`
- Create: `backend/internal/service/risk_control_client_homepage_test.go`
- Create: `backend/internal/handler/extensions_self_proxy.go`
- Create: `backend/internal/handler/extensions_self_proxy_test.go`
- Modify: `backend/internal/server/routes/auth.go`

- [ ] **Step 1: Write failing client and handler tests**

Use `httptest.Server` to assert only `/homepage/` assets are requested, GET/HEAD
responses preserve `Content-Type` and bounded cache headers, oversized bodies
are rejected, and unavailable upstreams return 503.

- [ ] **Step 2: Run focused backend tests and confirm they fail**

Run:

```bash
go test ./internal/service ./internal/handler ./internal/server/routes
```

Expected: compile failure because the homepage proxy methods do not exist.

- [ ] **Step 3: Implement the allowlisted proxy**

Add a client method that accepts only a cleaned relative homepage asset path,
performs GET/HEAD against `baseURL + /homepage/`, limits responses to 1 MiB, and
returns status/content type/cache control. Add an unauthenticated handler and
register GET/HEAD routes at `/api/v1/extensions-self/homepage/*path`.

- [ ] **Step 4: Run focused backend tests**

Run the same command from `backend` and expect all tests to pass.

- [ ] **Step 5: Commit the public proxy**

```bash
git add backend/internal/service backend/internal/handler backend/internal/server/routes/auth.go
git commit -m "feat(extensions): proxy the custom homepage"
```

### Task 4: Update Compose, Publishing, And Documentation

**Files:**
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/docker-compose.local.yml`
- Modify: `deploy/.env.example`
- Modify: `deploy/ops/publish-custom.sh`
- Modify: `deploy/ops/README.md`
- Modify: `deploy/RELEASE-RUNBOOK.md`
- Modify: `AGENTS.md`
- Modify: `extensions-self/risk-control/README.md`

- [ ] **Step 1: Run the deployment test and retain the failure**

Run: `node --test deploy/tests/extensions-self-layout.test.mjs`

Expected: failure on old service names and paths.

- [ ] **Step 2: Rename the Compose service without touching the database**

Change only the application service from `risk-control` to `extensions-self`,
build from `../extensions-self`, set image/container `deploy-extensions-self`
and `extensions-self`, and point `RISK_CONTROL_URL` at the new hostname. Keep
`risk-control-postgres`, its volume, credentials, and dependency unchanged.

- [ ] **Step 3: Make publishing migration-aware**

Back up metadata for both old and new container names, tag the currently running
extension image for rollback, build/recreate `sub2api extensions-self`, verify
`/healthz` and the public homepage, then remove the old `risk-control` container
only after success. Never include `risk-control-postgres` in `up`, `rm`, or
`down` commands.

- [ ] **Step 4: Update ownership and rollback documentation**

Document `extensions-self/`, the single-process container, public homepage URL,
first-release old-container retirement, inline-homepage backup, and explicit
database preservation.

- [ ] **Step 5: Run deployment contract tests**

Run:

```bash
node --test deploy/tests/risk-control-alias.test.mjs deploy/tests/extensions-self-layout.test.mjs
git diff --check
```

Expected: all tests pass and no whitespace errors.

- [ ] **Step 6: Commit deployment changes**

```bash
git add AGENTS.md deploy extensions-self/risk-control/README.md
git commit -m "fix(deploy): publish extensions-self as one unit"
```

### Task 5: Validate The Complete Migration

**Files:**
- Verify all changed files

- [ ] **Step 1: Run complete relevant tests**

Run deployment tests, `go test ./...` in both Go modules, backend formatting,
and `git diff --check`. Expected: zero failures.

- [ ] **Step 2: Render Compose and build the extension image**

Run `docker compose ... config --quiet`, build `extensions-self`, start it with
a disposable PostgreSQL service, and assert `/healthz` and `/homepage/` return
200. Expected: the database service name and volume remain unchanged.

- [ ] **Step 3: Perform visual smoke checks**

Render the migrated homepage at 1440x900 and 390x844. Compare it with the
current production page for missing text, broken links, horizontal overflow,
and console/network errors.

- [ ] **Step 4: Review migration references**

Run `rg -n "(^|/)risk-control|deploy-risk-control|container_name: risk-control"`
against deployment and ownership documents. Remaining matches must refer only
to the risk module, database, compatibility environment keys, or documented
rollback history.

- [ ] **Step 5: Commit verification fixes**

```bash
git add -A
git commit -m "test(extensions): verify unified migration"
```

### Task 6: Prepare Production Handoff

**Files:**
- Modify only if verification discovers release gaps

- [ ] **Step 1: Record the exact commit and test evidence**

Capture the feature commit, image build result, Compose rendering result, and
desktop/mobile screenshots.

- [ ] **Step 2: Keep production unchanged without release authorization**

Do not merge, push `custom`, change `home_content`, or recreate production
containers until the user explicitly authorizes publishing this migration.
