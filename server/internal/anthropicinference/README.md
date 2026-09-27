# Anthropic inference hooks

Receives Anthropic Enterprise's signed pre-inference transcript, stores conversation history through Gram's shared chat writer, and evaluates project security policies before returning an allow/deny verdict.

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
   set the timeout to **10 seconds**, and save.

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
  Missing session identifiers or actor identities fall back to the request identifier.
- Archival deduplication is separate from acceptance. Storage uses message hashes
  to align a delivery with the eight newest archived message identities. It tries
  the newest anchor first and scans incoming messages backward, stopping at the
  first matching hash without comparing earlier history. A newly appended message
  identical to the anchor is therefore treated as already archived. Compaction
  summaries before an archival anchor are not archived again. The
  legacy count fallback remains for history without a matching hash; archived
  rows are immutable and in-place edits are not reconciled. None of these
  archival decisions exempts content from enforcement.
- User emails resolve only against connected users in the configured organization.
  If an actor stops resolving, its conversation retains the last known user for
  both enforcement and stored-message attribution. Actors with no known identity
  receive organization-wide policies; another actor's identity is never borrowed.
- Enforcement skips historical content only when it matches a durable accepted
  checkpoint: `chats.inference_accepted_checkpoint` is a last-known-good marker,
  not the archive cursor. Attempted messages, including denied and fail-open
  attempts, are archived independently in `chat_messages` for the UI. The
  existing archival alignment does not append in-place rewrites of older
  messages; acceptance alignment still rescans those changes. The checkpoint stores the complete accepted frame's role/content
  hashes, its format version, resolved user, and an enforcement-context fingerprint.
  The fingerprint includes enforcing policies, current principal grants resolved
  through the scanner's authorization helpers, exclusions, and custom rules.
  Audience membership, evaluate/bypass grants, and exclusion edits therefore
  invalidate acceptance even without a policy-version change. Old archived
  history without a checkpoint and unknown checkpoint formats are rescanned.
  Scanner completeness is separate from disposition: analyzer failures keep
  the existing policy fail-open/fail-closed behavior, but incomplete evaluations
  never advance the marker. No applicable policies and out-of-scope content
  are complete. An in-scope prompt policy intentionally disabled by its feature
  flag still allows, but does not establish acceptance: flag state is not part
  of the durable fingerprint, so content not previously accepted is rescanned
  after enabling it. Previously accepted content remains valid across a
  disable/re-enable cycle when the enforcement context is otherwise unchanged.
  Bump the checkpoint format when built-in enforcement input or scanner
  semantics change.
- A matching accepted prefix, including a uniquely aligned retained tail after
  compaction without a new leading summary, can be skipped. A new leading
  summary causes the whole frame to be scanned; retained-tail alignment beyond
  that summary is archival-only. A mismatch starts scanning at that message;
  a later matching anchor never hides earlier edits. Ambiguous repeated anchors and
  newly introduced compaction summaries are conservatively rescanned. The
  current turn (messages after the last assistant reply) is always scanned.
- Each block keeps its native scope: user, assistant, tool request, tool response,
  or prompt attachment. Block, warn, and quarantine matches deny the current
  inference. There is no interactive warning acknowledgement or persistent
  session quarantine here. A corrected retry can succeed.
- Concurrent deliveries use optimistic compare-and-swap (CAS), scoped to the
  project/actor/session conversation. Loading a checkpoint retains its exact
  stored bytes, not a pooled connection. Archival and scanning run without a
  held transaction or advisory lock, allowing them to share even a one-connection
  pool. Acceptance uses a short transaction to recheck enforcement context and
  atomically replace only the checkpoint originally loaded. Every acceptance
  gets a fresh token, including identical-frame retries, so another acceptance
  cannot silently overwrite an intervening change. A CAS conflict leaves the winner's
  checkpoint untouched and still allows a successfully scanned delivery; it is
  not a new denial reason. Database errors and cancellation still propagate.
- Attempts archive independently through the shared writer's idempotent message
  identities. Only after every required inference scan is complete and allows is the
  checkpoint eligible to advance. Denials, incomplete/erroring
  scans, and canceled evaluation do not advance it. A denied assistant/tool
  block is therefore scanned again on retry, even when the latest user result
  is benign. Enforcement-context changes during evaluation fail closed rather
  than accepting mixed configurations. Transaction cleanup is separately bounded.
- Tool requests are stored in structured `tool_calls` with JSON arguments, and
  results are stored as tool messages linked by call ID. Mixed text/tool messages
  use separate rows so background policies retain their native scope. Attachments
  are stored as `prompt_attachment` content parts, atomically with their parent.
  These rows still count as one incoming message for append-by-count storage.
- Stored messages also enter the shared writer's analysis pipeline. Transcript
  capture is synchronous: a storage or outer policy-scanner error returns a
  generic HTTP-200 deny verdict. Suppressed per-policy analyzer failures retain
  their existing disposition, including fail-open, without advancing acceptance.
  Configuration lookup, body reading, storage, and policy evaluation share a
  9.75-second deadline, leaving 250ms before the configured upstream timeout.
  Evaluation is capped at nine seconds and shortened when necessary to reserve
  500ms for checkpointing and 250ms for the response. These margins do not
  guarantee network delivery before the upstream timeout. The endpoint returns a
  generic HTTP-200 deny when it expires and cancels remaining work. Operational configuration lookup failures
  also deny; missing and disabled integrations return 404. Anthropic's configured
  failure posture still governs network failures/timeouts.
- Identical retries do not duplicate stored messages. Their uncertain content
  and current turn are evaluated against current policies. Unknown event types allow after signature and tenant validation;
  unknown fields, source values, and content block types are tolerated.

Configure a 10-second verdict timeout and select block-on-failure in Anthropic if
network failures must not allow inference. Long transcripts
or conservative rescans that cannot complete within the nine-second budget
receive a deny verdict. A checkpoint reduces repeated scans; it does not remove
the deadline or guarantee that every transcript fits within it.
