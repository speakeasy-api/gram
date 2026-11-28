# Organization Creation Monitors

Speakeasy creates organizations from several flows: self-serve signup, the
Assistants onboarding, a workspace named from an MCP client, the admin
dashboard, and two mirrors that copy organizations from WorkOS. The
`organizations.count` gauge reports only the running total, so it cannot show
which flow is creating organizations or how quickly. This counter adds that
breakdown.

## Observability

| Metric                       | Type    | Unit             | Emitted by                                 |
| ---------------------------- | ------- | ---------------- | ------------------------------------------ |
| `gram.organizations.created` | Counter | `{organization}` | `server/internal/organizations/orgmetrics` |

It counts one per `organization_metadata` row actually inserted. Updates to an
existing row do not count. The count is recorded after the inserting transaction
commits, so a rolled-back transaction or a retried worker activity does not add
to it. When two flows race to create the same organization, only the flow whose
insert won counts it.

The counter has one tag, `gram.organization.creation_source`. In Datadog the
dots in OTel names carry over unchanged.

| Value            | Flow                                                                                                              | Process      |
| ---------------- | ----------------------------------------------------------------------------------------------------------------- | ------------ |
| `signup`         | Self-serve signup, from the register page or a company name carried through login. Includes partner-code signups. | server       |
| `assistants`     | The organization created automatically for a user who arrives through Assistants.                                 | server       |
| `platform_mcp`   | A workspace named while authorizing an MCP client.                                                                | server       |
| `platform_admin` | An organization a platform admin created from the admin dashboard.                                                | admin server |
| `workos_sync`    | A row inserted while applying a WorkOS organization event for an organization not yet known locally.              | worker       |
| `login_mirror`   | A row inserted while mirroring a user's WorkOS memberships at login, for an organization not yet known locally.   | server       |

The tag set is closed. The counter carries no organization or user identifiers,
so finding the specific organizations means querying `organization_metadata` by
`created_at`. Do not rely on its `creation_source` column to separate the
flows: the two mirrors insert it as null, and a product flow that later writes
the same organization fills it in.

The flows that create an organization in WorkOS first (signup, Assistants,
MCP workspace, admin) normally insert the row themselves. If the WorkOS event
sync or the login mirror gets there first, that organization is counted under
`workos_sync` or `login_mirror` instead. A steady trickle of those values is
expected; a sustained rise in them alongside a drop in the product flows points
at slow provisioning rather than new demand.

## Datadog monitors

Create these in Datadog (monitors are managed in the Datadog UI, not in this
repo). Set thresholds from the baseline observed after the counter ships.

### Organization creation volume per source

Alert when organizations are created much faster than usual from any one flow.

```
Monitor type: Metric (multi alert, grouped by gram.organization.creation_source)
Query: sum(last_1h):sum:gram.organizations.created{*} by {gram.organization.creation_source}.as_count()
Alert: > <threshold per hour>
```

An anomaly monitor on the same query works too, if the baseline is seasonal.

### Total organization creation volume

```
Monitor type: Metric
Query: sum(last_1h):sum:gram.organizations.created{*}.as_count()
Alert: > <threshold per hour>
```

## Responding

1. Break the counter down by `gram.organization.creation_source` to see which
   flow is responsible.
2. List the recent organizations in that window from `organization_metadata`,
   ordered by `created_at`, and look at who created them through
   `organization_user_relationships` and the audit log.
3. For `workos_sync` and `login_mirror`, check the WorkOS dashboard for the
   matching organizations, since those flows only copy what WorkOS reports.
