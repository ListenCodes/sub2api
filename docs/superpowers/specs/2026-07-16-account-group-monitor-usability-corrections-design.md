# Account And Group Monitor Usability Corrections Design

Date: 2026-07-16

Status: Approved in conversation; pending written-spec review

## 1. Scope And Precedence

This specification corrects the production behavior delivered by
`2026-07-15-extensions-center-account-group-monitor-design.md`. It covers the
native Vue user-risk, account-monitor, and group-monitor surfaces. Where the two
specifications conflict, this document takes precedence for:

- the account candidate set;
- account and group pagination;
- account group membership and filtering;
- platform filters and platform presentation;
- account detail tabs and fields;
- filter application and refresh behavior;
- extension page headings;
- group-monitor time ranges and bucket sizes.

The existing risk-score formula, final-request semantics, group ordering,
retention, source security model, routes, and release gates remain unchanged
unless this document explicitly says otherwise.

## 2. Production Evidence And Problem Statement

Production verification found:

- 895 non-deleted accounts in the main database;
- 111 accounts with attempt facts in the default seven-day range;
- 897 account-group rows covering 895 accounts and 13 groups;
- configured account platforms are Anthropic, OpenAI, and Grok;
- Sub2API supports Anthropic, OpenAI, Gemini, Antigravity, and Grok;
- account monitoring accepts only 20, 50, and 100 rows even though the shared
  table preference can offer 1000;
- group monitoring accepts only 12, 24, and 48 rows for the same reason;
- the shared `Pagination` component renders globally configured choices instead
  of the choices accepted by the two monitor APIs, so selecting 1000 falls back
  to the local default;
- account monitoring currently derives candidates only from attempt facts, so
  accounts without calls in the selected range disappear;
- the first group page can contain only no-data groups while groups with data
  remain on later pages under the required platform/name ordering.

The correction must make the pages describe the managed account and group
inventory accurately while keeping time-scoped metrics accurate.

## 3. Account Candidate And Group Data

### 3.1 Main-database security views

Keep `extensions_self_ro.account_dimension` as the authoritative account
inventory. Add a security-barrier view named
`extensions_self_ro.account_group_dimension` with only:

- `account_id`;
- `group_id`;
- `group_name`;
- `group_platform`;
- `group_status`;
- `group_deleted_at`.

The view joins `account_groups` to `groups`. It contains no account credentials,
group secrets, rates, user data, or request data. Installation is idempotent.
`extensions_self_monitor_ro` can select it; PUBLIC and an explicit denial role
cannot.

### 3.2 Account list assembly

The account list defaults to every non-deleted account in
`account_dimension`, including accounts with no attempt facts in the selected
time range. The extension backend performs the operation in this order:

1. Read account dimensions and account-group membership from the main database.
2. Apply dimension filters: account ID, parent account ID, account status,
   platform, and group membership.
3. Batch aggregate matching attempt facts from the extension database.
4. Apply fact filters such as model, user, API key, result, request type, error
   category, and status code. When any fact filter is active, an account with no
   matching facts is not a match.
5. Add zero-valued metrics for dimension-only accounts under dimension-only or
   unfiltered queries.
6. Evaluate health for accounts with sufficient facts. A zero-fact account has
   `risk_score_available=false`, an empty reason list, and no fabricated risk
   score.
7. Sort the full filtered candidate set and only then apply stable pagination.

The existing 5000-account guard remains for operations that require in-memory
risk scoring. It is a safety limit, not a page-size limit.

For parent rollup, group membership is the union of memberships of physical
accounts represented by that rollup row. Duplicate groups are removed and
sorted by platform, name, and ID.

### 3.3 Multi-group response and filtering

Each account summary adds a `groups` array containing:

- `group_id`;
- `name`;
- `platform`;
- `status`.

The account endpoint accepts `group_id=<positive id>|ungrouped`. A concrete ID
matches an account belonging to that group even when the account belongs to
other groups. `ungrouped` matches accounts with no current account-group row.
The UI displays every current membership, not only the selected one. Memberships
whose group has been soft-deleted are excluded from display and filtering and do
not prevent an otherwise ungrouped account from matching `ungrouped`.

### 3.4 Source failure behavior

