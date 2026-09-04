# Revamp PostHog Slack notifications (GRW-66)

## Context

Gram's Slack notifications in `#ops-significant-events`, `#ops-aicp-events` and
`#ops-all-events` grew one destination at a time over eighteen months. Eleven
PostHog destinations now post Gram events across those three channels, with
overlapping filters, three different message formats, and two different Slack
workspace integrations.

The bigger problem is coverage. The events Growth actually wants to see do not
exist in PostHog at all. There is no event for a project being created, an MCP
server being deployed, a security policy being written, or a member joining an
existing organization. Signup fires before we know whether the user was invited
or arrived organically.

Meanwhile every one of those mutations is already recorded by Gram's audit
logger and published to Pub/Sub as an `audit_log.*_event_v1` outbox event. The
`gram streams` process already consumes that stream. The signal exists; nothing
forwards it to PostHog.

This design replaces the eleven destinations with a single well-shaped event
and three purpose-built destinations, and closes the coverage gap by deriving
activity from the audit log.

## Goals

- One PostHog event, `gram_activity`, describing everything notable that happens
  in Gram, with a stable property shape.
- A significant-events channel that carries only the six moments Growth cares
  about, and a firehose channel that carries everything.
- Distinguish invited signups from organic ones.
- No new Temporal actions and no work in any request hot path.

## Non-goals

- Changing the dashboard-side `telemetry.capture` events (`mcp_event`,
  `toolset_event`, `onboarding_event`). They stay as they are and remain
  available for funnels. This design does not migrate them.
- Building a PostHog dashboard. Alex's existing dashboard can be extended
  against the new event separately.
- Customer-facing notifications. This is internal ops signal only.

## The event

A single event named `gram_activity`, emitted server-side.

Distinct id is the actor's email when one is resolvable, so the event attaches
to the same PostHog person as the existing signup and onboarding events. It
falls back to the organization id for system- and role-actor events that have
no human behind them.

Base properties, present on every emission:

| Property            | Meaning                                                |
| ------------------- | ------------------------------------------------------ |
| `activity`          | The taxonomy name, e.g. `project_created`              |
| `organization_id`   | Gram organization id                                   |
| `organization_slug` | Organization slug                                      |
| `organization_name` | Organization display name                              |
| `project_id`        | Project id, when the activity is project-scoped        |
| `project_slug`      | Project slug, when project-scoped                      |
| `actor_email`       | Acting user's email, when resolvable                   |
| `actor_name`        | Acting user's display name                             |
| `subject_name`      | Display name of the thing acted on                     |
| `acting_surface`    | `dashboard`, `api_key`, `platform_mcp`, `assistant`, … |
| `dashboard_url`     | Deep link to the subject in the Gram dashboard         |
| `audit_action`      | The raw audit action, e.g. `mcp-server:create`         |

Deliberately absent: any "is this the first of its kind" flag. An earlier draft
carried `is_first_in_project` and `is_first_in_organization` to gate the
significant channel to the first MCP server per project. Every MCP creation is
now significant, so nothing consumes those flags, and each would have cost a
count query per event on a path that is otherwise pure lookups. Where first-ness
genuinely matters it is baked into the activity name instead, as with
`device_first_seen` and `agent_first_detected`.

Per-activity extras are added on top: `signup_source` (`invited` / `organic`),
`mcp_kind` (`hosted`, `remote`, `tunneled`, `unproxied`, `meta`), `role` on
member joins, and `policy_name` on security policies.

The taxonomy lives in Go. Which activities count as _significant_ lives in the
PostHog destination filter, so Growth can retune the significant channel without
a deploy.

## Architecture

```
service tx ──> audit logger ──> outbox row (same tx)
                                     │
                                     ▼
                            Pub/Sub webhook events topic
                                     │
                                     ▼
                       gram streams: webhookEventHandler
                                     │
              ┌──────────────┬───────┴────────┬──────────────────┐
              ▼              ▼                ▼                  ▼
         svix relay    payg key refresh  billing notif    growthsignals (new)
                                                                 │
                                                                 ▼
                                                       PostHog gram_activity
```

`growthsignals` is a new package at `server/internal/growthsignals/`. It joins
the existing fan-out in `server/cmd/gram/streams.go` rather than opening its own
subscription, so it adds no new infrastructure.

