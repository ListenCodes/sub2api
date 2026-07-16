# Account Monitor Production Release - 2026-07-16

This record captures the approved implementation, verification, production release, backfill,
reconciliation, and rollback state for the extensions center, user risk control, account monitor,
and group monitor release.

## Approved Commits

| Stage | Commit | Notes |
|---|---|---|
| Approved design | `a2ceec31` | Extensions center and account/group monitor design |
| Correction plan | `0ac48cbf3` | Fixed timelines, filters, paging, and detail behavior |
| Feature branch head | `638299f9f` | PostgreSQL-aligned monitor implementation and tests |
| Custom merge | `de6085443` | Feature merged into `custom` |
| Dependency correction | `76e48467a` | Declared the locale compiler test dependency |
| Final production commit | `e53138d3a` | Preserved safe-view column order during upgrades |

`origin/custom` and `/root/sub2api` both pointed to
`e53138d3ae747daa47239c6e39c42479b6512f58` when the release completed.

## Delivered Surface

- The expandable extensions center contains `用户风控`, `账号监控`, and `分组监控` as separate
  child routes.
- User risk control keeps its three native pages for users, scenario rules, and operation audit.
- Account and group monitoring use native Vue pages and the authenticated same-origin proxy. The
  retired account-monitor iframe and static page are absent.
- Account inventory includes zero-call accounts, multi-group memberships, group names, and rate
  multipliers. Account risk scores use the documented exact formula and shared badge.
- Group monitoring supports call-first ordering, `has_calls`, paging, filters, detail scrolling,
  and fixed 24-bucket `6h/24h/7d/30d` timelines.

## Verification

- Account-monitor and risk-control Go tests passed.
- The complete backend Go suite passed.
- Frontend Vitest passed `182` files and `1248` tests; typecheck, lint, and production build passed.
- Deploy and Compose contract tests passed `16/16`; `git diff --check` passed.
- PostgreSQL migration, idempotency, aggregation, retention, source-view upgrade, restricted-role,
  and rebuild tests passed against an isolated PostgreSQL instance.
- Independent review found no remaining Critical or Important issue after the unknown-model,
  PostgreSQL ordering, and safe-view upgrade corrections.
- Public and authenticated browser acceptance was completed. The operator confirmed the final
  production pages after release.

## Production Release

The only publish entrypoint used was:

```text
/opt/sub2api-custom/publish-custom.sh --commit e53138d3ae747daa47239c6e39c42479b6512f58
```

Successful release backup:

```text
/root/backups/sub2api/20260716-034320
```

The backup contains both PostgreSQL dumps, Compose, `.env`, Nginx, certificate/private-key copies,
container/image metadata, checksums, release metadata, and rollback tags. All recorded checksums
passed. The earlier stopped attempt remains at `/root/backups/sub2api/20260716-033533` for diagnosis.

| Runtime unit | Released image ID |
|---|---|
| `sub2api:custom` | `sha256:41d92a654c62fbafe9bfd525f0a5b604b40ed156d3dae53930b09eb3b5788a6a` |
| `deploy-extensions-self:latest` | `sha256:cea9693cc54191afc1218b947107ebb4d104cabca595b26486e017cb22d429ed` |

`sub2api`, `extensions-self`, `sub2api-postgres`, `sub2api-redis`, and
`risk-control-postgres` were healthy. Main `/health`, extension `/healthz`, the signed data-quality
API, and public HTTPS returned success. The retired `risk-control` application container was absent.

## Backfill And Reconciliation

Backfill covered only the reported available range:

```text
2026-05-24T09:22:15.475904Z..2026-07-16T03:53:50.234123Z
```

| Job | Range | Status | Processed rows |
|---|---|---|---:|
| `8` | 2026-05-24 to 2026-06-24 | completed | 13,098 |
| `9` | 2026-06-24 to 2026-07-16 | completed | 26,244 |

The two non-overlapping segments processed `39,342` rows. Evidence is stored in
`backfill-jobs.tsv`, `backfill-metadata.env`, `data-quality-after-backfill.json`, and
`BACKFILL-SHA256SUMS` under the successful backup directory.

- Missing-group requests fell from `28,772` before rebuild to `747` after rebuild; the remainder
  is reported source-data quality, not synthesized as zero.
- Post-backfill quality reported `40` exact and `36,610` estimated final-model requests, a connected
  source, and no recent source error.
- Main-source non-deleted accounts: `895`; account-monitor physical inventory: `895/895`.
- Extension 30-day fact-active accounts: `131`; this diagnostic count does not drive inventory paging.
- Active account/group memberships: `897`; two sampled accounts belonged to multiple groups.
- `6h/24h/7d/30d` group cards and model details each returned exactly 24 buckets at
  `15m/1h/7h/30h` resolution. Call-first ordering and `has_calls` returned only non-zero groups.
- Sample account and group totals matched `account_monitor_attempt_facts` and
  `account_monitor_request_facts` for identical `[from,to)` UTC boundaries.

## Rollback State

Rollback was not required. The recorded targets are:

```text
sub2api:rollback-20260716-034320
deploy-extensions-self:rollback-20260716-034320
```

The first publish attempt stopped before image build or service replacement because PostgreSQL
rejected an inserted view column. Commit `e53138d3a` changed the new rate-multiplier column to an
append-only view upgrade, added the production-shape regression test, and passed the isolated
PostgreSQL upgrade test before the successful retry.
