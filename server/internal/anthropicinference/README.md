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
rotate; the previous key is accepted for five minutes for in-flight deliveries.
Disconnect revokes the URL immediately for subsequent deliveries. Disable the hook
in Claude as well: its failure posture determines how an unavailable endpoint is
handled. Requests already in progress can finish.

The receiver is a public HTTPS endpoint without redirects. The ingress must
accept bodies up to 10 MiB; larger bodies are rejected by the application. The
configuration is loaded per delivery, and its bound project must remain active.

## Storage and policies

The displayed user label prefers the actor email, falling back to the provider
actor ID when no email is supplied. Conversation identity still uses the stable
actor ID. Product sources come from `source.application`: `claude-ai` becomes
Claude Chat Web, `claude-code` becomes Claude Code Web, and `claude-design` becomes
Claude Design. Unknown application names are preserved; absent sources fall back
to Anthropic inference. The ingestion origin remains `anthropic-inference`.

- All supplied user and assistant history is archived, including original content
  blocks, attachments' extracted text, tool arguments, and tool results. The final
  assistant response becomes visible when a subsequent inference frame includes
  it; this pre-inference protocol does not deliver a final-response event.
- Conversation identity is scoped to the project, Anthropic tenant, and actor.
  Client-asserted session identifiers cannot join another actor's conversation.
  Missing session identifiers or actor identities fall back to the request identifier.
- Message identity combines transcript position and canonical content. Repeated
  full transcripts and growing prefixes deduplicate. Repeated utterances at
  different positions are retained. Edited branches are preserved as additional
  history. Compacted/reordered transcripts can produce additional records because
  Anthropic does not supply stable message identifiers or message timestamps.
- User emails resolve only against connected users in the configured organization.
  Unknown actors receive organization-wide policy evaluation, with no fallback to
  an administrator's identity.
- The shared risk scanner evaluates every known content block with its native
  scope: user, assistant, tool request, tool response, or prompt attachment.
  Block, warn, and quarantine matches deny the current inference. There is no
  interactive warning acknowledgement in this protocol. Quarantine matches here
  deny the frame; this receiver does not create a persistent session quarantine.
- Stored messages also enter the shared writer's analysis pipeline. Transcript
  capture is synchronous: a storage or policy-scanner error returns a generic
  HTTP-200 deny verdict, so an outage does not silently permit uninspected input.
  Anthropic's configured failure posture still governs network failures/timeouts.
- Retries do not duplicate stored messages and are evaluated against current
  policies. Unknown event types allow after signature and tenant validation;
  unknown fields, source values, and content block types are tolerated.

Configure a verdict timeout large enough for storage and policy evaluation
(Anthropic permits up to 10 seconds), and select block-on-failure in Anthropic if
network failures must not allow inference. Long transcripts with many content
blocks require correspondingly more policy work.
