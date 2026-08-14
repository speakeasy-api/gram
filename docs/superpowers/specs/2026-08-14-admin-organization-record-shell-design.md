# Admin organization record shell

The admin organization detail page becomes a record with four routed views behind
a contextual sidebar, replacing the single scrolling page with one flat fact grid
and two tabs at the bottom.

This is slice S0 of the organization detail epic. It restructures. It moves every
fact and table that exists today into the new shape and adds none. The six later
slices, listed in the appendix, fill the shape in.

Source design: the `Gram Admin` design project, `Organization detail handoff.html`,
generated from the canvas at `templates/organization-detail/OrganizationDetail.dc.html`.
Where this spec and the handoff disagree, this spec states why.

## Scope

In scope: routes, contextual sidebar nav, breadcrumb, record header, trial callout,
nav counts, the single-project rule, and the move of existing content into the four
views.

Out of scope, each with its own slice: fact grouping and inline edit, project and
member column additions, the events panel, the activity log, the disabled-organization
treatment.

Client only. No server change, no new endpoint, no migration.

## Context

### What the admin API already provides

`AdminOrganization` on `main` carries `id`, `name`, `slug`, `account_type`,
`workos_id`, `whitelisted`, `disabled_at`, `free_trial_started_at`,
`free_trial_ends_at`, `trial_state` (`none`, `running`, `ending_soon`, `expired`,
`demoted`, `converted`), `trial_ends_at`, `member_count`, `created_at` and
`updated_at`.

Endpoints on `main`: `organizations.list`, `organization.get`, `organization.update`,
`organization.disable`, `organization.enable`, `organization.members`,
`organization.projects`, `project.get`.

### Base branch

S0 branches from `main`. The admin client chain that this slice depends on merged
on 2026-08-14: #5318, #5292, #5298 and #5317. An earlier revision of this spec had
S0 stacking on `walker/age-3186-filters-client`; that branch no longer exists.

Reused from that merged work:

- `components/Trial.tsx` and `lib/trialLabels.ts`, the trial badge and the single
  map of trial-state words
- `lib/badgeTone.ts`
- `components/ConfirmDialog.tsx`
- `pages/organizations/OrganizationActions.tsx`, which packages disable, re-enable
  and extend for two surfaces behind a `layout` prop, together with
  `WriteReportProvider`, `WriteReportContext` and `canExtendTrial(org)`
- `pages/organizations/rowActions.tsx`, which holds `useOpenOrganization`,
  `useDisableOrganization`, `useEnableOrganization` and `useExtendTrial`
- `lib/adminQueries.ts`, which holds `writeOrganizationToCache(qc, org)` and
  `cancelOrganizationFetches(qc)`
- `lib/utils.ts`, whose `fmtDateShort` reads every date in UTC so two operators in
  two zones read one organization the same way
- the copy-inside-the-value pattern, currently inline in `PeekPanel.tsx`
- `components/data-table.tsx`