The all-account query depends on the main read-only source. When it is
unavailable or times out, the API returns a specific source error. The Vue page
keeps its most recent successful rows, total, and update time while displaying
the error. It must not replace the inventory with the 111 fact-only accounts or
label stale data as current.

## 4. Pagination Contract

Account and group monitoring use Sub2API's global
`table_page_size_options`. The UI shows exactly the sanitized configured values,
including 1000 when configured. The backend accepts any integer from 5 through
1000 and rejects values outside that range.

Selecting a page size:

- persists through the existing shared table preference;
- writes the value to the route query;
- resets the current page to 1;
- sends the same value to the backend;
- never silently falls back to 20 or 12.

The shared `Pagination` component must honor an explicit `pageSizeOptions` prop.
Monitor pages should use the global options directly instead of maintaining
incompatible fixed allowlists.

## 5. Platform Registry And Presentation

Create or extract one shared Sub2API platform registry used by filters and
badges. It contains all supported platforms:

| Value | Label |
|---|---|
| `anthropic` | Anthropic |
| `openai` | OpenAI |
| `gemini` | Gemini |
| `antigravity` | Antigravity |
| `grok` | Grok |

Account and group filters use a `Select`, not free-text search, and always offer
all five platforms plus "all platforms". The monitor pages reuse the existing
Sub2API platform icons, labels, light backgrounds, text colors, and dark-mode
variants. They do not introduce a second color map.

Account table platform cells and group-monitor cards use one shared compact
platform badge. Group-card names use `text-base` with semibold weight; badges
remain secondary to the group name. Unknown future platform values fall back to
the existing neutral platform style and remain readable.

## 6. Account Detail Dialog

### 6.1 Tabs and dimensions

Remove the Attempts tab from the native account detail dialog. The compatibility
API may remain, but the UI must not request or expose it. The remaining tabs are:

- Models;
- Users / API Keys;
- Errors;
- Trends;
- Media.

The dialog has a stable maximum height of 80 viewport-height units. The account
summary and tab strip remain visible. The content region owns vertical and
horizontal scrolling. Tabular content uses a sticky header inside that region.
Changing to Trends must not change the dialog's outer height. The dialog is not
resizable.

The dialog closes through its close button, Escape, or a click on the backdrop.
Clicks inside the dialog, including scrollbars and table cells, do not close it.

### 6.2 Users / API Keys columns

The Users / API Keys response and table expose:

- user email;
- API key name;
- attempts;
- successes;
- failures;
- success rate;
- tokens;
- user cost;
- last attempted time.

Do not return or render username or masked key prefix for this surface. Keep
numeric user and API-key IDs available internally for stable row identity and
filters, but do not add them as replacement display columns.

## 7. Filter And Refresh Interaction

The sidebar entries User Risk, Account Monitor, and Group Monitor all use the
same immediate-filter behavior. This also applies to the Users, Rules, and Audit
subpages inside User Risk.

- A select change queries immediately.
- Text and number input queries 300 ms after the last edit.
- A custom date range queries after both endpoints form a valid range.
- Every filter change resets page to 1 and updates the route query.
- The Apply button is removed.
- Reset remains and immediately reloads defaults.
- Pending debounced work and in-flight requests are cancelled on replacement or
  unmount.

"Automatic query" means a query caused by a user changing a filter. It does not
mean periodic refresh. Remove the 60-second automatic-refresh toggle and timer
from account and group monitoring. Keep one explicit Refresh button that
preserves the current filter, page, and open-detail state.

Every detail dialog or drawer on these surfaces follows the same backdrop,
Escape, and close-button behavior described above.

## 8. Page Heading Cleanup

Remove the repeated parent heading and subtitle from all three extension entry
surfaces:

```text
Extensions Center
User security and operational quality
```

The sidebar remains the parent navigation. Each route starts with its own page
title and operations. User Risk retains its internal Users, Rules, and Audit
navigation.

## 9. Group-monitor Long Ranges

Add `7d` and `30d` to list and detail endpoints and filters. The source of truth
remains the existing 10-minute aggregate table; no duplicate long-range fact
tables are added.

Use fixed query-time bucket sizes:

