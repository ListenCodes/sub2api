# Extension-First Homepage And Account Monitor Search Design

## Goal

Fix the custom homepage without broadening the official Sub2API surface:

- remove the blocked Google Fonts request without weakening CSP;
- render the configured site logo, site name, and site subtitle;
- use the approved product-led hero layout;
- show current public group rates from the main database on every page load;
- restore both sidebar brand links and the homepage brand link to `/home`;
- let administrators search account monitoring by configured account name or
  the actual account identity shown below that name in account management.

The implementation must prefer `extensions-self`. Official backend business
routes remain unchanged. Native frontend changes are limited to the sidebar
brand destination and the existing account-monitor integration surface.

## Scope boundary

The following changes belong to the extension:

- homepage HTML, CSS, and JavaScript;
- a read-only public group catalog query;
- an exact public JSON route below the existing homepage proxy namespace;
- account-monitor identity extraction and server-side search;
- the source-database security view and its tests and documentation.

The official backend receives no new route, handler, service, repository, or
setting. Its CSP is not relaxed. Main frontend edits are limited to
`AppSidebar.vue` and the existing account-monitor page, filter state, and API
types.

The anonymous homepage never receives account counts, account identities, group
IDs, deleted or disabled groups, exclusive groups, user-specific rates, routing
configuration, or credentials. The derived account identity added below is
limited to the existing authenticated administrator monitor surface.

## Data flow

The homepage remains an iframe served through the existing wildcard proxy:

1. The main application serves `/home` and embeds
   `/api/v1/extensions-self/homepage/`.
2. Homepage JavaScript fetches `/api/v1/settings/public` for branding.
3. Homepage JavaScript fetches the relative URL `api/public-groups`, which the
   browser resolves to
   `/api/v1/extensions-self/homepage/api/public-groups`.
4. The existing main wildcard proxy forwards that request to
   `extensions-self` as `/homepage/api/public-groups`.
5. The extension intercepts that exact path before the static file server and
   queries the main database through the existing dedicated read-only login.

No official backend routing change is required.

## Public group catalog

Add a dedicated security-barrier view named
`extensions_self_ro.public_group_catalog`. Keeping this separate from the
monitoring dimension avoids widening existing monitoring contracts.

The view filters at the database boundary:

- `status = 'active'`;
- `deleted_at IS NULL`;
- `is_exclusive = FALSE`.

It exposes only the columns required to query and order the public response:

- `name`;
- `platform`;
- `rate_multiplier`;
- `peak_rate_enabled`;
- `peak_start`;
- `peak_end`;
- `peak_rate_multiplier`;
- `sort_order` for server-side ordering only.

The JSON response does not include `sort_order`, status, deletion state,
exclusive state, subscription type, or an internal identifier. Both standard
and subscription groups are included when they satisfy the public filters.
Rows are ordered by `sort_order`, platform, then name. Group names are already
unique among non-deleted rows, so no internal ID is needed for deterministic
ordering.

The endpoint is `GET /homepage/api/public-groups` inside the extension. `HEAD`
is supported consistently with homepage assets; other methods return `405`.
The success response is:

```json
{
  "groups": [
    {
      "name": "Example",
      "platform": "openai",
      "rate_multiplier": 0.3,
      "peak_rate_enabled": true,
      "peak_start": "14:00",
      "peak_end": "22:00",
      "peak_rate_multiplier": 1.2
    }
  ]
}
```

The extension uses the existing `ACCOUNT_MONITOR_SOURCE_DATABASE_URL` and
query timeout. The public reader is available when that source connection is
configured; the monitor collector remains controlled by
`ACCOUNT_MONITOR_ENABLED`. This prevents homepage data access from being
accidentally coupled to whether periodic monitoring is running. Missing source
configuration or query failure returns `503` with a generic error and does not
leak database details.

The endpoint sends `Cache-Control: no-store`. The page performs one request on
initial load and one on each full refresh, with no polling and no persisted or
hardcoded fallback rates.

## Account monitor identity search

Add an `account_identity` column at the end of
`extensions_self_ro.account_dimension`. Appending the column preserves the
existing `CREATE OR REPLACE VIEW` column order contract.

The value must match the account-management subtitle fallback chain:

1. the account's `extra.email_address`;
2. the account's `extra.email`;
3. the account's `credentials.email`;
4. for a child account, its parent account's `credentials.email`;
5. an empty string when none exists.

The view may read those exact JSON keys to derive the value, but the restricted
role never receives the complete `extra` or `credentials` objects. Tokens,
keys, cookies, refresh tokens, account IDs from upstream providers, and other
credential fields remain inaccessible.

The authenticated account-monitor accounts endpoint accepts a trimmed `query`
parameter. Matching is a case-insensitive substring search across configured
account name and `account_identity`. Filtering happens before pagination, so a
match is not limited to the current page. In physical-account mode each row is
matched directly. In parent-rollup mode the parent row is retained when the
parent or any member matches either field.

