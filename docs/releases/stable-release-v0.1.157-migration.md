# Stable Release v0.1.157 Reconstruction and v0.1.158 Integration Evidence

## Scope

- Production publication: false
- Source branch preserved: `origin/custom`
- Target branch: `custom-release`
- Reconstruction Release: `v0.1.157`
- Current stable Release: `v0.1.158`

## Immutable References

| Reference | SHA |
|---|---|
| Release tag object | `a44e63f9fab426ec181bafcf4e4c1a002bbcb8e0` |
| Release peeled commit | `a2779cd5f30d6d3904a9d59088aed09507678dfe` |
| Custom upstream base | `09c6c6d74050cf49ed2fb864be6c11647798ef53` |
| Preserved custom source | `8b4901991f976e39d1aab76b74eb67fb771543ee` |

## Current Stable Release

| Reference | Value |
|---|---|
| Release | `v0.1.158` |
| Release tag object | `c6ece7d092843c19a2d14d1264669c6416969f6d` |
| Release peeled commit | `26abd19a2812edba02bbef93c3e2a620141cc257` |
| Published at | `2026-07-16T12:37:06Z` |
| Local merge commit | `fd0c15896` |

`v0.1.158` is a descendant of `v0.1.157`. It was merged after the custom
reconstruction with no conflicts. The only explicit merge-side metadata edit
was `deploy/stable-release-baseline.json`; high-risk official/custom overlap
kept both the official v0.1.158 constructor changes and the self-hosted risk
wiring.

## Custom Delta

- Patch: `E:\Code\sub2api-worktrees\custom-release.patch`
- SHA-256: `822120BB1DBB2CAE2E7D4AF52D04A79902CCC4E19CD3F512573936B61E5C84CE`
- Manifest: `E:\Code\sub2api-worktrees\custom-release-files.txt`
- Files in manifest: 241
- Apply method: `git apply --3way --index`

## Ownership Boundary

- Official baseline: the peeled `v0.1.158` commit, including its upstream
  routing, scheduler, audit, step-up, asynchronous image, and cooldown behavior.
- Self-hosted extensions: risk control, account/group monitoring, the custom
  homepage, their native UI/proxy routes, and the `riskEvents` gateway wiring.
- Compatibility fixes may adjust self-hosted extension calls or middleware
  registration inside official-path files, but must not change unrelated
  official handlers, routing decisions, or cooldown semantics.

## Initial Conflicts

The three-way application produced eight conflicted files:

1. `.gitignore`
2. `backend/cmd/server/wire_gen.go`
3. `backend/internal/server/routes/gateway.go`
4. `frontend/src/api/admin/index.ts`
5. `frontend/src/i18n/locales/en/admin/index.ts`
6. `frontend/src/i18n/locales/en/common.ts`
7. `frontend/src/i18n/locales/zh/admin/index.ts`
8. `frontend/src/i18n/locales/zh/common.ts`

Each conflict was resolved against the `v0.1.157` source before staging:

- `.gitignore` keeps the stable documentation allowlist and adds only the
  custom extension and monitor documents.
- `wire_gen.go` keeps the stable six-argument `NewUserHandler` constructor and
  adds the custom auth/risk-control wiring calls.
- `gateway.go` keeps the stable audit, step-up, Codex, and asynchronous image
  routes while registering the custom risk middleware on supported gateways.
- the admin API and locale barrels keep the stable audit exports and add the
  custom user-risk and account-monitor exports.
- the English and Chinese common locales keep the stable navigation keys and
  add the custom risk/account-monitor navigation keys.

No implementation newer than the peeled `v0.1.157` commit was used to resolve
these overlaps.

## Validation

| Command | Result | Evidence |
|---|---|---|
| Baseline JSON parse and tag verification | PASS | `deploy/stable-release-baseline.json` matches tag object and peeled commit |
| Frontend dependency install with pnpm 10.26.2 | PASS | Frozen lockfile installed 974 packages |
| Frontend `pnpm typecheck` on pristine v0.1.157 | PASS | `vue-tsc --noEmit` exited 0 |
| Frontend `pnpm typecheck` after custom transplant | PASS | `vue-tsc --noEmit` exited 0 with all conflict resolutions staged |
| `git diff --cached --check` after custom transplant | PASS | No whitespace errors |
| Stable Release ancestry check | PASS | Peeled `v0.1.157` commit is an ancestor of the reconstruction branch |
| Backend handler/service/routes focused Go tests | PASS | Portable verified Go 1.26.5; custom constructor compatibility fixed and focused packages passed |
| v0.1.158 ancestry and tag verification | PASS | `v0.1.157` is an ancestor; tag object and peeled commit match GitHub Release metadata |
| v0.1.158 backend handler/service/routes tests | PASS | All focused packages passed after the Release merge |
| v0.1.158 frontend typecheck | PASS | `vue-tsc --noEmit` exited 0 |
| v0.1.158 frontend full test suite | PASS | `pnpm test:run` exited 0 |
| Self-hosted risk-control tests | PASS | `go test ./... -count=1` exited 0 |
| Self-hosted account-monitor tests | PASS | `go test ./... -count=1` exited 0 |
| Release operations contracts | PASS | PowerShell contract, resolver fixtures, conflict artifact contract, and Bash syntax checks passed |
| Docker baseline validation | NOT RUN | Docker CLI is not installed in the local PowerShell environment |

## Final State

- Reconstruction commit: recorded by the custom transplant commit containing this evidence
- Current stable baseline: `v0.1.158`
- `origin/custom-release`: not pushed
- Production published: false
- Rollback performed: false