| Range | Display bucket |
|---|---|
| `1h`, `6h`, `12h`, `24h` | 10 minutes |
| `7d` | 1 hour |
| `30d` | 6 hours |

The server selects the bucket expression from this closed range-to-granularity
map. It never interpolates a client-supplied SQL expression. List cards and
model details use the same granularity and complete missing buckets with zeroes.
The response includes the effective bucket duration so labels and accessibility
text do not claim every view is a 10-minute timeline.

Retention remains 90 days. Group ordering remains
`LOWER(platform), LOWER(name), group_id`; the larger global page size makes it
possible to view all current production groups without changing the approved
ordering.

## 10. Error Handling And Compatibility

- Existing monitor routes remain valid.
- Existing short-range URLs keep their behavior.
- Unsupported range values and page sizes return 400 with precise messages.
- A route query containing a formerly valid page size is normalized only when
  it is absent from the current global configuration; normalization is visible
  in the URL and never changes after the request is sent.
- Group and account list errors preserve the last successful data independently
  from detail errors.
- Source-view upgrades work on existing production installations and clean
  installations.
- No iframe, static account-monitor page, legacy risk-control container, request
  body, credential, full API key, or per-request error text is introduced.

## 11. Test Plan

### 11.1 Go and PostgreSQL

Test:

- all non-deleted accounts are candidates;
- zero-fact accounts have zero metrics and unavailable risk score;
- fact filters exclude zero-match accounts;
- one account with multiple groups is returned once with all groups;
- concrete group and ungrouped filters;
- parent-rollup membership union;
- dimension filtering precedes full-set sorting and pagination;
- every page size from 5 through 1000 is accepted and outside values rejected;
- source timeout and disconnect errors;
- account-group safe-view columns and allow/deny permissions;
- idempotent source-view upgrade;
- 7-day hourly and 30-day six-hour aggregation;
- late data, empty buckets, time zones, boundary exclusion, and model detail
  totals;
- user/API-key detail fields exclude username and masked prefix.

### 11.2 Vue and API contracts

Test:

- global page-size options are rendered and 1000 remains selected;
- account and group pagination send the displayed page size;
- all five supported platform options;
- shared platform badge colors, icons, labels, dark mode, and unknown fallback;
- multi-group badges and group filter route state;
- zero-data account rows;
- Attempts tab absence;
- stable dialog height, internal overflow, and sticky headers;
- backdrop, Escape, and close-button behavior;
- immediate select queries and 300 ms input debounce across all requested pages;
- cancellation, reset, route restoration, and page reset;
- absence of automatic-refresh timers and toggles;
- removal of the repeated Extensions Center heading;
- long-range options and effective bucket labels.

### 11.3 Full verification

Run account-monitor and risk-control Go tests, the complete backend Go suite,
frontend unit tests, typecheck, lint and production build, deploy/ops and Compose
contract tests, database migration/idempotency/aggregation tests, and
`git diff --check`. Request an independent code review and rerun affected suites
after accepted fixes.

Browser acceptance covers 1440x900, 1920x1080, and 390x844. Verify page identity,
filters, page size 1000, all-account total, multi-group display, platform badges,
fixed detail dialog, sticky headers, backdrop closing, long ranges, manual
refresh, console, network, overlap, and horizontal overflow.

## 12. Release And Rollback

After all local gates pass, push the feature branch, merge into a clean and
unchanged `custom` baseline, rerun merge-sensitive tests, and push
`origin/custom`. Publish only through:

```text
/opt/sub2api-custom/publish-custom.sh --commit <approved-origin-custom-commit>
```

Before publishing, back up both PostgreSQL databases, Compose, `.env`, Nginx,
certificates, container metadata, image metadata, and rollback tags. Reinstall
and verify the upgraded safe views before rebuilding the main and
extensions-self images. Do not recreate `risk-control-postgres`.

Post-release checks include container and endpoint health, safe-view permissions,
account totals, multi-group samples, 7-day and 30-day group totals, data quality,
public HTTPS, authenticated browser acceptance, absence of legacy delivery, and
recorded image IDs and rollback targets.

Rollback restores the preceding main and extensions-self images and matching
configuration. Extension facts and cursors remain unless schema corruption is
proven; only then may the extension database be restored from the matching
release backup.