**Cost line.** `Temporal actions/month: 0 (outbox → existing streams handler, no
Temporal).` Scales with audit-log writes, which already flow through this
subscription.

### Matching on action, not event type

The handler decodes `event.Payload` into `events.AuditLogCreatedPayloadV1` and
switches on `payload.Action`, not on `event.EventType`. The event type is a
coarse bucket — MCP metadata updates and MCP server creates both publish under
`audit_log.mcp_server_event_v1` — while the action is the precise discriminator.
This also means the handler covers all 71 audit event types uniformly.

### Activity map

A curated map gives friendly names to the actions Growth named:

| Audit action                      | Activity                                     |
| --------------------------------- | -------------------------------------------- |
| `project:create`                  | `project_created`                            |
| `mcp-server:create`               | `mcp_server_created` (`mcp_kind: hosted`)    |
| `remote-mcp:create`               | `mcp_server_created` (`mcp_kind: remote`)    |
| `tunneled-mcp:create`             | `mcp_server_created` (`mcp_kind: tunneled`)  |
| `unproxied-mcp:create`            | `mcp_server_created` (`mcp_kind: unproxied`) |
| `meta-mcp:create`                 | `mcp_server_created` (`mcp_kind: meta`)      |
| `risk_policy:create`              | `security_policy_created`                    |
| `risk_policy:update`              | `security_policy_updated`                    |
| `mcp-server:update`               | `mcp_server_updated`                         |
| `mcp_metadata:update`             | `mcp_server_updated`                         |
| `mcp-server:update-tool-metadata` | `mcp_server_updated`                         |
| `organization_invitation:create`  | `member_invited`                             |

Every other audit action passes through with `activity` set to a normalized form
of the raw action, so the firehose has full coverage without an allowlist to
maintain. A small exclusion list drops known high-volume noise that has no ops
value: assistant tool calls, assistant wakes, chat session access, and platform
MCP diagnostics reads.

### Activities the audit log does not cover

Three moments are not audited today and are emitted directly, in-process, by
calling the same `growthsignals` emitter:

- **`organization_created`** — from both org-provisioning paths in the auth
  service (signup with a company name, and platform-admin invite).
- **`user_signed_up`** — at first-time user creation, carrying
  `signup_source: invited` when a pending invitation exists for that email at
  that moment, and `organic` otherwise.
- **`member_joined_organization`** — when a pending invite is accepted, at login
  or via the invite callback, carrying the granted `role`.

These reuse the existing "telemetry failures are logged, never returned" rule
already followed by `captureSignupTelemetry`: a dropped analytics event must
never fail the request that produced it.

### Devices and agents

New devices seen and new agents detected are in scope for the firehose. They are
a materially different shape from everything above, so they ship as their own
PRs at the end of the stack rather than as a later phase.

Neither is audited. The only device-related audit actions cover _integration
config_ (`device_integration:upsert` and friends), and there is no
agent-detection action at all. So neither can ride the audit path.

They also do not arrive as single-row mutations:

- **Devices** are not one table. There are four, written by three unrelated
  ingest paths: `mdm_devices` from bulk MDM snapshot syncs
  (`server/internal/deviceintegrations/sync.go`), the three
  `device_agent_*_syncs` tables from the device agent's ~60s heartbeat
  (`server/internal/agent/impl.go`), and `device_owners` from AI-provider
  account attribution (`server/internal/hooks/account_attribution.go`). None of
  them carries a project id.

  None can currently tell that a row is new. The MDM upsert is `:execrows` with
  `ON CONFLICT DO UPDATE`, so it returns 1 on both insert and update; detecting
  newness needs `RETURNING (xmax = 0)`. The heartbeat writes are worse: they are
  `:exec` with a one-minute throttle guard in the `WHERE`, so a row count would
  conflate "new" with "throttled".

  Even with newness solved, one MDM sync inserts every device it sees, so
  emitting per new row would post an organization's entire fleet to Slack the
  first time an integration is connected.

- **Agents** are not stored as detection rows at all. They are derived at read
  time by aggregating telemetry in ClickHouse
  (`server/internal/access/ai_detections.go` via
  `telemetryrepo.ListAIDetectionSummariesParams`). There is no insert to hook.

The device and agent PRs therefore add:

