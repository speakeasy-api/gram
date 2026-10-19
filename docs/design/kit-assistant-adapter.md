# Kit assistant adapter reference

Use this reference when implementing or diagnosing the local Speakeasy Kit
adapter. For configuration and validation steps, see
[Run Kit assistants locally](../runbooks/kit-assistant-local.md).

These contracts apply to local runtime version 3 with Kit commit
`c3cbc0ebfb3c802bd001340d2868448074e0e187` and image
`localhost/kit-assistant-integration:c3cbc0e`. They do not enable production GKE
deployment, migrate v1/v2 transcripts, or establish cold-restore correctness.

## Bootstrap files and gateway

Provision files before starting the gateway. Private files are owned by UID/GID
1000 and use mode `0600` under `/home/kit`. Never log their contents.

| Path relative to `/home/kit` | Contract |
| --- | --- |
| `.kit/AGENTS.md` | Global assistant instructions, loaded before workspace rules. |
| `.kit/config.toml` | Set `provider = "speakeasy"`, `credential_store = "file"`, `credential_dir = "/home/kit/.kit/credentials"`, and `mcp_config = "/home/kit/.kit/credentials/mcp.json"`. |
| `.kit/credentials/f5a93b2411bbf44d12df4db53aa064d87685df4fad637f3c61a653f4bc5b200f.json` | Version-1 inference record containing `bearer_token` and the matching `origin`. `inference.json` is not a recognized replacement filename. |
| `.kit/credentials/gateway-token` | Gateway bearer credential. |
| `.kit/credentials/mcp.json` and referenced private files | MCP client-credentials configuration and machine secrets; not a fabricated credential-store record. |

The persistent workspace is `/home/kit/workspace`; ACP `cwd` must match it
exactly. The gateway listens on `0.0.0.0:8081` with explicit wildcard permission
and a credential file. Docker publishes the gateway only on host loopback.
The protocol endpoint is `/acp/v2`, not a legacy runner endpoint.

The database reservation's absent metadata or valid empty JSON object is an
unprovisioned sentinel. Nonempty saved metadata must match the runtime's
immutable authority. `null`, arrays, malformed JSON, and partial authority
records are not equivalent to the empty sentinel.

## Inference and MCP authority

Every fresh process start or restore must revoke/remint inference credentials
before startup. `PrepareKitStartupBootstrap` revokes runtime-row credentials and
issues their replacement. Composition also rotates gateway authority and
provisions MCP machine authentication. Do not add a per-turn inference-token
exchange or reuse a bearer restored from a snapshot.

Session creation/restoration supplies
`_meta["ai.speakeasy/gram-chat-id"]` with the assistant thread's canonical chat
UUID. An ACP session UUID is not a substitute.

The Speakeasy provider uses authenticated `GET /chat/models` for discovery and
ordinary ACP configuration for model selection. Inference uses
`POST /chat/completions` with bearer authentication and `Gram-Chat-ID`. Catalog
discovery must retain the provider's supported-model policy through composed
client wrappers; it must not invent a fallback catalog.

The local composition accepts one configured, attached, issuer-gated HTTPS
`/mcp/<slug>` resource. Its machine token sessions use the agent principal only.
The persisted user-session issuer UUID must match the resource's advertised
OAuth issuer. Built-in Platform MCP machine authentication is outside this
composition's supported contract; neither human credentials nor inference
bearers are substitutes.

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

`InterruptTurn` returns `ErrKitInterruptControlUnavailable` for a persisted
session without dialing or sending cancellation. With no saved session, it
returns false without an error. A replacement `session/resume`
would take control away from the active projector and is not a safe workaround.
Cancellation requires control ownership that preserves active projection; no
successful live cancellation is established by the continuation/replay tests.

Runtime-generated context-upgrade notices for preexisting transcripts are
unsupported by the pinned runtime (`ErrKitUpgradeNoticeUnsupported`). Do not
import legacy history or manufacture a successful upgrade notice.

GKE memory-snapshot restore requires restarting the gateway and children with
freshly provisioned credentials. Updating files alone does not invalidate
credentials cached in restored processes. Provisioning helpers are not evidence
of safe memory restore, cancellation, or production deployment readiness.
