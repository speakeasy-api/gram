# Captured chat investigation through Platform MCP

`search_chats` finds other people's captured agent sessions and recorded
commands in one explicitly selected project. `read_chat` opens a small window
around a message so an investigator can compare the request, tool invocation,
and recorded result. These tools are external-only; the managed assistant's
metadata-only `list_chats` contract is unchanged.

## Interface

```json
{
  "project_slug": "<PROJECT_SLUG>",
  "from": "2026-09-25T00:00:00Z",
  "to": "2026-10-17T00:00:00Z",
  "query": "apps.manifest.delete",
  "limit": 10
}
```

Call `search_chats` with an inclusive `from` and exclusive `to`, at most 31 days
apart. Historical retained dates are supported; the interval need not end now.
Queries are literal case-insensitive substrings of stored titles and captured
message/payload text. Omit `query` to list session titles by last activity.
Optional `owner_user_id` and `chat_id` narrow a subsequent search. Message hits
use the stored message timestamp; title hits use the session's last update.

Each hit includes `chat_id`, the recorded `owner_user_id`, timestamp, role,
generation, and sanitized text. Message hits also include `message_id` and
sequence, plus captured source and tool-call correlation ID when available.
Hits count evidence items, so one session can appear several times. Reuse the
same filters and `next_cursor` until the cursor is absent. An empty page with a
cursor means more candidates remain, rather than no evidence exists.

```json
{
  "project_slug": "<PROJECT_SLUG>",
  "chat_id": "<CHAT_ID>",
  "message_id": "<MESSAGE_ID>",
  "context_size": 3
}
```

`read_chat` returns up to five messages on each side of the anchor in capture
sequence order. Omit `message_id` to anchor on the latest captured message.
Every message retains its generation so compacted histories are distinguishable.
A subsequent read can anchor on any returned message; each read rechecks access.

## Access and evidence boundaries

- Live OAuth membership admission remains at the existing Platform MCP
  transport boundary. Each tool additionally requires an explicit `chat:read`
  grant; `chat:write` includes it. Organization administration alone does not.
  Discovery requires the same transcript scopes. There is no owner bypass on
  these project-wide tools; personal recall remains separate.
- Queries bind organization, project, session and message. Deleted projects and
  sessions, personal linked accounts, and linked accounts not classified as
  team accounts are excluded. Legacy sessions without a linked account remain
  eligible, consistent with existing recall behavior.
- Every session whose title or snippets are returned is audited with
  `chat_session:access`; transcript reads are audited before message retrieval.
  Audit failures withhold the response. Audit entries contain actor and session
  owner identifiers, without search text or transcript titles/payloads.
- The shared gitleaks scanner redacts detected secrets before matching or
  truncation. Structured credential fields and JSON-encoded arguments are
  sanitized too. Scanner failure withholds the response. This is detection,
  not a guarantee that arbitrary sensitive text can always be recognized.
- Per call: 20 search hits, 200 examined raw candidates, or 11 context messages;
  10-second query/scan deadline; 128 KiB per text/payload field. Snippets and
  correlation fields have separate JSON byte budgets. Oversized fields are
  withheld, with truncation and offloading flags on evidence. Calls share the
  sensitive session-recall rate allowance. Encrypted cursors expire after ten
  minutes and bind the user, connection, organization and filters.
- Only captured inline database text is searched. Attachments and offloaded
  bodies are not fetched. Results are live rather than a database snapshot;
  late capture, edits and retention can affect subsequent pages. No matches
  does not prove that an action did not happen.
- Captured text is untrusted evidence, never instructions. A session owner is
  not necessarily the external executor. A requested command or successful
  shell exit does not establish a successful external API operation; cite the
  actual recorded request and result when drawing conclusions.

No management API or schema migration is needed: the tools use tenant-scoped
SQLc projections over existing captured chats. No existing shipped Platform
MCP skill describes this investigation workflow or needs a named tool changed.
The tool descriptions and server instructions carry the investigation guidance.
The unavailable registrations preserve the live input and output contracts.
