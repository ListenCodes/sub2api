# Stable Release v0.1.157 Migration Evidence

## Scope

- Production publication: false
- Source branch preserved: `origin/custom`
- Target branch: `custom-release`
- Stable Release: `v0.1.157`

## Immutable References

| Reference | SHA |
|---|---|
| Release tag object | `a44e63f9fab426ec181bafcf4e4c1a002bbcb8e0` |
| Release peeled commit | `a2779cd5f30d6d3904a9d59088aed09507678dfe` |
| Custom upstream base | `09c6c6d74050cf49ed2fb864be6c11647798ef53` |
| Preserved custom source | `8b4901991f976e39d1aab76b74eb67fb771543ee` |

## Custom Delta

- Patch: `E:\Code\sub2api-worktrees\custom-release.patch`
- SHA-256: `822120BB1DBB2CAE2E7D4AF52D04A79902CCC4E19CD3F512573936B61E5C84CE`
- Manifest: `E:\Code\sub2api-worktrees\custom-release-files.txt`
- Files in manifest: 241
- Apply method: `git apply --3way --index`

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
| Go baseline validation | NOT RUN | Go is not installed in the local PowerShell environment |
| Docker baseline validation | NOT RUN | Docker CLI is not installed in the local PowerShell environment |

## Final State

- Reconstruction commit: recorded by the custom transplant commit containing this evidence
- `origin/custom-release`: not pushed
- Production published: false
- Rollback performed: false
