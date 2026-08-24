# Extensions-Self Risk Control Module

This module runs inside the `extensions-self` container beside the static custom
homepage and the account monitor. It stores risk events and rule state separately from the main Sub2API
database. It does not read or write the main user tables. The main service
remains authoritative for administrator authentication, account status, and
token revocation.

## Local configuration

Required variables:

- `RISK_CONTROL_DATABASE_URL`: dedicated PostgreSQL connection string.
- `RISK_CONTROL_INTERNAL_SECRET`: long random secret shared only by Sub2API and this service.

Optional variables:

- `RISK_IDENTITY_PREVIOUS_ENCRYPTION_KEY` and `RISK_IDENTITY_PREVIOUS_ENCRYPTION_KEY_ID` keep retained IP rows readable during an online encryption-key rotation; remove them only after those rows have been re-encrypted.

- `RISK_CONTROL_LISTEN`, default `:8090`.
- `RISK_CONTROL_MODE`, default `shadow`; use `review` before `enforce`.
- `RISK_CONTROL_DECISION_FAIL_MODE`, default `open` in the main service.
- `EXTENSIONS_SELF_HOMEPAGE_DIR`, default `/app/homepage`.
- `RISK_IDENTITY_V2_ENABLED`, master V2 ingest switch, default `false`.
- `RISK_IDENTITY_IP_COLLECTION_ENABLED` and `RISK_IDENTITY_DEVICE_COLLECTION_ENABLED`, independent collection switches, default `false`.
- `RISK_IDENTITY_ADMIN_ENABLED`, administrator evidence access switch, default `false`. The signed identity health endpoint remains available for capability discovery and reports `admin_enabled=false`; evidence and rebuild endpoints remain unavailable.
- `RISK_IDENTITY_IP_RULES_ENABLED`, `RISK_IDENTITY_DEVICE_RULES_ENABLED`, and `RISK_IDENTITY_COMPOSITE_RULES_ENABLED`, independent Shadow rule switches, default `false`.

Identity V2 rules are permanently constrained to Shadow observation. They have
no review or enforcement mode, do not reject registration, and do not ban or
change an account. The generic `RISK_CONTROL_MODE` rollout below applies only
to the pre-existing non-identity rules.

When V2 rules are enabled for the first time, migration version 3 performs the
approved one-time V1 cleanup. It removes all `legacy_v1` risk events, the V1
event-key ledger, derived `risk_subjects`, and the retired registration/API
observation rule configurations. It keeps administrator audit records, V2
identity facts and signals, account-monitor data, and the remaining generic
risk rule configurations. The cleanup records its row counts in the audit log
and is not repeated. A release backup is the only recovery source for removed
V1 data.

Repeated registration attempts for one normalized email create a zero-score
`account` observation keyed only by the email HMAC. It never enters the user
risk summary, and IP/device/composite rebuilds do not remove that evidence.

Account-monitor variables and source DB permissions belong to the sibling
[`../account-monitor`](../account-monitor/README.md) module. Both modules share
the process and `risk-control-postgres` connection, but risk tables, monitor
tables, routes, and tests remain separate. Risk control must never query the
main database through the monitor source DSN.

The service initializes `schema.sql` on startup, exposes `/healthz`, and serves
the read-only homepage at `/homepage/`. Internal event and audit endpoints
require HMAC timestamp/nonce signatures. Admin APIs are only intended to be
reached through the authenticated Sub2API proxy. Startup rejects an internal
secret shorter than 32 bytes and health fails if the homepage is missing.

## Admin surface contract

The Sub2API admin UI exposes exactly three risk-control pages. Their behavior,
Chinese labels, API contract, batch actions, rule creation, sorting and audit
requirements are defined in [`../../docs/RISK-CONTROL-ADMIN-SPEC.md`](../../docs/RISK-CONTROL-ADMIN-SPEC.md).
The V2 identity-association design for encrypted raw IP display, geolocation,
browser-instance identity, associated accounts, permanent retention, Shadow
rules, summary rebuilds, and main-service isolation is defined in
[`../../docs/RISK-CONTROL-IDENTITY-ASSOCIATION-DESIGN.md`](../../docs/RISK-CONTROL-IDENTITY-ASSOCIATION-DESIGN.md).
The reviewed authentication lifecycle seams cover password, 2FA, Passkey,
verified-email OAuth, pending OAuth binding/exchange, and each supported provider's successful account resolution. They
only enqueue V2 Shadow facts and do not alter authentication or token issuance.

