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
- `RISK_IDENTITY_IP_RULES_ENABLED`, `RISK_IDENTITY_DEVICE_RULES_ENABLED`, and `RISK_IDENTITY_COMPOSITE_RULES_ENABLED`, independent Shadow rule switches, default `false`.

Identity V2 rules are permanently constrained to Shadow observation. They have
no review or enforcement mode, do not reject registration, and do not ban or
change an account. The generic `RISK_CONTROL_MODE` rollout below applies only
to the pre-existing non-identity rules.

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

The current implementation must be treated as incomplete until the user page
shows account identity and explainable reasons, the rule page supports creating
and testing rules, and the audit page shows the administrator, target, reason,
result and failure detail. Raw values such as `login_failure` and `critical`
are protocol values and must not be the primary text in the admin UI.

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
2. Confirm registration, login, OAuth registration, content-risk, quota, upstream-error, and normal API events appear in the three admin pages.
3. Tune rules in `review` mode and verify the operation audit page.
4. Enable `enforce` only after a real signed event and a manual ban/unban have been verified locally.

Production deployment is not implied by building this image. Run the backend tests, frontend checks, Compose validation, and an authenticated browser smoke test first.
