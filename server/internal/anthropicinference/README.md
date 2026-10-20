# Anthropic inference hooks

Receives Anthropic Enterprise's signed pre-inference transcript, stores conversation history through Speakeasy's shared chat writer, and evaluates project security policies before returning an allow/deny verdict.

Protocol: https://platform.claude.com/docs/en/manage-claude/inference-hooks-endpoint

## Configuration

An organization administrator connects from **Settings → AI Integrations →
Anthropic inference hooks → Connect**. Speakeasy generates a unique webhook URL,
automatically binds it to the organization's first active project, and stores
configuration in the existing encrypted AI integration store. There are no
per-organization environment variables or server restarts.

1. Copy the webhook URL into Claude's **Organization settings → Data and privacy →
   Inference hooks**. Test the connection and save with **Enforce verdicts** off.
2. Paste the signing secret Claude reveals into Speakeasy and save it.
3. In Claude, enable **Enforce verdicts**, choose **Block** for the failure posture,
   keep the timeout at its **5,000ms** default or higher, and save.

Claude configuration: https://platform.claude.com/docs/en/manage-claude/inference-hooks-configuration

Before a signing secret is saved, only synthetic `config-test` prompt probes
receive an allow response; they are never stored or scanned. All other unsigned
requests are rejected. Once configured, every request requires a valid signature,
including configuration tests. A signature binds each delivery to its stored
organization and project; callers cannot supply a different project binding.

Secrets are encrypted at rest and write-only in the management API. All reads and
mutations require `org:admin`; mutations and their audit events commit atomically.
The URL is stable during setup and rotation. Paste a new Claude signing secret to
rotate; each retired key is accepted for five minutes for in-flight deliveries,
including when multiple rotations happen within that window. Later rotations do
not extend an older key's expiry.
Disconnect revokes the URL immediately for subsequent deliveries. Disable the hook
in Claude as well: its failure posture determines how an unavailable endpoint is
handled. Requests already in progress can finish.

The receiver is a public HTTPS endpoint without redirects. The ingress must
accept bodies up to 10 MiB; larger bodies are rejected by the application. The
configuration is loaded per delivery, and its bound project must remain active.
Deleting the bound project makes the endpoint unavailable.

## Storage and policies

The displayed user label prefers the actor email, falling back to the provider
actor ID when no email is supplied. Conversation identity still uses the stable
actor ID. Product sources come from `source.application`: `claude-ai` becomes
Claude Chat Web, `claude-code` becomes Claude Code Web, and `claude-design` becomes
Claude Design. Unknown application names are preserved; absent sources fall back
to Anthropic inference. The ingestion origin remains `anthropic-inference`.

- User and assistant conversation history is archived independently of the
  enforcement verdict, including original content blocks, attachments' extracted
  text, tool arguments, and tool results. The final assistant response becomes
  visible when a subsequent frame includes it; this pre-inference protocol does
  not deliver a final-response event.
- Conversation identity is scoped to the project, Anthropic tenant, and actor.
  Client-asserted session identifiers cannot join another actor's conversation.
  A frame with a session identifier continues the conversation that
  identifier names, or starts it. A frame with no session identifier (the
  protocol allows that, and some products send none) continues the actor's
  archived conversation when its transcript extends that conversation's
  history; a conversation that merely starts the same way is never continued,
  two conversations that so far read the same are never chosen between, and a
  frame with no actor never continues one. Otherwise it starts a new
  conversation keyed by its request identifier. See `ResolveConversation` for
  the matching rule.
  Each frame's resolution is counted on `anthropic_inference.conversations`
  (`session`, `adopted_prefix`, `ambiguous_prefix`, `new`) by application and
  whether a session
  identifier was present.
- Archival deduplication uses message hashes
  to align a delivery with the eight newest archived message identities. It tries
  the newest anchor first and scans incoming messages backward, stopping at the
  first matching hash without comparing earlier history. A newly appended message
  identical to the anchor is therefore treated as already archived. Compaction
  summaries before an archival anchor are not archived again. The
  legacy count fallback remains for history without a matching hash; archived
  rows are immutable and in-place edits are not reconciled.
- User emails resolve only against connected users in the configured organization.
  If an actor stops resolving, its conversation retains the last known user for
  both enforcement and stored-message attribution. Actors with no known identity
  receive organization-wide policies; another actor's identity is never borrowed.
- Enforcement evaluates only the latest exchange: the last assistant message
  through the end of the frame, or the whole frame before the first assistant
  reply. That is the model output this frame delivers for the first time and
  the input the model is about to act on. Earlier messages were evaluated when
  they arrived, so they are never rescanned, including after policy, grant, or
  exclusion changes. Retrying a denied prompt or tool call is denied again,
  because the denied content is still in the latest exchange; once the
  conversation moves past it, it no longer denies. Every stored message also
  gets asynchronous risk analysis through the shared writer, so history outside
  the latest exchange, such as turns Anthropic never sent or a conversation
  first seen mid-way, still produces findings. Content edited in place before
  the archive's alignment anchor is neither enforced nor stored.
- Each block keeps its native scope: user, assistant, tool request, tool response,
  or prompt attachment. Block, warn, and quarantine matches deny the current
  inference. There is no interactive warning acknowledgement or persistent
  session quarantine here. A corrected retry can succeed.
- Tool requests are stored in structured `tool_calls` with JSON arguments, and
  results are stored as tool messages linked by call ID. Mixed text/tool messages
  use separate rows so background policies retain their native scope. Attachments
  are stored as `prompt_attachment` content parts, atomically with their parent.
  These rows still count as one incoming message for append-by-count storage.
- Stored messages also enter the shared writer's analysis pipeline. Transcript
  capture is synchronous: a storage or outer policy-scanner error returns a
  generic HTTP-200 deny verdict. Suppressed per-policy analyzer failures retain
  their existing disposition, including fail-open.
  Configuration lookup, body reading, storage, and policy evaluation share a
  4.5-second deadline, leaving 500ms of Anthropic's default 5-second timeout.
  Evaluation is capped at 4.25 seconds and shortened when necessary to reserve
  250ms for the response. These margins do not
  guarantee network delivery before the upstream timeout. The endpoint returns a
  generic HTTP-200 deny when it expires and cancels remaining work. Operational configuration lookup failures
  also deny; missing and disabled integrations return 404. Anthropic's configured
  failure posture still governs network failures/timeouts.
- Identical retries do not duplicate stored messages, and their latest exchange
  is evaluated against current policies. Unknown event types allow after signature and tenant validation;
  unknown fields, source values, and content block types are tolerated.

Keep the verdict timeout at 5 seconds or more and select block-on-failure in
Anthropic if network failures must not allow inference. Anthropic defaults to
allow-on-failure. A latest exchange that cannot be scanned within the 4.25-second
budget receives a deny verdict.
