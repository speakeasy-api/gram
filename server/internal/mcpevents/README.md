# Platform MCP review events

Speakeasy exposes experimental native MCP webhook subscriptions for external OAuth connections. Administrators subscribe to `review.requested` for one project; members subscribe to `review.decided` for reviews they requested. Events contain project, request, and optional decision IDs only. Consumers must read current state through the existing authorized review tools before acting.

This surface implements `events/list`, `events/subscribe`, `events/unsubscribe`, and authenticated `server/discover` capabilities. It adds no tool: the native event protocol handles subscriptions, while existing review tools handle inspection and decisions. Tool schemas and shipped skills remain unchanged. Managed assistants are excluded because their execution still needs a recipient identity and authorization model. There is no dashboard data surface or useful seeded subscription: callbacks and signing keys belong to real OAuth connections.

## Contract

The adapter targets protocol version `2026-07-28`, the [pinned MCP Events draft](https://github.com/modelcontextprotocol/experimental-ext-triggers-events/blob/d5316be214be2e76e51462069c1d67e00a5a7619/docs/design-sketch-proposal.md), and the [ChatGPT integration profile](https://developers.openai.com/plugins/build/mcp-events). Callback verification errors use the ChatGPT profile's `-32015`; the draft uses `-32027`. Actual ChatGPT interoperability still requires a hosted receiver/client validation before rollout.

Arguments must contain exactly one `project_id` UUID. Only HTTPS webhooks are supported, with connection-time SSRF protection and no redirects. The receiver supplies a Standard Webhooks secret and must echo a signed random verification challenge. Signing keys are encrypted at rest. Refresh keeps the subscription identity and lifetime generation; key rotation signs with both keys for five minutes. Expired or unsubscribed identities begin a new generation when renewed, preventing old deliveries from becoming eligible again.

Subscriptions last between one minute and 24 hours, with a 24-hour default, and are capped at 20 active subscriptions per connection generation and project. Supplied replay cursors return `truncated: true`; replay, polling, and streaming are unsupported. Discovery is private and uncached because capabilities depend on the authenticated principal.

## Production and delivery

Review admission and decisions publish domain events in their database transaction through the existing outbox. Requested events represent initial demand, reopening, or an additional requester while pending; unchanged repeat requests and evidence-only dossiers do not produce them. Decisions capture the requester set at commit time.

Pub/Sub routing persists one delivery per occurrence, subscription, and lifetime generation, then publishes its handoff in the same transaction. Delivery rechecks current organization capability, feature flags, membership, connection generation and authorization lifetime, project access, and review visibility. Authorization queries share the transaction's database connection. Unsubscribe and delivery serialize on the subscription identity.

Delivery is at least once and may arrive out of order. Receivers deduplicate using the stable `webhook-id`/`eventId`. Network failures and HTTP 408, 425, 429, and 5xx responses retry through Pub/Sub, bounded by eight attempts and a 24-hour delivery lifetime. HTTP 410 and 413 are terminal. Each callback is limited to ten seconds and a 4 KiB response. No workflow runs per event.

An hourly worker sweep deletes subscriptions 24 hours after expiration or unsubscribe, and delivery records after seven days. References are detached in bounded batches before subscription deletion so foreign-key actions cannot cause unbounded writes. Each phase handles at most 50,000 rows per attempt in batches of 500. Temporal actions/month are approximately 1,440 per task queue per namespace, fixed; three activity attempts increase the maximum to about 2,880. Preview queues each register their own schedule.

## Rollout

1. Deploy the schema migration independently, before application code.
2. Deploy generated Pub/Sub topics and subscriptions before enabling application delivery.
3. Deploy the server and streams consumer changes. Keep the organization-targeted `platform-mcp-events` flag disabled until client validation; the existing Platform MCP capability and MCP approval flag are also required.
4. Validate a real ChatGPT Work/Cloud subscription against the hosted endpoint before enabling selected organizations. The local tests use a controlled TLS receiver and the actual MCP HTTP handler.

Disabling the event flag stops new subscriptions and suppresses delivery on the next attempt. The review domain events are still published independently of client rollout. No deployment or flag enablement is part of the code change.
