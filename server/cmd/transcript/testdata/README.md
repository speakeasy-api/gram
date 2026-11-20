# Transcript merge seed data

Each directory is one made-up session, written in the exact format of every
source that would capture it. `go run ./cmd/transcript` builds a tree from one
file and merges the others into it, in memory, with the merge in
`server/internal/transcript`. All text and ids are invented. Where real sources
share an id, the files share it: tool_use ids appear in both the transcript and
the inference frames, prompt ids only in the transcript, and inference text
carries no ids.

Run the commands from `server/`:

```sh
D=cmd/transcript/testdata
go run ./cmd/transcript tree  $D/basic/transcript.jsonl
go run ./cmd/transcript merge $D/basic/transcript.jsonl $D/basic/inference.jsonl
```

## Reading the output

- Node labels: `p` user text (a prompt or a compaction summary), `a`
  assistant text, `t` tool call, `r` tool result, numbered in the order they
  were added. Each tree line also shows the role, a short preview (tool name
  and tool_use id for calls and results), the source that added the node,
  `[synthetic]` on a compaction summary, and `continues → <label>` on a
  compaction root.
- `merge` prints one line per entry of every file after the first:
  `= … by id (<kind>)` when a natural ID matched, `= … by text` when the
  content key matched, `+ … added` otherwise. `IDs attached` lists the kinds
  of natural ID the stored node gained, and `continues → <label> attached`
  marks a stored root that gained its old leaf.
- A possible duplicate is two siblings with the same content key that do not
  both carry an ID of the same kind: the merge should have matched them. The
  command lists them and exits 1. It exits 2 on bad input.
- `matched` and `added` in the last line count the files after the first.
  Inference frames re-send the whole conversation, so their entries are
  counted once per frame.

## Scenarios

Each file into itself is a resend and must add nothing.

### basic

A prompt, a tool call, its result and a reply, then a second turn with a tool
call. `transcript.jsonl` is the Claude Code session; `inference.jsonl` has one
prompt frame per model call, each carrying the whole conversation so far.

| Command (`merge …`)                                   | Outcome                                                                                                                        |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `$D/basic/transcript.jsonl $D/basic/inference.jsonl`  | 19 matched · 0 added: text matches by content key, tool calls and results by tool_use id.                                      |
| `$D/basic/inference.jsonl $D/basic/transcript.jsonl`  | 8 matched · 1 added: the transcript attaches its claude_entry and prompt IDs, and adds the last reply, which no frame carries. |
| `$D/basic/transcript.jsonl $D/basic/transcript.jsonl` | 9 matched · 0 added.                                                                                                           |
| `$D/basic/inference.jsonl $D/basic/inference.jsonl`   | 19 matched · 0 added.                                                                                                          |

### parallel-tools

One model response with text and two tool calls. The transcript writes one
line per block, and each tool result names the tool_use line it answers; the
frames group the calls in one assistant message and both results in one user
message. Both become the same chain: text, call A, call B, result A, result B.
`inference.jsonl` also holds a `tool_call` frame, which the server does not
archive; the CLI skips it.

| Command (`merge …`)                                                     | Outcome                              |
| ----------------------------------------------------------------------- | ------------------------------------ |
| `$D/parallel-tools/transcript.jsonl $D/parallel-tools/inference.jsonl`  | 15 matched · 0 added.                |
| `$D/parallel-tools/inference.jsonl $D/parallel-tools/transcript.jsonl`  | 8 matched · 1 added: the last reply. |
| `$D/parallel-tools/transcript.jsonl $D/parallel-tools/transcript.jsonl` | 9 matched · 0 added.                 |
| `$D/parallel-tools/inference.jsonl $D/parallel-tools/inference.jsonl`   | 15 matched · 0 added.                |

### rewind-same-text

The user answers "config.yaml", goes on one more turn, rewinds to before the
answer and types "config.yaml" again (a new prompt id), then goes on
differently. The transcript has two "config.yaml" branches. The inference
frame sent after the rewind is identical to the one sent before it.

