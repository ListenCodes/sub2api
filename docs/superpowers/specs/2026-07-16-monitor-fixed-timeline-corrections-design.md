# Monitor Fixed Timeline Corrections Design

Date: 2026-07-16

Status: Approved for expedited implementation in conversation

## 1. Scope And Precedence

This specification supplements the approved account and group monitor designs.
Where it conflicts with the earlier usability-corrections specification, this
document takes precedence for account group presentation, filter labels, group
ordering and call filtering, group-monitor ranges, and timeline layout.

## 2. Account Monitor

- The account table retains its dedicated platform column.
- The group column displays each current group as `group name + rate
  multiplier`; it does not repeat the group platform badge or platform text.
- The safe account-group dimension exposes the non-secret
  `groups.rate_multiplier` value. The extension API returns it as
  `rate_multiplier` on every account group summary, including the complete
  group-filter inventory and parent-account membership unions.
- The Trends detail tab returns and renders the newest bucket first.

## 3. Explicit Filter Labels

Account-monitor and group-monitor controls have visible compact labels so the
purpose remains clear for default and selected values. Account labels cover
time range, platform, group, actual model, account status, result, rollup, and
risk score. Group labels cover group name, platform, group status, call status,
and time range. Existing immediate select querying and 300 ms text-input
debouncing remain unchanged.

## 4. Group Ordering And Call Filter

After all group dimensions and selected-range metrics are assembled, list
results are ordered by:

1. groups with `total_requests > 0`;
2. groups with no calls;
3. within each partition, the existing case-insensitive platform, name, and
   group-ID order.

The call-status filter adds `has_calls`. It matches every group whose selected
range has `total_requests > 0`, regardless of whether its detailed state is
normal, partially failed, all failed, or recently idle. Existing detailed
status filters remain available.

## 5. Fixed 24-Bucket Time Contract

Group-monitor list and detail APIs accept only these ranges and always return
exactly 24 chronological buckets:

| Range | Duration | Bucket size |
|---|---:|---:|
| `6h` | 6 hours | 15 minutes |
| `24h` | 24 hours | 1 hour |
| `7d` | 7 days | 7 hours |
| `30d` | 30 days | 30 hours |

The default and minimum range is `6h`; `1h` and `12h` are removed from the API,
route parser, filters, and validation messages. The response continues to
include `bucket_seconds`.

Because a 10-minute aggregate cannot produce exact 15-minute windows, display
buckets are calculated from `account_monitor_request_facts`, using its existing
group/time indexes. List and model-detail queries use the same closed,
server-selected bucket interval. Missing buckets are filled with zero values.
Range totals and detail totals equal the sum of their 24 buckets.

## 6. Timeline Presentation

- Every card timeline uses a 24-column grid with a stable full-card width and
  equal-width bars. Switching ranges cannot resize the chart or change the bar
  count.
- Bucket colors keep one semantic map: no calls gray, all successes green,
  partial failures amber, and all failures red.
- Detail timelines remain chronological from left to right and contain the same
  24 buckets as the card.
- The model timeline container always reserves a horizontal scrollbar gutter
  and uses an explicit horizontal-scroll surface so later columns are
  reachable. The first model column and header remain sticky.

## 7. Compatibility And Error Handling

- URLs containing `1h`, `12h`, or any unsupported group range normalize to
  `6h` in the Vue route state; direct API requests receive HTTP 400 with the new
  allowed-range message.
- Existing refresh, pagination, dialog, stale-data, and last-successful-data
  behavior remains unchanged.
- No iframe, credential field, raw request body, or legacy container is added.

## 8. Verification And Release

TDD coverage must include safe-view multiplier propagation, newest-first
trends, explicit filter labels, `has_calls`, calls-first stable ordering, four
range mappings, exactly 24 buckets for list and detail, semantic bar colors,
fixed chart tracks, and the visible detail scroll surface. Run affected Go and
Vue suites, full frontend typecheck/build, database source-view integration,
deploy contracts, and `git diff --check`, then request independent review.

After local verification, push the feature branch, merge into an unchanged and
clean `custom`, push `origin/custom`, and publish only with
`/opt/sub2api-custom/publish-custom.sh --commit <commit>`. Re-run authenticated
desktop, wide, and mobile browser acceptance after production health and data
checks. Retain the publisher's database, configuration, image, and rollback
backups.