Normal successful API requests are kept only in the daily activity aggregate. A
short-lived event-key ledger prevents retry double-counting but contains no raw
or derived IP/device identity and is not part of the permanent evidence record.
API success reports older than the ledger coverage window are discarded rather
than counted again after their event key expires.
Write-mode summary rebuilds remain unavailable until the persisted 14-day
Shadow deadline has passed and the same administrator has completed a matching
Dry Run within the preceding 30 minutes without intervening evidence or rule changes.
That approval check, the evidence/rule snapshot, and the write run share one
serializable transaction that locks both source tables until commit.
The risk service remains responsible for risk events, subjects, rules and
audit data; Sub2API remains authoritative for administrator authentication and
the final user account status.

The authenticated admin surface keeps the all-user view as its default, adds a
server-aggregated work overview, completes account data in bounded batches, and
opens associated accounts by exact user ID inside the existing investigation
drawer. Exact full-IP searches use `POST .../ip-identities/search` with a JSON
body; the admin UI must not place an IP in a URL, browser storage, or audit
record. Account completion preserves `available`, `unavailable`,
`not_evaluable`, and `deleted` as distinct states.

The service-to-service `GET /api/v1/admin/risk-index` endpoint merges positive
generic subjects with currently effective identity signals, deduplicates by
user, and applies stable score/recent-hit pagination. It can be narrowed to at
most 100 requested user IDs for current-page completion. The main backend keeps
PII authoritative, excludes risk IDs in its normal-account database query, and
never exposes the internal index response directly to the browser.

Identity rule rows remain evidence evaluators with the persisted `mode='shadow'`,
but that storage detail is not presented as the decision outcome. Admin responses
separate detection state, decision mode, configured action, effective action,
data quality, and configuration source. The composite registration rule may use
the independently gated `reject_candidate` decision when quality is healthy; it
only rejects the threshold candidate and never changes existing account status.
Unsupported `auto_ban` configurations fail safe to manual review.

Identity rule changes use a draft -> simulation -> publish/enable/rollback
workflow. Every operation requires an unexpired simulation owned by the current
administrator; candidate rejection and auto-ban configurations additionally
require the exact `PUBLISH <rule-code>` confirmation. Shared-network labels use
impact preview and an operator reason. Safe shared labels resolve current IP and
composite signals without deleting identity facts; revoking a label requires a
subsequent controlled replay before any old signal can become current again.

The primary UI uses readable Chinese names; protocol identifiers, source event
IDs, rule revisions, request IDs, and other technical values stay behind
collapsed technical details. Zero-score API client and successful login/API
observations never become a primary risk conclusion.

The service owns the extensions database, including risk-control and account-monitor
tables. Back it up separately from Sub2API:

```bash
docker compose \
  -f deploy/docker-compose.local.yml \
  -f deploy/docker-compose.custom.local.yml \
  --env-file deploy/.env.local \
  exec -T risk-control-postgres sh -c \
  'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' \
  > risk-control-$(date +%Y%m%d-%H%M%S).dump
```

Restore into a stopped risk database with `pg_restore --clean --if-exists`. Keep the dump and the deployed image together. The embedded schema is idempotent for the current version; take a dump before upgrading and stop on startup/schema errors rather than deleting the risk data directory.

## Rollout order

1. Start the dedicated PostgreSQL and `extensions-self` containers in `shadow` mode.
2. Confirm registration identity evidence and actionable risk events appear in the three admin pages; normal successful API activity remains diagnostic-only.
3. Tune rules in `review` mode and verify the operation audit page.
4. Enable `enforce` only after a real signed event and a manual ban/unban have been verified locally.

Production deployment is not implied by building this image. Run the backend tests, frontend checks, Compose validation, and an authenticated browser smoke test first.