| Command (`merge …`)                                                         | Outcome                                                                                                                                                                                                                                                                                       |
| --------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `$D/rewind-same-text/transcript.jsonl $D/rewind-same-text/inference.jsonl`  | 17 matched · 0 added. Frame 3 follows the first "config.yaml": when an entry without IDs matches two siblings, the merge picks the one whose branch keeps matching the rest of the frame.                                                                                                     |
| `$D/rewind-same-text/inference.jsonl $D/rewind-same-text/transcript.jsonl`  | 5 matched · 5 added: inference cannot tell the two prompts apart, so the second branch's frames joined the first; the transcript adds the second branch (4) and the first branch's last reply (1). ID-less copies of the second branch's reply and prompt stay under the first "config.yaml". |
| `$D/rewind-same-text/transcript.jsonl $D/rewind-same-text/transcript.jsonl` | 10 matched · 0 added.                                                                                                                                                                                                                                                                         |
| `$D/rewind-same-text/inference.jsonl $D/rewind-same-text/inference.jsonl`   | 17 matched · 0 added.                                                                                                                                                                                                                                                                         |

### compaction

Two turns, an automatic compaction (a `compact_boundary` line and the summary
under it), then two more turns. Frames before the compaction start at the
first prompt; frames after it start with the summary.

| Command (`merge …`)                                             | Outcome                                                                                                                                                                                                                                                     |
| --------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `$D/compaction/transcript.jsonl $D/compaction/inference.jsonl`  | 17 matched · 0 added: the post-compaction frames match the summary root (`[synthetic] continues → a3`).                                                                                                                                                     |
| `$D/compaction/inference.jsonl $D/compaction/transcript.jsonl`  | 10 matched · 2 added: the summary root inference stored gains its old leaf (`continues → a4 attached`); the two replies no frame carries are added. The root is not flagged `[synthetic]`: the frame cannot know, and the merge result has no field for it. |
| `$D/compaction/transcript.jsonl $D/compaction/transcript.jsonl` | 12 matched · 0 added.                                                                                                                                                                                                                                       |
| `$D/compaction/inference.jsonl $D/compaction/inference.jsonl`   | 17 matched · 0 added.                                                                                                                                                                                                                                       |

### system-reminders

The frames carry `<system-reminder>` text that the transcript does not: one
before the first prompt, one appended to a tool result, and one with an
attribute before the second prompt. `inference.jsonl` puts each reminder in
the text it rides on; `inference-reminder-blocks.jsonl` sends each as its own
text block in the user message.

| Command (`merge …`)                                                                        | Outcome                                                                                                                              |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ |
| `$D/system-reminders/transcript.jsonl $D/system-reminders/inference.jsonl`                 | 9 matched · 0 added: reminders do not change the content key.                                                                        |
| `$D/system-reminders/inference.jsonl $D/system-reminders/transcript.jsonl`                 | 5 matched · 1 added: the last reply.                                                                                                 |
| `$D/system-reminders/transcript.jsonl $D/system-reminders/inference-reminder-blocks.jsonl` | 9 matched · 0 added. A block that holds only a reminder is not an entry (Split drops text that is empty once reminders are removed). |
| `$D/system-reminders/inference-reminder-blocks.jsonl $D/system-reminders/transcript.jsonl` | 5 matched · 1 added: the last reply.                                                                                                 |
| each file into itself                                                                      | 6 · 0 for the transcript, 9 · 0 for `inference.jsonl`, 9 · 0 for `inference-reminder-blocks.jsonl`.                                  |

### compliance

A Claude.ai chat as Compliance API chat messages pages. `page-1.json` holds the
first four messages (the last one has a tool call, its result and text) with
`has_more: true`; `page-2.json` is the next page; `export-full.json` is the
whole chat in one page. The importer fetches a chat's next page with
`after_id` set to the cursor of the last message it stored, so the CLI starts a
later page of the same chat after the last message merged from it.

| Command (`merge …`)                                                                  | Outcome                                                            |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `$D/compliance/page-1.json $D/compliance/page-2.json`                                | 0 matched · 2 added: page 2 continues after page 1's last message. |
| `$D/compliance/page-1.json $D/compliance/export-full.json`                           | 6 matched · 2 added.                                               |
| `$D/compliance/export-full.json $D/compliance/page-1.json`                           | 6 matched · 0 added.                                               |
| `$D/compliance/page-1.json $D/compliance/page-1.json`                                | 6 matched · 0 added.                                               |
| `$D/compliance/page-1.json $D/compliance/page-2.json $D/compliance/export-full.json` | 8 matched · 2 added.                                               |

