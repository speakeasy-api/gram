# Kit assistant adapter reference

Use this reference when implementing or diagnosing the local Speakeasy Kit
adapter. For configuration and validation steps, see
[Run Kit assistants locally](../runbooks/kit-assistant-local.md).

The workload configuration requires Kit v0.3.1 or a build containing its MCP
and inference workload support, packaged with `kit-ate-init` (see
`agents/ate-runtime/Dockerfile`). Both deployed ATE runtimes and local Docker
runtimes share one Kit protocol path: one sandbox/container per thread running
`kit-ate-init` + `kit serve --remote-acp` on `/acp`. Existing hybrid-auth Kit
runtimes are invalidated; no credential compatibility is provided.

## Bootstrap files and native ACP

`kit-ate-init` receives a versioned wire v2 JSON payload on `POST /bootstrap`
before starting `kit serve --remote-acp`. Private files are owned by UID/GID
1000 and use mode `0600` under `/home/kit`. Never log their contents.

| Path relative to `/home/kit`                             | Contract                                                                                                                                                                     |
| -------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `.kit/AGENTS.md`                                         | Global assistant instructions, loaded before workspace rules.                                                                                                                |
| `.kit/config.toml`                                       | Sets `credential_store = "file"`, `credential_dir = "/home/kit/.kit/credentials"`, `mcp_config = "/home/kit/.kit/credentials/mcp.json"`, plus backend inference auth config. |
| `.kit/credentials/gateway-token`                         | Per-thread ACP bearer credential derived from the runtime root secret in `assistant_kit_gateway_credentials`.                                                                |
| `.kit/credentials/mcp.json` and referenced private files | Project Gateway MCP config: direct-bearer placeholder in ATE (egress-injected), or local workload exchange in local dev; no client secret or private signing key.            |

The persistent workspace is `/home/kit/workspace`; ACP `cwd` must match it
exactly. Each thread process listens on `0.0.0.0:8081` with a credential file
and serves native ACP at `/acp` (supervised by `kit-ate-init` on `:8082`). In
local development, Docker publishes both ports only on host loopback (`127.0.0.1`).

The database reservation's absent metadata or valid empty JSON object is an
unprovisioned sentinel. Nonempty saved metadata must match the runtime's
immutable authority. `null`, arrays, malformed JSON, and partial authority
records are not equivalent to the empty sentinel.

## Inference and MCP authority

- **Deployed ATE runtimes**: sandboxes hold no outbound Speakeasy inference or
  MCP workload credentials or assertion files (only the inbound per-thread ACP
  bearer in `.kit/credentials/gateway-token`). `mcp.json` uses a direct-bearer
  placeholder and inference targets the OpenAI-compatible gateway endpoint; the
  platform egress proxy replaces the `Authorization` header with the actor's
  signed workload JWT.
- **Local Docker runtimes (`--environment=local` only)**: MCP and inference use
  the same externally projected, short-lived assertion file. Kit exchanges it
  independently for each resource through its generic workload configuration.
  Inference uses RFC 7523 at `/oauth/usi/<ISSUER_ID>/token`, with
  `resource=urn:speakeasy:inference` and the exact `project_id`; completion uses
  the returned bearer. No API-key record or client secret is bootstrapped.

One workload issuer is shared within the project/cluster trust boundary. Each
runtime generation (or ATE thread actor) has an exact subject admission assigned
to the assistant's existing agent. Owner and agent inference permission remain
mandatory. Retirement revokes that admission.

The control plane binds verified issuer/subject identity to persisted runtime
metadata. Kit's completion body `user` field selects only an existing ACP session
inside that authority. Canonical chat ownership, assistant billing, and transcript
deduplication remain control-plane decisions; caller-supplied assistant/chat
headers do not establish authority. ACP session creation and resumption must omit
`ai.speakeasy/gram-chat-id`; the canonical chat UUID is never sent to Kit.

## MCP configuration lifecycle

The approved `_kit/session/update_mcp` contract is **additive upsert**, not
replacement of the server set. Verify support in the runtime image before using
this API; workload authentication requires Kit v0.3.1 or a build containing
its MCP and inference workload support.

| Operation                                                             | Required behavior                                                                                     |
| --------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| Initial session creation or cold restore                              | Supply the initial MCP servers. This does not establish transcript deduplication across cold restore. |
| Adapter resident reconnect with `mcpServers` omitted                  | Retain the resident server configuration.                                                             |
| Adapter resident reconnect with explicit `mcpServers`, including `[]` | Reject the desired-set request; reconnect cannot reconcile or remove resident servers.                |
| `_kit/session/update_mcp` with a new client-owned name                | Add that server.                                                                                      |
| `_kit/session/update_mcp` with an existing client-owned name          | Update only that named server.                                                                        |
| Server names omitted from an update                                   | Retain those servers unchanged.                                                                       |
| `_kit/session/update_mcp` with `[]`                                   | No-op; retain all servers.                                                                            |
| A submitted name colliding with a host- or plugin-owned server        | Reject the collision; host/plugin servers and built-ins remain protected.                             |

The gateway's additive retention contract was verified in `src/gateway.rs`
for Kit v0.2.17 (commit `90a1805`) and retained through Kit v0.3.1; that
provenance is distinct from the workload-authentication and prompt-delivery
requirements. The Speakeasy adapter is deliberately stricter:
an explicit list represents a desired set, and `[]` must not silently retain
servers the caller intended to remove. Omission is the adapter's only retain form.