- `device_first_seen`, emitted from the MDM sync path for rows the sync newly
  inserted, and **suppressed for a config's first successful sync**, which is a
  backfill rather than a stream of new devices. A per-sync cap guards against a
  large fleet expansion posting hundreds of messages.
- `agent_first_detected`, emitted from the AI scan write path
  (`server/internal/telemetry/repo/ai_detections.go`). That repo already reads
  existing rows before upserting so it can preserve `first_seen`, which means a
  detection with no prior row is identifiable without a schema change. The
  existing lookup is keyed per device and user, so an organization-first signal
  needs one extra ClickHouse query keyed on organization and target only. No new
  table is required.

Both route to `#ops-aicp-events` only, never to the significant channel. The
firehose destination needs no change to pick them up, because they are the same
`gram_activity` event.

## PR stack

The work ships as five stacked PRs, each branched from the one before, so review
stays tractable and the risky parts land last.

| PR  | Scope                                                                                                                                                   | Touches                                     |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------- |
| 1   | Core `growthsignals` package: activity taxonomy, action map, event builder, emitter and enricher interfaces, TTL cache. No wiring, no behaviour change. | new package only                            |
| 2   | Audit-event stream handler plus fan-out wiring. Turns on every audit-derived activity.                                                                  | `growthsignals/`, `cmd/gram/streams.go`     |
| 3   | Direct emits: `organization_created`, `user_signed_up` with invited-vs-organic, `member_joined_organization`.                                           | `auth/`, `organizations/`, `auth/identity/` |
| 4   | Devices: newness detection across the MDM and heartbeat write paths, backfill suppression, per-sync cap.                                                | `deviceintegrations/`, `agent/`, `hooks/`   |
| 5   | Agents: org-scoped first-detection query and emit.                                                                                                      | `telemetry/repo/ai_detections.go`           |

PR 1 is the contract every later PR codes against, so it merges first. PRs 2 and
3 are independent of each other. PRs 4 and 5 are the highest-risk and land last,
behind signal that the pipeline already works.

Each PR carries a changeset with `"server": patch`. PR 4 changes SQL queries, so
it must commit the regenerated sqlc output.

### Enrichment and caching

The audit payload carries organization id, project id, actor id, and display
names, but not the organization slug or name, the project slug, or the actor's
email. Those need lookups.

The handler keeps a small in-process TTL cache keyed by organization id and by
project id, so a burst of events from one org costs one query rather than one
per event. Actor email is resolved from the user repo, also cached. A failed
lookup degrades the event rather than dropping it: the property is omitted and
the event still ships.

### Filtering

The demo organization is skipped entirely, so the daily reseed does not spam the
channels. Internal Speakeasy users are excluded at the PostHog destination level
via `filter_test_accounts: true`, which already carries the maintained list of
internal email patterns.

## PostHog rebuild

### Teardown

These eleven Gram destinations are disabled, verified quiet, then deleted:

| Destination                                        | Channel                                 |
| -------------------------------------------------- | --------------------------------------- |
| Gram - sign up -> sig-events                       | `#ops-significant-events`               |
| Gram - first time functions -> sig-events          | `#ops-significant-events`               |
| Gram - Subscription Changes -> Sig Events          | `#ops-significant-events`               |
| Gram - feature request -> sig-events               | `#ops-significant-events`               |
| Gram - enterprise gate viewed -> sig-events        | `#ops-significant-events`               |
| Gram - Elements actions -> #ops-significant-events | `#ops-significant-events`               |
| Gram - book demo page view -> sig-events           | `#ops-significant-events` (already off) |
| Gram - overage reporting -> #significant-events    | `#ops-significant-events` (already off) |
| Gram - all actions -> #gram-events                 | `#ops-aicp-events`                      |
| Gram - Elements actions -> #gram-events            | `#ops-aicp-events`                      |
| Gram - all actions -> #all-events                  | `#ops-all-events`                       |

`Gram - identity provider interest` is left alone: it is already disabled and
points at a different channel.

### New destinations

All five are created on Slack workspace integration 57009, which already posts
successfully to all three channels. Each is created disabled, tested with a
sample invocation, then enabled.

