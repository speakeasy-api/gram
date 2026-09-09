# Anthropic inference hooks

Receives Anthropic Enterprise's signed pre-inference transcript, stores conversation history through Gram's shared chat writer, and evaluates project security policies before returning an allow/deny verdict.

Protocol: https://platform.claude.com/docs/en/manage-claude/inference-hooks-endpoint

## Configuration

Set `GRAM_ANTHROPIC_INFERENCE_HOOKS` to a JSON array in your deployment's secret configuration:

```json
[
  {
    "id": "enterprise",
    "organization_id": "<ORG_ID>",
    "project_id": "<PROJECT_ID>",
    "tenant_id": "<ANTHROPIC_TENANT_ID>",
    "signing_secrets": ["<ANTHROPIC_SIGNING_SECRET>"]
  }
]
```

The receiver is `POST https://<GRAM_HOST>/hooks/anthropic-inference/enterprise`.
The project must belong to the configured Gram organization. The signed
`tenant_id`, when present, must match the configured Anthropic tenant. A null
tenant is authenticated by the endpoint-specific signing secret. Each endpoint has its
own secret set. Invalid configuration prevents startup; an empty variable
registers no endpoints. Configuration changes require a server restart.

Save the Anthropic configuration to obtain its `whsec_` signing secret before
using Test connection. Unsigned initial connection tests are rejected. Point
Anthropic at the final public HTTPS URL, without redirects. Ensure the ingress
accepts request bodies of at least 10 MiB. The application rejects larger bodies.

During secret rotation, configure both old and new secrets, restart, rotate at
Anthropic, then remove the old secret after in-flight deliveries have drained.
Secrets use standard base64, not URL-safe base64. Never commit actual secrets,
tenant identifiers, or organization/project identifiers.

## Storage and policies

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
blocks require correspondingly more policy work. This first implementation uses
server-side setup; it does not add a dashboard configuration page.