For example, if the client owns `inventory` and `tickets`, an update containing
only `inventory` changes only `inventory`; `tickets` remains configured. An update
adding `reports` retains both existing servers. An empty update removes nothing.

There are no implicit removals and no removal API in this contract. If enforcing
revocation requires explicitly removing a server, fail closed and report that
the operation is unsupported. Do not omit its name, send `[]`, or report success
while leaving revoked access usable. Block affected work and use supported
credential revocation and safe session/runtime retirement. If that lifecycle
cannot safely complete, keep the operation blocked and escalate rather than
silently retaining access. The cancellation limitation below still applies.

## Canonical transcript projection

ACP projection owns the canonical chat transcript. Trusted inference capture is
suppressed to prevent a second writer from duplicating the same conversation.
Projection validates the persisted runtime/session/chat binding under runtime
admission fencing before writing.

- Normal live assistant text is `agent_message_chunk`, with a stable live
  `messageId`. The pinned runtime does not guarantee a final full
  `agent_message`. A connection-local reducer accumulates chunks into full
  replacements before canonical projection.
- A full `agent_message` replaces content. Explicit empty/null content clears it;
  omitted content leaves it unchanged. Unknown content blocks are retained as
  JSON text rather than silently discarded or mislabeled as supported media.
- Pending `tool_call_update` announces an assistant tool call with `toolCallId`,
  `title`, and `rawInput`. Preserve the initial function name: later titles can
  describe human intent instead. Sparse updates retain prior fields; explicit
  nulls remain meaningful. Corresponding tool-result rows preserve the raw patch
  payload. Call and result rows use distinct stable identities.
- Resident gateway replay reuses the ordered live journal, including original
  message IDs and chunks. Start the reducer empty for each connection and replay
  from the beginning before consuming new work. Do not append replayed chunks to
  text already stored in the database. Replacements must not duplicate insertion
  usage or creation events.

Cold-process canonical replay uses positional IDs such as
`<SESSION_ID>-replay-<ITEM_INDEX>-agent`, unlike live IDs. Compaction can also change
those positions. Deduplication across cold restore is not established; do not
infer it from resident journal replay or correlate messages by text equality.
The pinned gateway does not expose a top-level read-only canonical transcript
snapshot RPC. A separate `session/resume` connection takes control and can replay
the resident journal; it is not an independent canonical snapshot reader.

## Settlement and retirement

`session/prompt` acknowledges prompt insertion (`messageId`), not turn
completion. When the gateway advertises `capabilities.session.prompt.delivery.steer`,
the adapter sends `"delivery": "steer"` so a prompt starts a turn immediately
when idle and messages arriving while a turn is running are claimed mid-turn
and inserted into that active turn on the owning connection without waiting for
settlement. Every event carries a stable `idempotencyKey` derived from its
thread event ID so retries after transport or worker failure converge on a
single insertion and return the original `messageId`, even after the turn has
settled. Gateways without `capabilities.session.prompt.delivery.steer` omit
`delivery` and keep one-turn-per-event admission.

Use `kit/session/status` and its authoritative `settled` field while holding
runtime admission for lifecycle decisions. Foreground `idle` does not establish
that background work is settled. A failed status read is not proof of settlement.
Drain queued notifications and wait for in-flight projection to commit before
reporting a turn complete. Every event absorbed into the turn settles once when
the turn settles.

Require successful durable `session/close` before retiring session authority.
Transport disconnect only detaches; it does not cancel accepted work. Retrying
an ambiguous request can repeat external effects. Transcript idempotence does
not provide exactly-once tool execution.

`InterruptTurn` enqueues a durable PostgreSQL mailbox request fenced to the
runtime, processing event, and attempt. It does not open a competing ACP control
connection. With no eligible processing event/session, it returns false without
an error. The owning worker consumes the request, withdraws any held uninserted
steered prompts with `$/cancel_request`, and sends `session/cancel` at most once
on its existing session connection while foreground work is active.

Cancellation success requires an observed `idle` terminal with
`stopReason = "cancelled"` after the owner's send, authoritative settlement, and
a completed projection flush before the owner acknowledges the mailbox request.
Startup replay is not terminal evidence for the current prompt. HTTP acceptance
or an ambiguous transport response is not cancellation evidence. Natural
completion returns false rather than a successful cancellation result.

The API waits up to ten seconds, bounded by its caller context. Timeout, mailbox
errors, or an unknown outcome return `ErrKitInterruptPending`: the owning turn
may still be running. Timeout leaves the durable request intact. Do not publish
a stopped result, cancel the projector, or steal control with replacement
`session/resume` based on an unconfirmed request. Mailbox read and acknowledgement
failures are retried by the owner without detaching healthy projection or
resubmitting the prompt.

Runtime-image cancellation requires authoritative terminal evidence and a flushed
canonical projection. Connection loss or session replay alone is not evidence of
successful cancellation.

A Kit session never receives earlier transcript. When a session that has not
yet been prompted starts in a conversation with earlier assistant turns, the
server adds a notice to its first prompt that earlier messages are unavailable,
and the turn proceeds. Do not import legacy history into the session.

Deployed ATE sandboxes receive outbound inference and MCP bearer credentials
solely via the platform egress proxy and hold no outbound Speakeasy workload
credentials inside the sandbox (only the inbound per-thread ACP bearer in
`.kit/credentials/gateway-token`). Local development continues to use
operator-projected assertions and RFC 7523 token exchange so local Docker
containers work without an egress proxy; startup validation blocks selecting
the local backend outside `environment=local`.