| Name                      | Channel                   | Fires on                                                                                                                                                                              |
| ------------------------- | ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Gram significant          | `#ops-significant-events` | `gram_activity` where `activity` is one of `organization_created`, `user_signed_up`, `member_joined_organization`, `project_created`, `mcp_server_created`, `security_policy_created` |
| Gram firehose             | `#ops-aicp-events`        | every `gram_activity`                                                                                                                                                                 |
| Gram subscription changes | `#ops-significant-events` | `gram_subscription_changed`                                                                                                                                                           |
| Gram feature requests     | `#ops-significant-events` | `feature_requested`                                                                                                                                                                   |
| Gram enterprise gate      | `#ops-significant-events` | `enterprise_gate_viewed`                                                                                                                                                              |

Every MCP server creation reaches the significant channel, not only the first per
project. MCP settings changes ride the firehose into `#ops-aicp-events` and get
no destination of their own: the firehose already carries every activity, so a
second destination would only duplicate them into a channel nobody watches for
this. `#ops-all-events` therefore receives nothing from this design.

The last three carry over today's behaviour on their own events; they are
recreated cleanly rather than folded into `gram_activity` because they are
genuinely different events with different properties.

All five share one message template: actor, activity, org and project, and a
button linking to the subject in the Gram dashboard.

## Testing

- Unit tests for the action-to-activity mapping, including the pass-through and
  exclusion behaviour.
- Handler tests following the `billingnotifications` handler test pattern, with
  a fake PostHog client and a fake enricher capturing forwarded payloads. These
  need no database and run in parallel. Envelopes are built with
  `webhooksv1.Event_builder{...}.Build()`.
- One integration test against the test database covering real enrichment.
- Tests for the three direct emits, in particular that `signup_source` is
  `invited` when a pending invitation exists and `organic` when it does not.
- A test that the demo organization is skipped.
- Manual end-to-end run on the local stack: create a project and an MCP server,
  confirm `gram_activity` arrives in the PostHog Dev project (96574) with the
  expected properties.

## Rollout

1. Ship the Go change. Events begin flowing to PostHog with no destinations
   consuming them yet, so Slack stays quiet.
2. Confirm `gram_activity` volume and shape in PostHog over a day.
3. Create the six new destinations disabled, test-invoke each, enable them.
4. Disable the eleven old destinations in the same sitting, so there is no
   window of doubled notifications.
5. After a week of clean signal, delete the eleven disabled destinations.
6. PRs 4 and 5 land devices and agents. No destination
   change is needed, since both are `gram_activity` and the firehose already
   accepts every activity.

## Open risks

- **Firehose volume is unknown until step 2.** If `#ops-aicp-events` proves too
  loud, the fix is a filter change on one destination, not a deploy.
- **Enrichment adds queries to the streams handler.** The TTL cache should keep
  this to a handful of queries per burst; if it does not, the fallback is to
  drop enrichment to the ids the payload already carries.

## Appendix: the shared Slack message template

All six destinations use one template, so every Gram notification reads the same
way regardless of channel. PostHog's Slack destination templates support hog
expressions, including `??` and ternaries, which today's destinations already use.

Plain-text fallback:

```
{event.properties.activity} in {event.properties.organization_name} by {event.properties.actor_email ?? 'system'}
```

Blocks:

```json
[
  {
    "type": "section",
    "text": {
      "type": "mrkdwn",
      "text": "*{event.properties.activity}* in *{event.properties.organization_name}*\n{event.properties.actor_email ?? 'system'} · {event.properties.subject_name}"
    }
  },
  {
    "type": "context",
    "elements": [
      {
        "type": "mrkdwn",
        "text": "`{event.properties.organization_slug}{event.properties.project_slug ? concat('/', event.properties.project_slug) : ''}` · via {event.properties.acting_surface}"
      }
    ]
  },
  {
    "type": "actions",
    "elements": [
      {
        "type": "button",
        "text": { "type": "plain_text", "text": "Open in Gram" },
        "url": "{event.properties.dashboard_url}"
      },
      {
        "type": "button",
        "text": { "type": "plain_text", "text": "View Event" },
        "url": "{event.url}"
      }
    ]
  }
]
```

**This imposes a requirement on the emitter.** Slack rejects a button whose `url`
is empty, which would fail the whole message. So `dashboard_url` must be present
on _every_ `gram_activity` emission, never omitted. Where an activity has no
natural subject page, it falls back to the organization's dashboard page, and
where even the organization slug is unresolvable it falls back to the Gram site
root. This is the one property exempt from the "omit empty properties" rule.
