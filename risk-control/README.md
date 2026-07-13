# Risk Control Service

This service stores risk events and rule state separately from the main Sub2API database. It does not read or write the main user tables. The main service remains authoritative for administrator authentication, account status, and token revocation.

## Local configuration

Required variables:

- `RISK_CONTROL_DATABASE_URL`: dedicated PostgreSQL connection string.
- `RISK_CONTROL_INTERNAL_SECRET`: long random secret shared only by Sub2API and this service.

Optional variables:

- `RISK_CONTROL_LISTEN`, default `:8090`.
- `RISK_CONTROL_MODE`, default `shadow`; use `review` before `enforce`.
- `RISK_CONTROL_DECISION_FAIL_MODE`, default `open` in the main service.

The service initializes `schema.sql` on startup and exposes `/healthz` for container health checks. Internal event and audit endpoints require HMAC timestamp/nonce signatures. Admin APIs are only intended to be reached through the authenticated Sub2API proxy. Startup rejects an internal secret shorter than 32 bytes.

The service owns only its risk database. Back it up separately from Sub2API:

```bash
docker compose -f deploy/docker-compose.local.yml exec -T risk-control-postgres \
  pg_dump -U "$RISK_CONTROL_POSTGRES_USER" -d "$RISK_CONTROL_POSTGRES_DB" -Fc \
  > risk-control-$(date +%Y%m%d-%H%M%S).dump
```

Restore into a stopped risk database with `pg_restore --clean --if-exists`. Keep the dump and the deployed image together. The embedded schema is idempotent for the current version; take a dump before upgrading and stop on startup/schema errors rather than deleting the risk data directory.

## Rollout order

1. Start the dedicated PostgreSQL and risk-control containers in `shadow` mode.
2. Confirm registration, login, OAuth registration, content-risk, quota, upstream-error, and normal API events appear in the three admin pages.
3. Tune rules in `review` mode and verify the operation audit page.
4. Enable `enforce` only after a real signed event and a manual ban/unban have been verified locally.

Production deployment is not implied by building this image. Run the backend tests, frontend checks, Compose validation, and an authenticated browser smoke test first.
