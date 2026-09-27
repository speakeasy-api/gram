# Platform MCP: chat access for the assistant audience

Ticket: GRW-184. Status: decided, option (a) implemented.

## Problem

The project's managed (dashboard) assistant is moving from its `managed-assistant`
platform toolset to the Platform MCP server. Today that toolset gives the
assistant three chat tools:

| Managed tool            | What it returns                                                                                                                                                                                                                                                                      |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `platform_list_chats`   | Every chat in the project: id, title, raw user id, raw external user id, linked account email, assistant, source, message count, risk count, timestamps; substring `search` over id, title, user display name and emails; filters by user, assistant, risk presence, pinned, window. |
| `platform_load_chat`    | Full message content by chat id, paged by sequence and generation, with `risk_only` and `query` text search over message content.                                                                                                                                                    |
| `platform_search_chats` | ClickHouse text search over chat conversations, filtered arbitrarily.                                                                                                                                                                                                                |

The Platform MCP already says something about conversations, and it says it
narrowly. `list_my_sessions` and `continue_session` are external-only, scoped
to the caller's own captured sessions, and the digest masks every finding span,
redacts tool inputs and outputs unconditionally, and withholds prose that risk
analysis has not yet seen. Their descriptions promise "never returns other
users' sessions and never returns transcript content" and "cannot access other
users' sessions". `search_tool_calls` (GRW-180) promises "no arguments,
results, bodies, headers, URLs, trace IDs, or raw identities", and returns a
masked identity plus a short-lived, session-bound person reference. The
server instructions tell the model to report outcomes rather than mechanism
and never to volunteer an identifier. The audience contract test states the
rule directly: session recall stays external-only "because it contains
user-personal cross-project transcripts".

Moving the managed chat tools over as they are would put the widest content
reader in the catalogue right beside the tools that promise there is none.

## Options

### (a) Metadata-only `list_chats`

One tool, admitted to both audiences, org:admin, project-scoped. Each row is
when the chat started and was last active, how many messages it holds, how
many live risk findings it carries, the observed source and client, the
account type, the assistant behind it, a masked participant, and a person
reference that narrows a follow-up listing. Filters: window (closed set, at
most 30 days), risk presence, exact source label, assistant id, person
reference. Opaque session-bound cursor, bounded pages, 500-chat traversal cap.
No title, no content, no search parameter.

The window selects by activity, not creation: the dashboard query the tool
reuses keeps a chat whose last message is at or after the window start and
that was created at or before the window end, ordered by last activity, and
has no creation-time order. That is also the question an administrator asks
("which chats were active this week"), so the tool adopts it rather than
adding a second time semantics to the same query. The same query left-joins
every live assistant thread on a chat, so a chat several assistants worked in
arrives as several rows; the projection reports each chat once, as the
assistant the listing was narrowed to or otherwise its first thread, and
`total_matches` counts thread rows for such chats.

- **Dashboard questions it answers.** "How many conversations did Claude Code
  have in this project this week?" "Which chats were flagged, when, and are
  they still active?" "Is one person producing most of the flagged chats?" "Did
  the new assistant get used after we published it?" "Which app produced the
  chats we do not recognise?" Given a chat id from a risk finding (GRW-181), it
  says when the chat ran, how long it was, and who (masked) was in it.
- **What it leaks.** That a chat exists, its timing and size, its source, its
  risk count, and a masked identity that an administrator who already knows
  the person can recognise. Nothing the dashboard's chat list does not already
  show an administrator with `chat:read`, minus titles and raw identities. The
  reference codec is the one `search_tool_calls` uses, so the same handle
  works the same way across both tools and dies with the session.
- **What it costs.** One Postgres query the dashboard already runs
  (`chat.ListChats`), no schema or migration change, no new SQL, one new
  service and tool file, no new budget (the sensitive-diagnostics allowance
  already meters personal-data reads). Title is a column the query returns and
  the projection drops.
- **Interaction with the promises.** Consistent. The recall tools promise no
  transcript content; this returns none. `search_tool_calls` promises masked
  identities and expiring references; this uses the same codec and masking.
  The server instructions gain one sentence saying chat listings are metadata
  only and a transcript is read in the dashboard.

