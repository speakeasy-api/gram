# Chat Detail Sheet — Bug Investigation Summary

## Bug 1 — "resource not found" when opening a session

### Symptom
Clicking some agent-session rows in the cost/session table opens the chat detail
sheet, which calls `GET /rpc/chat.load?id=…` and fails with **`resource not
found`**. The error takes down the **whole page** (error boundary), not just the
sheet.

### Root cause — two independent data stores, linked only by session id
The session **list** and the session **transcript** come from entirely different
places:

| | Cost/session table (the row) | Chat detail sheet (`chat.load`) |
|---|---|---|
| Store | ClickHouse `telemetry_logs` | Postgres `chats` / `chat_messages` |
| Written by | `writeClaudeOTELLogsToClickHouse` — the **OTEL exporter** stream (`hooks/otel.go:161`) | `persistConversationEvent` / `writeToolCall*` — the **hooks** stream (`hooks/session_capture.go`) |
| Gated by | nothing (always written) | `FeatureSessionCapture` flag (`session_capture.go:204`) |
| Session key | `gram_chat_id` = raw `gen_ai.conversation.id` ← `session.id`, stored as-is (`otel.go:281`, `logger.go:237`) | `chats.id` = `sessionIDToUUID(session.id)` (`session_capture.go:79`) |

`ListSessions` reads **only** `telemetry_logs` (`telemetry/repo/sessions.go:106`),
so the cost table is a **superset** of the sessions that actually have a loadable
transcript. `chat.load` 404s (`impl.go:347-351`, `GetChat` → `ErrNoRows`)
whenever the Postgres transcript was never written for that id. That occurs when:

1. `session_capture` was **off** when the session ran (telemetry still flows;
   transcript skipped),
2. the session exported **OTEL telemetry but never fired the conversation/tool
   hooks** (exporter and hooks are separate Claude Code setup steps),
3. all conversation events had **empty content** (skipped at
   `session_capture.go:280`).

The screenshot id (`09c4…e24`) is a valid UUID, so it reached the lookup and
genuinely has no row — consistent with the above.

### Two distinct defects here
- **A. Structural gap (the screenshot case):** telemetry-only sessions have no
  transcript → 404. This is "by design but surprising," and not fixable by a
  small code change alone.
- **B. Latent keying bug:** when `session.id` is **not** a UUID, ClickHouse
  stores the raw string while Postgres stores a UUIDv5 of it (`sessionIDToUUID`
  fallback, `session_capture.go:86`). Those ids can **never** line up;
  `chat.load` with the raw string returns `invalid chat ID`. Affects
  non-UUID/non-Claude agents. This *is* a real correctness bug.
- **C. Blast radius:** the global QueryClient throws every non-403 to the page
  error boundary (`contexts/Sdk.tsx:33`), and `useLoadChatAllGenerations`
  doesn't opt out — so a single 404 crashes the page instead of the sheet's
  existing "Not found" branch rendering.

### Remediation options
1. **Graceful fallback (low risk):** opt `chat.load` out of `throwOnError`, show
   "No transcript captured for this session" in the sheet. Fixes C; contains A.
2. **Fix non-UUID keying:** apply `sessionIDToUUID` consistently so non-UUID
   sessions resolve. Fixes B.
3. **Reconstruct transcript from telemetry:** fall back to building a transcript
   from `telemetry_logs` when no Postgres chat exists. Largest effort; limited by
   how much content OTEL logs carry.
4. **Config/ops:** ensure `session_capture` is enabled and the hooks (not just
   the OTEL exporter) are installed so transcripts persist going forward.

---

## Bug 2 — sheet struggles to render chats with many messages

### Root cause
`chat.load` paginates by **generation only** (`design/chat/design.go:82-111`);
within a generation it returns **all** messages
(`ListChatMessagesByGeneration`). The panel then renders every message as live
DOM (markdown, code blocks, accordions) via `ChatMessagesList` — no windowing —
so large single-generation chats jank. There is **no message-level pagination**
on the endpoint today.

### Agreed approach
**Virtualization + backend message pagination:**
- **Backend:** add `limit`/`offset` (within a generation) to `chat.load` — Goa
  design change (`design/chat/design.go`) + new SQLc query variant + impl change
  + SDK regen. No DB migration needed.
- **Frontend:** virtualize the message list with `@tanstack/react-virtual`
  (already a dependency, pattern in `RiskEvents.tsx:321`) and infinite-scroll
  older messages. Note the accordion-by-generation grouping in
  `ChatMessagesList` complicates virtualization — the flat single-generation path
  is straightforward; multi-generation will need care.

---

## Recommended next step
For the fastest correctness + stability win, ship **graceful fallback + non-UUID
keying** for Bug 1 first (small, low-risk), then tackle Bug 2 (the larger
virtualization + pagination change) as its own PR.