Do not merge `page-2.json` before `page-1.json`: the importer never pages
backwards, and the CLI would hang page 1 after page 2.

### claude-ai-chat

One Claude.ai chat captured by both the Compliance API and inference hooks.
`compliance.json` is the first chat messages page (four messages,
`has_more: true`) and `compliance-page-2.json` the page after it.
`inference.jsonl` has one prompt frame per model call with
`source.application` `"claude-ai"` and a `session_id`, plus the `tool_call`
frame for the web search (skipped, as before). One assistant response
searches the web: the Compliance export holds it as one assistant message
(text, `tool_use`, `tool_result`, text), while the frames carry the call in an
assistant message, the result in the next user message and the answer in the
assistant message after that. Split makes the same four entries of both, and
the tool_use id is shared, so the call and result match by id; every other
entry can only match by text.

| Command (`merge …`)                                                                                            | Outcome                                                                                                    |
| -------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `$D/claude-ai-chat/compliance.json $D/claude-ai-chat/inference.jsonl`                                          | 17 matched · 1 added: the last prompt, which comes after page 1.                                           |
| `$D/claude-ai-chat/inference.jsonl $D/claude-ai-chat/compliance.json`                                          | 7 matched · 0 added: every Compliance entry attaches its `compliance_message` ID to what inference stored. |
| `$D/claude-ai-chat/compliance.json $D/claude-ai-chat/compliance-page-2.json $D/claude-ai-chat/inference.jsonl` | 18 matched · 2 added (page 2).                                                                             |
| `$D/claude-ai-chat/inference.jsonl $D/claude-ai-chat/compliance.json $D/claude-ai-chat/compliance-page-2.json` | 8 matched · 1 added: the last reply, which no frame carries.                                               |
| each file into itself                                                                                          | 7 · 0 for `compliance.json`, 18 · 0 for `inference.jsonl`.                                                 |

Open question: whether a frame's `session_id` equals, or maps to, the chat id
the Compliance API returns for the same chat (`claude_chat_…`) is not
verified, and the files encode no mapping. The CLI merges the files it is
given into one tree, so it tests entry-level matching either way; in
production the two sources must first land in the same chat.

### cowork-three-way

One Cowork session captured three ways: its Claude Code transcript
(`transcript.jsonl`, `entrypoint` `remote_cowork`), inference frames
(`inference.jsonl`, `source.application` `"cowork"`), and a Compliance API
local session transcript (`compliance.json`). Each prompt starts with the
send-time `<system-reminder>` Cowork adds, the same in all three. The
Compliance page opens with a synthetic marker message (the endpoint's note
that a system prompt was set), which the CLI skips.

| Command (`merge …`)                                                                                            | Outcome                               |
| -------------------------------------------------------------------------------------------------------------- | ------------------------------------- |
| `$D/cowork-three-way/transcript.jsonl $D/cowork-three-way/inference.jsonl`                                     | 32 matched · 0 added.                 |
| `$D/cowork-three-way/inference.jsonl $D/cowork-three-way/transcript.jsonl`                                     | 11 matched · 1 added: the last reply. |
| `$D/cowork-three-way/transcript.jsonl $D/cowork-three-way/compliance.json`                                     | 12 matched · 0 added.                 |
| `$D/cowork-three-way/compliance.json $D/cowork-three-way/transcript.jsonl`                                     | 12 matched · 0 added.                 |
| `$D/cowork-three-way/inference.jsonl $D/cowork-three-way/compliance.json`                                      | 11 matched · 1 added: the last reply. |
| `$D/cowork-three-way/compliance.json $D/cowork-three-way/inference.jsonl`                                      | 32 matched · 0 added.                 |
| `$D/cowork-three-way/transcript.jsonl $D/cowork-three-way/inference.jsonl $D/cowork-three-way/compliance.json` | 44 matched · 0 added.                 |
| `$D/cowork-three-way/compliance.json $D/cowork-three-way/inference.jsonl $D/cowork-three-way/transcript.jsonl` | 44 matched · 0 added.                 |
| `$D/cowork-three-way/compliance.json $D/cowork-three-way/compliance.json`                                      | 12 matched · 0 added.                 |

