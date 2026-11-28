# Internal organizations

## Intent

Speakeasy employees own one or two dozen organizations used for testing and
dogfooding. They pollute customer analysis: a recent study of trial-to-PAYG
conversion signals had to have them filtered out by hand, by telling the agent
doing the research which organizations to ignore.

Staff need a durable label, "this organization is internal", that any data
project can read, whether the work happens in PostHog, against our own
databases, or through an agent using the Admin MCP.

## Goals

- Staff can mark and un-mark an organization as internal.
- Anyone starting a data project, human or agent, can get the full list of
  internal organizations in one step.
- PostHog work can exclude internal organizations without leaving PostHog.

## Non-goals

- Bulk marking. The initial set is one or two dozen organizations, marked by
  hand.
- Automatic detection (for example by member email domain).
- A history of when an organization was internal. The label is a current
  value only.
- Hiding PostHog events that carry no organization group.
- Changing or removing the hardcoded `internal: false` person property the
  dashboard sends today.

## Anti-goals

- The label must not grant anything. Credit-limit bypass, early hooks
  releases and whitelist rights stay tied to the configured staff
  organization (`staff.Organization`).
- No existing admin page or report may start dropping internal organizations
  by default. Exclusion is always something the reader opts into.
- Changes must not appear in the organization's own audit log, which its
  admins can read and which feeds their webhooks.

## Approaches

1. **PostHog only.** Set an `is_internal` property on the organization group
   by hand. No code, but the label is invisible to the admin dashboard, the
   Admin MCP, and anything built on Postgres or ClickHouse.
2. **Database only.** A column and an admin toggle. Readable by our own
   tooling, but PostHog cannot join against Postgres, so nothing is hidden
   there.
3. **Database as the source of truth, pushed to PostHog on change.** Chosen.
   It serves both kinds of data project, and the push is small because the
   `GroupIdentify` helper already exists.

## Design

**Storage.** `organization_metadata.internal boolean NOT NULL DEFAULT FALSE`.

**Admin API.** `updateOrganization` gains an optional `internal` field next to
`whitelisted`; the "at least one field" rule extends to it. In the result the
field defaults to false and is not required, so an admin client that validates
responses still accepts one from a server that predates the label. `internal` must be
sent on its own: combining it with `account_type` or `whitelisted` is
rejected, so a write is never half-applied. `AdminOrganization`
returns `internal`. `listOrganizations` returns it on each row and accepts an
`internal_status` filter of `all` (default), `internal` or `external`,
mirroring `disabled_status`. The count query takes the same filter.

**Admin dashboard.** An "Internal" switch in the organization overview's
Details panel, beside "Whitelisted" and using the same confirm-then-save
interaction.

In the organizations list, internal rows are muted: their text uses the muted
foreground colour. There is no badge, dot or extra column. Because colour
alone is not readable by a screen reader, the row's name cell also carries
visually hidden "Internal" text. Muting uses text colour, not opacity, since
the whole table already dims with opacity while a page is loading. No other
row state is muted today, so the treatment is unambiguous.

The filter lives in the existing filter sheet as an "Internal" select with
"All organizations" (default), "Internal only" and "Exclude internal". When
set, it shows as a removable chip in the toolbar like the other filters. The
list still shows every organization by default.

**Admin MCP.** `find_organizations` returns the flag and accepts the filter,
and the filter alone counts as an explicit filter, so listing internal
organizations needs no search term. A page holds at most 20 results, so the
tool description tells the caller to compare the total with the results and
keep paging before treating the list as complete. `get_organization_summary` returns the flag. Both
descriptions say that internal organizations are employee test and
dogfooding organizations and should normally be excluded from customer
analysis. No write tool: marking happens in the dashboard.

**PostHog.** After every successful write of the flag, whether or not the
value changed, the admin service calls `GroupIdentify("organization", <slug>, {"is_internal": <bool>})`.
The admin service does not hold a PostHog client today and needs one
injected. The call is best-effort: a failure is logged and does not fail the
request. Saving the toggle again re-sends the current value, which is how
drift is repaired.

**Recording.** Each change is written to the server log with the staff actor
and organization id, as the adoption dashboard link does.

**Manual step, outside the repo.** In the PostHog project settings, add
"organization `is_internal` is not `true`" to the internal and test accounts
filter.

## Known limits

- PostHog keeps one current value per group property. Marking an
  organization is expected to hide its past events too, and un-marking to
  bring them back. Confirm this against the real project before relying on
  it.
- The PostHog group is keyed by organization slug. If a slug changes, the new
  group does not carry the property until the toggle is saved again.

## Left to the builder

- Exact wording of the switch's confirmation text.
- Test layout, following the existing tests for `whitelisted` and the
  adoption dashboard link.