`AccountSummary` returns `account_identity` only through the existing signed,
administrator-only account-monitor proxy. It is not persisted in
`risk-control-postgres`, included in the public homepage response, or exposed by
an anonymous route.

The account-monitor filter bar adds one debounced search control labeled
`账号`, with the placeholder `搜索账号名称或实际账号`.
The value is stored as `query` in route state, resets pagination to page 1, and
survives refresh, back/forward navigation, and copied URLs. The table shows the
actual account as secondary text below the configured name when present, with
the existing Sub2API account ID retained as tertiary metadata so an
administrator can verify why a result matched.

## Branding and homepage presentation

Remove the Google Fonts `@import` and use a system font stack. Do not add
`fonts.googleapis.com` or `fonts.gstatic.com` to production CSP.

Load public settings and apply them to:

- the document title;
- the homepage navigation logo;
- the homepage navigation site name and subtitle;
- the hero brand eyebrow;
- the footer site identity.

The configured `site_logo` may be a validated data URL. If the settings request
or image load fails, fall back to the existing `/logo.svg`; never use the broken
`/logo.png` path. If site name or subtitle is empty, use a concise local fallback
without blocking the rest of the page.

Use the approved product-led layout:

- navigation: configured logo, site name, and subtitle;
- hero eyebrow: configured site name;
- large hero title: `AI API 统一网关`;
- supporting copy: configured subtitle followed by the current concise value
  proposition;
- group cards generated from the live response and grouped by platform.

High-peak details are shown only when `peak_rate_enabled` is true, using the
stored time range and multiplicative peak factor without claiming that the
browser has calculated a user-specific effective rate. Unknown platforms use a
neutral text badge instead of disappearing. Empty results show a neutral `no
public groups` state. Fetch failures show `rates temporarily unavailable`;
neither state renders the old hardcoded rows.

The homepage logo/name link uses `href="/home"` and `target="_top"`. The native
sidebar logo and site-name links also use `/home` for admins and regular users.
The console/dashboard call-to-action keeps its current destination.

## Security and failure behavior

- The browser receives only the JSON field whitelist above.
- Filtering occurs in the security view, not only in JavaScript or the HTTP
  handler.
- Admin-editable group names and platform values are inserted with DOM text
  APIs, never concatenated into `innerHTML`.
- The homepage accepts the configured logo only as an allowed image data URL or
  same-origin path and otherwise uses `/logo.svg`.
- The extension source login keeps access limited to `extensions_self_ro`.
- Database and internal error text are logged server-side only; public errors
  are generic.
- Branding and rates fail independently so one unavailable endpoint does not
  blank the page.
- Dynamic responses use `no-store`; static homepage assets may keep their
  existing short cache.
- The implementation introduces no inline secrets, main database owner DSN,
  CSP relaxation, runtime injection, or direct database access from the
  browser.
- The account identity is returned only by the authenticated administrator
  endpoint and is derived by a field-level database whitelist; raw credential
  JSON never crosses the view boundary.

## Testing

Focused tests cover:

- the security view name, security barrier, public filters, field whitelist,
  restricted-role access, and denial of raw main tables;
- the source reader's scan, ordering, timeout, empty result, and query failure;
- the exact extension route, GET/HEAD behavior, `405`, `503`, generic errors,
  JSON shape, and `no-store` cache header;
- homepage removal of Google Fonts and `/logo.png`, dynamic settings and group
  fetches, generated group cards, failure states, and `/home` top navigation;
- native sidebar logo and name links targeting `/home`;
- account identity extraction parity with account management, including parent
  fallback and blank values;
- case-insensitive name and identity search before pagination in physical and
  parent-rollup modes;
- account-search API serialization, URL-state restoration, debounce behavior,
  page reset, table identity display, and empty results;
- existing extension layout, proxy, security-header, frontend, and deployment
  contract tests.

Browser verification at desktop and mobile widths must confirm the configured
logo has non-zero natural dimensions, the site name and subtitle are visible,
group data matches the endpoint, both brand links reach `/home`, and the console
contains no CSP or resource-loading error. Authenticated browser verification
also searches one account by configured name and one by actual account identity
and confirms the same row is returned.

## Release and rollback

The versioned source-view installer must be reapplied as the main database owner
before deploying the new extension image. Verify that the restricted login can
select `extensions_self_ro.public_group_catalog` and still cannot select raw
main tables or credential columns.

Code completion, branch push, CI/GHCR publication, source-view application,
production deployment, and browser verification are separate reported states.
Normal production deployment still uses the paired immutable main and extension
image digests and the administrator-triggered release path.

Rollback restores the previous paired image digests. The added read-only view
may remain because it is additive and unused by the previous image; dropping it
is optional and must never be part of automatic database rollback.
