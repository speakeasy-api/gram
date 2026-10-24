# Kit assistant adapter reference

Use this reference when implementing or diagnosing the local Speakeasy Kit
adapter. For configuration and validation steps, see
[Run Kit assistants locally](../runbooks/kit-assistant-local.md).

The workload configuration requires Kit v0.2.19 or a build containing its MCP
and inference workload support. Production has a separate
[GKE lifecycle contract](../runbooks/kit-assistant-production.md). Existing
hybrid-auth Kit runtimes are invalidated; no credential compatibility is provided.

## Bootstrap files and gateway

Provision files before starting the gateway. Private files are owned by UID/GID
1000 and use mode `0600` under `/home/kit`. Never log their contents.

| Path relative to `/home/kit`                             | Contract                                                                                                                                                              |
| -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `.kit/AGENTS.md`                                         | Global assistant instructions, loaded before workspace rules.                                                                                                         |
| `.kit/config.toml`                                       | Set `provider = "speakeasy"`, `credential_store = "file"`, `credential_dir = "/home/kit/.kit/credentials"`, and `mcp_config = "/home/kit/.kit/credentials/mcp.json"`. |
| `.kit/credentials/gateway-token`                         | Gateway bearer credential.                                                                                                                                            |
| `.kit/credentials/mcp.json` and referenced private files | MCP workload exchange configuration; no client secret or private signing key.                                                                                         |

The persistent workspace is `/home/kit/workspace`; ACP `cwd` must match it
exactly. The gateway listens on `0.0.0.0:8081` with explicit wildcard permission
and a credential file. Docker publishes the gateway only on host loopback.
The protocol endpoint is `/acp/v2`, not a legacy runner endpoint.

The database reservation's absent metadata or valid empty JSON object is an
unprovisioned sentinel. Nonempty saved metadata must match the runtime's
immutable authority. `null`, arrays, malformed JSON, and partial authority
records are not equivalent to the empty sentinel.

## Inference and MCP authority

MCP and inference use the same externally projected, short-lived assertion file.
Kit exchanges it independently for each resource through its generic workload
configuration. Inference uses RFC 7523 at `/oauth/usi/<ISSUER_ID>/token`, with
`resource=urn:speakeasy:inference` and the exact `project_id`; completion uses the
returned bearer. No API-key record or client secret is bootstrapped.

One workload issuer is shared within the project/cluster trust boundary. Each
runtime generation has an exact subject admission assigned to the assistant's
existing agent. Owner and agent inference permission remain mandatory. Retirement
revokes that admission; restore receives a fresh projected token, never a token
copied from rootfs or a snapshot.

The control plane binds verified issuer/subject identity to persisted runtime
metadata. Kit's completion body `user` field selects only an existing ACP session
inside that authority. Canonical chat ownership, assistant billing, and transcript
deduplication remain control-plane decisions; caller-supplied assistant/chat
headers do not establish authority.

## MCP configuration lifecycle

The approved `_kit/session/update_mcp` contract is **additive upsert**, not
replacement of the server set. Verify support in the runtime image before using
this API; this contract does not change or establish support in the image pinned
above.

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

The pinned Kit gateway itself accepts omitted or empty `mcpServers` as retention
(`src/gateway.rs`, released Kit `v0.2.17`, commit `90a1805`; unchanged from the
image pin `2c4a35e`). The Speakeasy adapter is deliberately stricter:
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

Use `kit/session/status` and its authoritative `settled` field while holding
runtime admission for lifecycle decisions. Foreground `idle` does not establish
that background work is settled. A failed status read is not proof of settlement.
Drain queued notifications and wait for in-flight projection to commit before
reporting a turn complete.

Require successful durable `session/close` before retiring session authority.
Transport disconnect only detaches; it does not cancel accepted work. Retrying
an ambiguous request can repeat external effects. Transcript idempotence does
not provide exactly-once tool execution.

`InterruptTurn` enqueues a durable PostgreSQL mailbox request fenced to the
runtime, processing event, and attempt. It does not open a competing ACP control
connection. With no eligible processing event/session, it returns false without
an error. The owning worker consumes the request and sends cancellation at most
once on its existing session connection while foreground work is active.

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

Runtime-generated context-upgrade notices for preexisting transcripts are
unsupported by the pinned runtime (`ErrKitUpgradeNoticeUnsupported`). Do not
import legacy history or manufacture a successful upgrade notice.

Production GKE restore accepts only rootfs-only checkpoints and starts a fresh
gateway process with renewed credentials. Whole-pod memory snapshots are rejected:
updating files cannot revoke credentials cached in restored processes. A matching
checkpoint marker and authenticated readiness are required before admission.
Verify the complete lifecycle in the target deployment before rollout.