Assumption: this scenario assumes the Compliance API also covers Cowork
sessions, which Speakeasy's support matrix lists as unknown. The Compliance
API reference does document session transcript endpoints that name Cowork
(`GET /v1/compliance/apps/sessions/local/{id}/messages`, with
`product_surface` `"cowork"` for Cowork in Claude Desktop, and
`/sessions/remote/…` for Cowork in Anthropic's cloud), but Speakeasy does not
import them yet. `compliance.json` follows the documented local session format;
the marker's role and wording are made up.

Open question: as for the Claude.ai chat, whether the frame's `session_id`
(here the Claude Code session id) equals, or maps to, the Compliance session id
(`clls_…`) is not verified. The CLI tests entry-level matching; in production
the sources must first land in the same chat.

## Example

```
$ go run ./cmd/transcript merge $D/compaction/inference.jsonl $D/compaction/transcript.jsonl
tree from …/compaction/inference.jsonl (anthropic_inference, 5 frames): 10 nodes

merge …/compaction/transcript.jsonl (claude_transcript, 1 delivery)
  = p1   "Add a /healthz endpoint to the server."       by text, IDs attached (claude_entry, prompt)
  = a1   "I'll add it to the router."                   by text, IDs attached (claude_entry)
  = t1   Edit toolu_01ExampleCompactEdit00001           by id (tool_call), IDs attached (claude_entry)
  = r1   toolu_01ExampleCompactEdit00001 "The file /h…" by id (tool_result), IDs attached (claude_entry)
  = a2   "Added /healthz. It answers 200 OK with an…"   by text, IDs attached (claude_entry)
  = p2   "Add a test for it."                           by text, IDs attached (claude_entry, prompt)
  + a4   "Added TestHealthz in server/router_test.g…"   added
  = p3   "Summary of the conversation so far, writt…"   by text, IDs attached (claude_entry), continues → a4 attached
  = p4   "Now document the endpoint in the README."     by text, IDs attached (claude_entry, prompt)
  = a3   "The README has a new Health check section…"   by text, IDs attached (claude_entry)
  = p5   "Thanks, that's all."                          by text, IDs attached (claude_entry, prompt)
  + a5   "You're welcome."                              added

final tree
p1   user      "Add a /healthz endpoint to the server."       · anthropic_inference
a1   assistant "I'll add it to the router."                   · anthropic_inference
t1   assistant Edit toolu_01ExampleCompactEdit00001           · anthropic_inference
r1   tool      toolu_01ExampleCompactEdit00001 "The file /h…" · anthropic_inference
a2   assistant "Added /healthz. It answers 200 OK with an…"   · anthropic_inference
p2   user      "Add a test for it."                           · anthropic_inference
a4   assistant "Added TestHealthz in server/router_test.g…"   · claude_transcript

p3   user      "Summary of the conversation so far, writt…"   · anthropic_inference continues → a4
p4   user      "Now document the endpoint in the README."     · anthropic_inference
a3   assistant "The README has a new Health check section…"   · anthropic_inference
p5   user      "Thanks, that's all."                          · anthropic_inference
a5   assistant "You're welcome."                              · claude_transcript

10 matched · 2 added · 0 possible duplicates
```

## How the CLI turns each file into deliveries

Every adapter splits each message with `transcript.Split`, so all sources
split the same way.

- Claude transcript: one delivery for the whole file (a real upload is a run
  of lines). Each user or assistant line gives the entries Split makes of its
  content, each with a `claude_entry` ID (uuid, block position); the first
  user text of a turn also carries its `promptId`, and tool blocks carry their
  tool_use id. The first entry of a line names `parentUuid` (last block). Meta,
  sidechain, attachment, system and thinking-only lines become links to their
  parent; a `compact_boundary` becomes a compaction link to
  `logicalParentUuid`; the `isCompactSummary` line becomes a synthetic entry
  that starts the new tree.
- Inference: one delivery per `prompt` frame, the whole trajectory from the
  root with no parent named. `tool_call` frames are skipped.
- Compliance: one delivery per page, each entry with a `compliance_message`
  ID (message id, block position), in order; a later page of the same chat
  or session starts after the last message merged from it. Chat messages
  pages and local session messages pages are both read; synthetic marker
  messages in a session page are skipped.

## Format sources

### Claude Code session transcript (JSONL)

- Claude Code hooks reference, <https://code.claude.com/docs/en/hooks>:
  `transcript_path` ("Path to conversation JSON", example
  `~/.claude/projects/.../transcript.jsonl`) and the `prompt_id` input field.
  Anthropic does not publish the line schema.
- `server/internal/hooks/claude_transcript.go`: the lines the server accepts
  (`claudeTranscriptLine`: `type`, `uuid`, `timestamp`, `isSidechain`,
  `isMeta`, `isCompactSummary`, `promptId`, `message.id/model/content`;
  content is a string or blocks; a tool result's content is a string or text
  blocks) and `server/internal/hooks/claude_cowork_rows.go` (prompt and tool
  keys).
- `hooks/relay/transcript_ship.go`: the upload body (raw JSONL lines in
  `entries`); the shipper sends only `user` and `assistant` lines.
- The rest of the shape (`parentUuid`, `logicalParentUuid`, `subtype`, line
  kinds and how they chain) was read from local Claude Code session files,
  keys and line kinds only, no content: an assistant response is written one
  content block per line, the lines sharing `message.id`; thinking blocks are
  lines of their own; a tool result is a user line with
  `sourceToolAssistantUUID`, and its `parentUuid` is the tool_use line it
  answers; `attachment` lines (`hook_success`, …) and `system` lines
  (`turn_duration`, `stop_hook_summary`) sit in the parent chain; a session
  starts with a `hook_success` attachment whose `parentUuid` is null; a
  `compact_boundary` is a `system` line with a null `parentUuid`,
  `logicalParentUuid` naming the old leaf and `compactMetadata.trigger`; the
  summary is a `user` line with `isCompactSummary` and
  `isVisibleInTranscriptOnly`, string content, `parentUuid` naming the
  boundary, and the `promptId` of the next prompt.

### Anthropic inference hooks frame

- Develop an Inference hooks integration,
  <https://platform.claude.com/docs/en/manage-claude/inference-hooks-endpoint>:
  the prompt frame (`type`, `request_id`, `tenant_id`, `actor`, `source`,
  `messages`, `session_id`, `model`, `metadata`), content blocks (`text`;
  `tool_use` with `id`, `tool_name`, `input`, and `tool_info` on tool call
  frames only; `tool_result` with `content` as text, `is_error`, `tool_name`,
  `tool_use_id`; `attachment`), tool results under the `user` role, and the
  `tool_call` frame, which carries only the latest assistant message.
- The same page's `tool_info` section: claude.ai's own web search is an
  application tool (`"type": "application"`) whose `tool_use` id is a
  `toolu_…` id; `source.application` values include `claude-ai`,
  `claude-code` and `cowork`.
- Inference hooks overview,
  <https://platform.claude.com/docs/en/manage-claude/inference-hooks>: "Each
  request includes the whole conversation".
- Production metrics (from the coordinator, not in the repo): Claude.ai chat
  frames arrive with `source.application` `"claude-ai"`, and almost all carry
  a `session_id`.
- `server/internal/anthropicinference/protocol.go` (`Frame`, `Message`, and
  `contentBlock`, whose `content` is a string and whose tool name is
  `tool_name` with `name` as a fallback), `storage.go` and `service.go` (block
  to row), the fixtures in `service_test.go` and `tool_call_test.go`, and the
  package `README.md`. The CLI decodes frames with `anthropicinference.Frame`.

### Anthropic Compliance API chat messages

- Get chat messages,
  <https://platform.claude.com/docs/en/api/compliance/apps/chats/messages>:
  `GET /v1/compliance/apps/chats/{claude_chat_id}/messages`; `chat_messages`
  with `id` (`claude_chat_msg_…`), `role`, `created_at` and content blocks
  (`text`; `tool_use` whose `input` is a JSON-encoded string; `tool_result`
  whose `content` is an array of text items); `has_more`, and `first_id` /
  `last_id` as opaque cursors for `before_id` / `after_id`.
- `server/internal/thirdparty/anthropic/client.go` (`ChatMessagesPage`,
  `ChatMessage`, `GetChatMessages` with `order=asc`) and
  `server/internal/aiintegrations/compliance_import.go` (skips messages without
  an id or a user/assistant role, renders content, stores each page's
  `last_id` as the chat's cursor and fetches the next page after it). The CLI
  decodes pages with `anthropic.ChatMessagesPage`.
- Retrieve local session messages,
  <https://platform.claude.com/docs/en/api/compliance/apps/sessions/local/messages>,
  and the endpoint list, <https://platform.claude.com/docs/en/api/compliance/apps>:
  `data` messages with `type` `compliance_local_session_message`, `id`
  (`clsm_…`), `role`, `created_at`, `model`, `provenance` (null, or
  `content_unavailable`, `client_asserted`, `synthetic_marker`) and the same
  `text` / `tool_use` / `tool_result` blocks as chat messages, without
  `integration_name` and `mcp_server_url`; `next_page`; `session` with `id`
  (`clls_…`) and `product_surface` (`cowork`, `claude_code`, …). Thinking is
  never included, the system prompt only as a marker. No repo code reads this
  endpoint, so the CLI's `complianceSessionPage` follows the reference.

### Cowork

- `hooks/relay/transcript_ship.go`: inside the Cowork VM Claude Code reports
  `CLAUDE_CODE_ENTRYPOINT` `remote_cowork`, and the hooks binary ships the
  session transcript from Stop.
- `server/internal/hooks/cowork_warn_confirm_test.go` and
  `server/internal/risk/prompt_fingerprint_test.go`: a Cowork prompt starts
  with a `<system-reminder>` that carries the send time.

## Assumptions

- Claude Code's line schema is undocumented; the shape above is what current
  Claude Code writes and what the server's parser reads.
- The text of a prompt, reply and tool result is the same in the transcript
  and in the frames. Tool calls and results match by tool_use id either way.
- After a compaction, the next frame starts with the summary as the first user
  message, with the same text as the transcript's summary line. The inference
  hooks docs do not cover compaction.
- `server/internal/transcript/key.go` assumes frames carry the client's
  `<system-reminder>` text. The docs say frames never include system prompts or
  Anthropic-internal context, and do not say whether client-injected reminders
  arrive inside the prompt's text block or as blocks of their own, so the
  scenario has both shapes.
- `page-2.json` is the response to `after_id` set to page 1's `last_id`. That
  cursor is opaque, so the CLI names page 1's last message as the parent
  instead.
- A claude.ai application tool (web search) runs between two model calls, so
  the prompt frame after it carries the call and its result, under the
  assistant and user roles. The Compliance export holds the same response as
  one assistant message. The tool_use id is the same in both.
- The text of each message, and of a tool result, is the same in the
  Compliance export and the frames. Tool results match by id, so a
  difference there would not matter; the Compliance docs say non-text result
  items (images, links) are left out.
- A Compliance user message can hold an `added_as_context` text block whose
  `text` is empty (the app's own notes are not exported). Split drops it, like
  the reminder-only blocks above; the seed messages have none.
- In a Cowork session the prompt text, send-time reminder included, is the
  same in the transcript, the frames and the Compliance session transcript.

## Shapes seen in real transcripts but not seeded

The CLI tolerates these and prints a note:

- A reply written before the prompt and attachments it follows. The CLI merges
  lines parents first.
- A `compact_boundary` whose `logicalParentUuid` names a line after the summary
  (inside the new tree). The CLI drops that continuation.
- A line written again with the same uuid after a partial compaction
  (`compactMetadata.preservedMessages`). The merge copies the kept lines under
  the summary as a new subtree, and the turns after them follow the copies; the
  old tree is unchanged.
- A parent in another file (a continued session). The line starts the
  conversation.
