# Remote MCP catalogue promotion

Staff review healthy remote MCP deployments and publish explicitly approved
provider metadata into the shared catalogue from the admin dashboard or Admin
MCP. Publishing records the catalogue entry and pending setup-guide delivery
atomically. Staff track qualification and guide progress in the admin workflow.

## Configure delivery

Configure the process running the shared worker (`worker`, or `start` when it
runs the worker) with these environment values:

| Variable                                 | Purpose                                                                                                                                                       |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GRAM_CATALOG_GUIDES_GITHUB_TOKEN`       | GitHub App installation token or fine-grained PAT with repository access and Issues read/write on the guide repository. Operators manage expiry and rotation. |
| `GRAM_CATALOG_GUIDES_GITHUB_REPOSITORY`  | Guide repository, default `speakeasy-api/mcp-setup-docs`.                                                                                                     |
| `GRAM_CATALOG_PROMOTION_DELIVERY_PAUSED` | Set `true` to suspend external guide writes while retaining pending jobs, candidate discovery, and progress metrics. Defaults to `false`.                     |

The GitHub token is environment-only; there is no command-line token flag.
The admin, MCP, and private ingress processes do not consume these delivery
settings or construct the GitHub delivery client. Provision the token only to
the intended shared worker process.

Use a token whose writes trigger Actions. Actions' built-in `GITHUB_TOKEN`
suppresses most follow-on workflow events and is unsuitable for this integration.
The repository needs its `guide:draft` label, enabled **Guide draft** workflow,
`OPENROUTER_API_KEY`, and configured publisher credentials. Missing or invalid
credentials never count as queued delivery.

Ship generated database migrations in a separate migration-only PR and apply
them before merging and deploying the API and worker change. Configure external
delivery only in the intended shared worker. Local and preview environments
should leave credentials unset and set delivery paused. Every public guide
request comes from a staff-approved promotion.

The delivery pause switch takes effect when workers restart with the updated
configuration. Roll it out to every worker polling the shared queue. To halt
an already-running batch, cancel that Temporal workflow as well; its heartbeats
allow cancellation to reach blocked requests. An external request already
accepted by GitHub cannot be undone by cancellation. Reconciliation recovers
its receipt on the next permitted attempt.

## Staff UI and MCP

The admin catalogue workflow opens on active candidates, newest first by discovery
creation time, with status filters for all candidates, rejected, archived, and
published entries. Reject and Archive remove unpublished candidates from the active
queue and suspend automatic qualification until Restore. These actions preserve the
source and are distinct from unpublishing a catalogue entry. Restore requires fresh
eligibility before publication. Each change checks the reviewed curation revision.

The review form suggests an editable title, catalogue name, guide slug, shared
endpoint, authentication, and observed documentation URL where available. Source
metadata is untrusted and may be private: staff must check each value and explicitly
confirm it is suitable for public distribution. Credentials, query strings, fragments,
and arbitrary endpoint paths are not suggested. Descriptions and missing official
documentation references require staff input; the form does not invent them.
Suggestions never overwrite edits on refresh. Guide status and retry information
remain visible through the Published filter after promotion.

Enable staff Admin MCP using `GRAM_ADMIN_MCP_ENABLED` and its independent
`GRAM_ADMIN_MCP_SIGNING_KEY`. For writes, set `GRAM_ADMIN_MCP_WRITES_ENABLED=true`
and include `promote_registry_candidate`, `retry_registry_promotion_docs`, and
`curate_registry_promotion_candidate` in
`GRAM_ADMIN_MCP_WRITE_OPERATIONS`, preserving other configured operations.
Preparing a write creates an exact stored proposal; the same staff member
reviews and approves it in the admin browser. Tool discovery alone grants no
publication or retry authority.

Admin MCP uses the same business service and canonical targets as the dashboard.
`list_registry_promotion_candidates` accepts the same status filters and returns
newest-first pages. Use `prepare_registry_promotion_curation` to reject, archive, or
restore an exact candidate, then approve it in the browser and execute its proposal.
Private source-derived suggestions are confined to the staff review form; Admin MCP
retains bounded evidence and requires independently supplied public metadata.
Retrying documentation changes only the delivery attempt state, never the
approved public metadata or catalogue publication. Completed guide receipts
remain completed. Retained promotions can be recovered after their source
project is deleted; organization, promotion ID, and delivery revision remain
checked against the stored receipt.

Platform MCP assessment: shared catalogue publication and guide recovery remain
staff capabilities. Customer Platform MCP remains a consumer through existing
catalogue discovery tools and native catalogue rollout rules. No customer
publishing or recovery tool is added.

Demo seed assessment: one synthetic, ineligible candidate is seeded with
organization-scoped cleanup and postflight checks. The demo organization is
excluded from qualification. The fixture does not publish a global entry or
create a guide request.

## Eligibility and public review

Qualification observes the last 14 days after the latest source configuration
change. A candidate needs at least 10 successful direct calls across two UTC
days, activity within 48 hours, and no observed errors or unknown outcomes.
Missing or disabled telemetry does not demonstrate health. Custom connection
headers and disabled deployments are ineligible. Evidence must be refreshed
within 15 minutes before approval.

Staff supplies the public provider identity, title, description, endpoint,
documentation URL, guide slug, and authentication mode (`none` or `oauth`).
The catalogue publishes the provider identity, title, description, endpoint, and
documentation URL. The guide slug and authentication mode go to the guide factory.
Do not include customer identifiers, private endpoints, credentials, connection
headers, or customer-specific instructions.
Verify the endpoint and setup information against public primary sources.
The approved endpoint must match the observed deployment. Credentials, URL
query strings, private hostnames, and unsupported ports are rejected. Source
credentials and private display names are never copied automatically.

Promotion excludes endpoints already in the native catalogue and serializes
competing promotions of the same endpoint. Existing manual registry creation
retains its support for entries or versions sharing an endpoint and does not
participate in promotion endpoint locking. Avoid creating a manual entry while
promoting the same provider. Existing native catalogue rollout remains unchanged.

## Guide lifecycle and recovery

1. Promotion atomically publishes a catalogue entry and records pending delivery.
2. The dispatcher reconciles a GitHub issue by stable public guide slug.
3. It creates the issue if absent, then separately adds `guide:draft`.
4. The existing factory researches, drafts, reviews, validates, and opens a PR.
5. A maintainer reviews and merges the PR; existing repository workflows publish it.

`queued` means the factory accepted the issue trigger. It does not mean the guide
has been generated, merged, or published. Follow the linked issue and factory
workflow for subsequent progress. Factory runs marked `guide:blocked` require
operator input through the existing repository process. The dispatcher checks
historical label events because the factory removes the trigger while processing;
it does not automatically repeat a previously triggered factory run.

Transient failures use persisted delayed retries, starting at one hour with
exponential backoff. Five consecutive failed or interrupted attempts require
staff attention. Each attempt is bounded, and a failed record moves behind other
due work. Rate-limit responses defer the next attempt until at least the
provider's retry time; manual retry cannot bypass this delay. Invalid
configuration, conflicting metadata, and other permanent failures become
`needs_attention`. Resolve the
underlying problem, then explicitly retry from the admin UI or Admin MCP.
Success receipts are never cleared by retrying another attempt.

Guide issue reconciliation scans at most 100 pages of 100 issues or events and
also obeys the attempt deadline. Reaching a history limit blocks creation to
avoid duplicates; large repositories may require indexed reconciliation.
An issue with the same marker but a different body requires manual reconciliation.
Preserve the original body and put factory questions and answers in comments.

A crash after GitHub accepts a write but before Postgres saves its receipt is
recovered by issue/label reconciliation. Database locking serializes effects
for a promotion; public guide slugs are unique. Operator retry preserves the
same public identity and does not repeat catalogue publication.

## Scheduling, bounds, and monitoring

One shared-worker schedule runs hourly, with overlapping runs skipped and a
bounded catch-up window. Each tick has two activities: candidate discovery and
guide dispatch. Delivery still runs if discovery fails. Activities attempt once
per tick; persisted due times control later attempts. Preview queues do not
register the shared schedule. There are no per-tenant workflows or per-call signals.

Each activity selects at most 50 records with a three-minute batch deadline.
Source checks have a ten-second deadline and guide attempts have a 45-second
deadline, so one slow record cannot consume the full activity timeout. A
five-second independent cleanup budget preserves attempt receipts during
cancellation. Heartbeats carry no tenant
information. Durable database receipts, rather than heartbeat payloads, determine
what work remains after restart.

Progress gauges are exported through the existing Temporal metrics handler:

- `catalog_promotion_pending_docs`: guide requests waiting for automatic delivery.
- `catalog_promotion_needs_attention`: requests needing staff intervention.
- `catalog_promotion_oldest_pending_seconds`: age of the oldest request awaiting automatic delivery.
- `catalog_promotion_last_discovery_timestamp_seconds`: latest persisted discovery attempt.
- `catalog_promotion_discovery_failures`: sources whose latest discovery failed.
- `catalog_promotion_delivery_paused`: whether this worker has delivery paused.
- `catalog_promotion_batch_failures`: failed activity batches, tagged by phase.

Alert on sustained backlog age, any requests needing attention, and stale
discovery when sources exist. A successful workflow alone does not establish
that every source qualified or every guide was generated. Metric-read failures
fail the dispatch activity rather than silently reporting success. These metric
signals ship with the implementation; deployment-specific alert rules must be
configured in the monitoring system.

Temporal actions/month: about 720 starts + 1,440 activities = 2,160 per shared
namespace, plus delivered heartbeats for active batches. A conservative ceiling
before SDK throttling is approximately 46,800 actions if both activities consume
their full ten-minute timeout every hour (one initial heartbeat plus one every
20 seconds). Normal budgeted batches cost less. The schedule count is fixed;
database and telemetry work scales with the bounded batch of remote deployments.
A fleet of 500 sources requires roughly ten hourly discovery batches, with more
time when failures or record deadlines exhaust the batch budget.

## Rollout verification

Run domain, transport, Temporal, admin MCP, and UI tests without production
credentials. In staging, use a public test provider and dedicated guide repository.
Verify one catalogue entry, one guide issue and factory run, visible retry state,
and recovery after a lost response. Confirm pausing preserves pending jobs and
resuming does not repeat completed effects. Confirm the eventual guide PR before
enabling production delivery.