### (b) `load_chat_digest` for a chat the caller did not own

Reuse `continue_session`'s redaction (`maskFindingSpans`, `turnsFromRows`,
`sessionhandoff.Render` with tool payloads redacted) against a chat selected
by id rather than by ownership.

- **Dashboard questions it answers.** "What was this flagged chat about?"
  "What did the agent do before the finding?" Roughly what an administrator
  gets from opening the chat in the dashboard.
- **What it leaks.** Every user turn and assistant turn that risk analysis has
  scanned and found nothing in. The recall redaction is an owner-recall
  redaction: it masks _known_ finding spans and withholds _unanalysed_ prose;
  it does not attempt to make another person's conversation safe to read. A
  chat that discusses a colleague by name, a customer, a salary, or a health
  matter is served verbatim unless a Watchdog rule happens to fire. Tool names
  survive, so the digest also reveals which files and systems were touched.
- **What it costs.** New SQL: the recall queries fuse organization, owner
  `user_id`, not-deleted and the personal-account exclusion into the row
  filter, so a non-owner read needs new queries (`ListChatTranscriptMessagesForAdminDigest`
  and a findings-span variant), sqlc regeneration, a new audit event kind
  (an administrator read someone's chat, distinct from a self-recall), and a
  new per-call budget. No schema change, but not a bounded piece of (a).
- **Interaction with the promises.** Contradicts them. `continue_session`
  says it "cannot access other users' sessions" and the audience test says
  recall is withheld from the assistant because it carries transcripts. A
  digest of someone else's chat is transcript content of another user by
  construction. The server instructions would need to explain why the model
  may quote some conversations and not others, which is exactly the
  mechanism-narration they tell it to avoid.

### (c) No transcript or chat access for the assistant

Rely on `list_risk_findings` by chat (GRW-181) and `search_tool_calls` (GRW-180).

- **Dashboard questions it answers.** "Which chats carry findings and how
  many?" (GRW-181). "What tools were called, by whom (masked), and did they
  fail?" (GRW-180). Usage volume through `get_project_overview`.
- **What it leaks.** Nothing new.
- **What it costs.** Nothing to build. It costs the assistant every question
  about conversations that are _not_ flagged: chat volume by source or
  assistant, whether a rollout is being used, whether a flagged chat is a one
  off or one of forty by the same person, when a chat ran relative to a tool
  call. Risk-by-chat only sees chats with findings; tool-call search only
  sees chats that called tools. A conversation with no tool calls and no
  findings is invisible, and that is most conversations.
- **Interaction with the promises.** Consistent by omission.

## Recommendation

**(a).** It is the only option that gives the assistant the chat questions an
administrator actually asks the dashboard for while staying inside every
promise the Platform MCP has already made: no transcript content, masked
identities, expiring session-bound references, bounded pages. It needs no
schema or migration change and reuses the query, codec, masking, budget and
envelope shapes already in the package, so the cost is one service and one
tool.

(b) is rejected outright, not deferred: the recall redaction was built to hand
a person back their own work, and no bounded subset of it makes another
person's prose safe to serve. If the product ever wants administrators to read
transcripts through an agent, that is a new redaction contract and a new
promise, argued for on its own, not a reuse of this one.

(c) is what the assistant gets if (a) is switched off: the stub refuses
readably and the risk and tool-call tools still stand.

## What the assistant can no longer do

Against today's managed tools, under (a) the assistant loses:

- **Reading any message.** `platform_load_chat` is gone. No prompt, reply,
  tool input or tool output is available. The dashboard is the transcript
  reader; the assistant should send the user there with the chat id.
- **Chat titles.** Titles are derived from the first user message and are
  content. The listing drops them.
- **Text search over chats.** `platform_search_chats` and `platform_list_chats`'s
  `search` are gone. There is no way to ask "which chats mention X", because
  any match over titles or content is a content oracle.
- **Raw identities.** No user id, external user id, display name or email.
  A row carries a masked identity and, when the chat query can filter on the
  column, a reference that narrows the next listing to the same person and
  expires with the session. Chats attributed only through a linked account
  email are masked without a reference, because the query cannot narrow to
  an email without matching titles too.
- **Arbitrary time bounds.** `from`/`to` timestamps become a closed window of
  1h, 24h, 7d or 30d, capped at 30 days, like every other project read. The
  window selects chats by activity (last message at or after the start,
  created at or before the end), which is the dashboard query's semantics.
- **Unbounded paging.** Offset paging over the whole project becomes bounded
  pages behind an opaque cursor with a 500-chat traversal cap per listing.
- **Pinned, account-type, minimum-risk-score and sort controls.** Not carried
  over; none of them is a question the assistant is asked, and each is a
  filter the cursor would have to be bound to.
- **Generation and sequence paging.** Meaningless without messages.

## Contract

Tool `list_chats`, `ToolMeta{Authorization: ExternalAuthorizationOrgAdmin,
Audiences: bothAudiences, ProjectScope: ProjectScopeDefaultable}`, read-only.
Registered beside the session recall tools in `tools.go`; served as a "not
switched on" stub when the service is not composed.

Input (closed object; the assistant policy injects `project_id` and hides
both selectors):

| Field            | Type    | Meaning                                                                 |
| ---------------- | ------- | ----------------------------------------------------------------------- |
| `project_id`     | uuid    | Optional exact project. At most one selector.                           |
| `project_slug`   | string  | Optional exact project slug. Omit both for the literal default project. |
| `window`         | enum    | `1h`, `24h`, `7d` (default), `30d`. Activity: a chat is listed when its last message is at or after the window start and it was created at or before the window end. |
| `risk`           | enum    | `with_findings`, `without_findings`; omit for no filter.                |
| `source`         | string  | Exact chat source label, at most 64 characters, no control characters.  |
| `assistant_id`   | uuid    | Keep only that assistant's threads.                                     |
| `user_reference` | string  | Person reference from a previous `list_chats` row in this project.      |
| `limit`          | integer | Default 20. The tool schema admits 1–50; the service clamps a value above 50 to 50 rather than refusing it, treats 0 as the default, and refuses only a negative value. |
| `cursor`         | string  | Opaque cursor from a previous result. It pins the absolute interval the first page read. |

Output:

```
{
  "project": {"id", "name", "slug"},
  "data": DataEnvelope,            // queried_at, data_through, freshness, no_observations, resolved_window
  "chats": [{
    "chat_id", "created_at", "last_message_at",
    "message_count", "risk_findings_count",
    "source", "client", "account_type",
    "assistant_id", "assistant_name",     // one row per chat; the narrowed assistant when assistant_id was given
    "masked_identity", "user_reference"
  }],
  "total_matches": int,
  "next_cursor": string,           // omitted on the last page or at the traversal cap
  "limitations": string
}
```

Refusals: `invalid_request` (selectors, window, risk, source, assistant id,
negative limit), `not_found` (project; cursor or reference, one message for both so an
expired reference is indistinguishable from an unknown one), `rate_limited`,
`feature_unavailable` (stub).

Budget: the sensitive-diagnostics allowance, shared with the drill-down reads
and `search_tool_calls`, because a page carries masked identities and person
references. Cursor and reference: `subjectReferenceCodec`, kinds `cursor` and
`user`, ten-minute TTL, bound to organization, session binding and a query
scope. The cursor scope hashes project, named window, risk, source,
assistant and decoded identity, so a position cannot be replayed against a
different question; the absolute `from`/`to` the first page read travel
inside the sealed cursor and are restored from it, so later pages walk the
same interval rather than one that slid with the clock. The user scope is the
project alone, so a reference survives a
change of window or filter within one investigation and never resolves in
another project.

## Follow-ups

- GRW-181's `list_risk_findings` by-chat grouping returns chat ids; pair it
  with `list_chats` filtered by `risk: with_findings` in the shipped Platform
  MCP skill that walks flagged chats, once both have landed.
- The managed `platform_list_chats`, `platform_load_chat` and
  `platform_search_chats` tools are withdrawn from the assistant when the
  toolset migration completes; the managed assistant instructions should then
  say that transcripts are read in the dashboard.