Server endpoints arriving separately: `trial.rearm` (#5301), `organization.create`
(#5297). `trial.extend` merged with #5294.

### What does not exist anywhere

`include_in_reporting`, `event_limit`, `support_tier`, `billing_period` and
`payment_method` return zero hits across the server. The handoff's Settings group
and half of its Plan group are design placeholders. They are cut from the epic. The
Plan group carries account type and trial information only.

## Routes and data flow

`routes/organizations.$idOrSlug.tsx` changes from a leaf into a layout route. It
owns the record read, renders the record chrome, and renders `<Outlet/>` beneath it.

```
/organizations/$idOrSlug                              index     Overview
/organizations/$idOrSlug/projects                               Projects
/organizations/$idOrSlug/projects/$projectIdOrSlug              One project
/organizations/$idOrSlug/members                                Members
```

S0 creates those four child routes and no others. `/organizations/$idOrSlug/activity`
is created by S5, together with the view it opens and the nav item that points at it.
Until then it is an unknown child path and takes the treatment described below.

The layout reads `organizationQuery(idOrSlug)`.

### Seeding the record

Already built. `useOpenOrganization` in `rowActions.tsx` writes the row into
`organizationQuery` before it navigates, and `writeOrganizationToCache` in
`adminQueries.ts` does the same after every write, under both the id and the slug.

S0 builds nothing here and must not duplicate it. The property to preserve is that
seeding leaves `staleTime` at its default of zero, so the layout still refetches in
the background on mount. The operator sees the record immediately, and a seeded
record cannot stay stale.

### Passing the record down

Child views receive the organization as a prop from the layout. They do not re-read
`organizationQuery`. One render has one answer to which record it is showing.

Each view fetches its own collections keyed on `org.id`, on entry to that view.

### Failure paths

The handoff does not cover these. Both are decided here.

**The record read fails.** The layout renders the error in the content column and
the sidebar falls back to the global nav. A contextual nav for a record that does
not exist strands the operator with a back link and nothing else.

**An unknown child path under a real record**, such as `/organizations/acme/billing`.
The router's `notFoundComponent` renders, and the record chrome stays, because the
record is real and only the view is not.

### Breadcrumb

`components/site-header.tsx` stops deriving a title from the pathname and reads
`useMatches()`. Each route carries its crumb in `staticData`. The bar reads
`Organizations / {org name} / {view}`, with only the last segment in medium weight.
On the single-project route the last segment is the project name, not `Projects`.

This needs the `breadcrumb` shadcn primitive, which `client/admin` does not have.
Add it through the `admin-shadcn` skill. Do not hand-write it, and do not edit
anything under `components/ui/`.

## Sidebar record nav

`AppSidebar` gains one branch: `useMatchRoute({ to: "/organizations/$idOrSlug", fuzzy: true })`.
Inside a record it renders `RecordNav`. Outside one it renders the global nav it
renders today. Sidebar chrome does not change: width, `NavUser` at the bottom, hover
and active treatments are all as they are.

`RecordNav` structure, top to bottom:

| Slot      | Content                      | Behaviour                                                                                                        |
| --------- | ---------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Back row  | Chevron, "All organizations" | Links to `/organizations`. Muted, hovers to the sidebar accent.                                                  |
| Identity  | Monogram, org name, subtitle | Static. Name truncates with an ellipsis. Subtitle is account type and trial state.                               |
| Divider   | Hairline                     | Separates identity from nav.                                                                                     |
| Nav items | Overview, Projects, Members  | One active at a time. Active is medium weight on the sidebar accent. Inactive is normal weight, accent on hover. |
| Spacer    | `margin-top: auto`           | Pushes the operator block down.                                                                                  |
| Operator  | `NavUser`                    | Unchanged.                                                                                                       |

The subtitle reads its trial words from `lib/trialLabels.ts`, the same source as the
row badge, so the sidebar and the list cannot say different words for one state.

### Counts

Members costs no query: `member_count` is on the record and paints with the identity
block.

Projects reads `organizationProjectsQuery(org.id)`. The nav firing that query is
intended, not incidental. A resolved count means the Projects view renders from
cache when the operator reaches it.

Neither count reserves space and neither shows a zero while pending. A count appears
only once its query resolves. Overview carries no count, because it is not a
collection.

### The single-project rule

A nav item that would open a one-row list opens the row instead.

| Projects  | Count badge | Item target                                                           |
| --------- | ----------- | --------------------------------------------------------------------- |
| 0         | none        | the list, which renders its empty state                               |
| 1         | none        | that project, at `/organizations/$idOrSlug/projects/$projectIdOrSlug` |
| 2 or more | the count   | the list                                                              |

While the query is pending the item targets the list, which is the safe target, and
swaps on resolve.

The item keeps the label `Projects` and stays highlighted while that project is
shown. It is still the Projects branch of the nav, and relabelling it to the project
name would make the nav shift under the operator between records. The breadcrumb
carries the specific name.

### Activity

The Activity item is not in the nav in S0. It arrives in S5, with the view it opens.
The nav lists views that exist; an item leading to "not available yet" is not one.

## View contents in S0

S0 moves content. It adds none.

| View        | S0 content                                                            | Refined by |
| ----------- | --------------------------------------------------------------------- | ---------- |
| Overview    | today's flat fact card from `OrganizationDetail.tsx`, moved unchanged | S1         |
| Projects    | today's `OrgProjectsPanel`, moved                                     | S2         |
| One project | the existing `ProjectDetail` content, at the new nested route         | S2         |
| Members     | today's `OrgMembersPanel`, moved                                      | S3         |

No fact is gained and none is lost. The diff a reviewer reads is the shell and the
nav, not the shell and four new tables.

## Record chrome

The header and the trial callout are record-level chrome. They render on every view,
above that view's content. They are not part of Overview.

### Header

Record name, then badges for account type and trial state, reusing `Trial.tsx` and
`badgeTone.ts`.

Then one meta line. The handoff puts plan and billing period, current month's events,
and created date on it. Billing period is cut and events belong to S4, so in S0 the
line carries account type and created date. Separators are 3px dots that never begin
a wrapped line. The line grows when S4 lands. It is not padded now.

Anything longer than the meta line belongs in a fact group. The header is not a stat
strip.

Actions sit on the right:

- **Open in Gram**, via the existing `impersonationUrl(org.slug)`. That function
  returns undefined when `__GRAM_APP_URL__` is unset, and the action is then absent
  rather than dead.
- **Disable**, **Re-enable** and **Extend trial**, via `OrganizationActions`. Do not
  write a second implementation of these actions.

The existing `layout="footer"` already draws exactly what the header needs: outline
buttons, Re-enable in place of Disable when the record is disabled, Extend trial
gated on `canExtendTrial(org)`, record-named `aria-label`s, and the focus restore
that all six dialog exit paths depend on. A third layout would duplicate that JSX
and its guarantees.

So the header reuses it, and the prop values are renamed from `"menu" | "footer"` to
`"menu" | "buttons"`. `footer` names the one place it happened to be used first;
`buttons` names the shape, which is what the third surface is asking for. The rename
touches `OrganizationActions.tsx`, `PeekPanel.tsx` and `OrganizationActions.test.tsx`.

`OrganizationActions` reads `WriteReportContext`, which the list page currently
provides. The record layout provides its own reporter and its own polite live region.

### Trial callout

Renders for `trial_state` of `running` or `ending_soon`, and for nothing else. It is
not a general-purpose banner slot.

It carries the trial end date and hosts the same `OrganizationActions`, so the rule
about when extend is offered has one home rather than two.

Two things the handoff draws that S0 does not build:

**Payment-method status** is cut with the rest of the billing facts.

**Convert to paid** has no endpoint. The nearest real thing is `organization.update`
moving `account_type`, but the trial row would go on reporting `running`, so the
button would report a conversion that did not happen. It is left out and filed.

`trial.rearm` is the reverse case: a real action for a demoted trial that the handoff
does not know about. It arrives in the organizations list slice, and because the
header hosts the same component, the header gets it with no further work here.

## States

| State                               | Treatment in S0                                                     |
| ----------------------------------- | ------------------------------------------------------------------- |
| No trial                            | No trial badge, no callout.                                         |
| Trial running                       | Badge and callout.                                                  |
| Trial ending soon                   | Badge and callout.                                                  |
| Trial expired, demoted or converted | Badge only.                                                         |
| Disabled organization               | Badge, and the header action reads Re-enable. Full treatment is S6. |
| Record read fails                   | Error in the content column, sidebar falls back to global nav.      |
| Unknown view under a real record    | `notFoundComponent`, record chrome retained.                        |

## Testing and verification

Client only, so vitest against the existing `client/admin/src/test/harness.tsx`, plus
a changeset.

What the tests pin, beyond rendering:

- the sidebar branch in both directions, including the fallback to the global nav
  when the record read fails
- counts absent while pending and present once resolved, which is a claim about two
  states and needs both exercised
- the single-project rule at zero, one and two projects, because the behaviour of
  interest is a boundary
- the item target while the projects query is still pending, and the swap on resolve
- the breadcrumb on every route the slice creates, read from `useMatches` and not
  from the pathname, including the single-project route ending in the project name
- the callout's presence across all six `trial_state` values
- that the `footer` to `buttons` rename changed no behaviour: the existing
  `OrganizationActions` tests keep passing under the new value, and the peek panel
  footer still renders the same controls

Mutations are written for this slice rather than taken from the list above. The list
is a floor. A sweep that kills everything is treated as a broken harness until a
known survivor proves otherwise.

Gates before the pull request, each canaried, because a gate that prints nothing on
success looks the same as a gate that never ran:

```
aube run -F admin type-check
aube run -F admin lint:oxlint
aube run -F admin lint:format
aube run -F admin test
```

Do not run `aube run -F admin lint`. That script chains its four steps with bare
`pnpm` calls, which trigger an install that removes `node_modules`. Run the scripts
above individually instead.

## Questions filed rather than answered

- What "Convert to paid" should do, given that `account_type` and the trial row are
  separate axes.
- What a disabled organization looks like across the views, and whether editing is
  locked while disabled. The handoff leaves this undesigned. It is S6.
- Whether the back row should preserve the list's filters, sort and page. S0 uses a
  plain link, so that state is lost. Small follow-up, not S0.

## Appendix: the epic

Seven slices. S0 blocks the rest. S1 to S5 are independent of each other once S0
lands. Each is one chunk: its own worktree, its own session, a stacked pull request.

| #   | Slice                                   | Backend work                                              | Depends on            |
| --- | --------------------------------------- | --------------------------------------------------------- | --------------------- |
| S0  | Record shell                            | none                                                      | the open client chain |
| S1  | Overview fact groups, copy, inline edit | none                                                      | S0                    |
| S2  | Projects view                           | `toolset_count` on `AdminProject`                         | S0                    |
| S3  | Members view                            | role on `organization.members`                            | S0                    |
| S5  | Activity view                           | admin endpoint over the `auditlogs` repo, plus its facets | S0                    |
| S4  | Events panel                            | new ClickHouse organization series, new admin endpoint    | S0                    |
| S6  | Disabled-organization treatment         | unknown                                                   | design pass first     |

S4 is sequenced late deliberately. It is the only slice needing new ClickHouse work,
and the handoff scopes its panel as context rather than an analytics surface. It also
fills the header's meta line and the Projects view's events column, both of which
render honestly without it.

`auditlogs.list` already carries cursor paging with `action`, `subject_type` and
`actor_id` filters, and `listFacets` is the event-type filter the handoff draws. It
is `Session`-scoped, so S5 is a thin admin endpoint over the same repo taking an
`organization_id`, not a new subsystem.
