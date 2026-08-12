# Stream the whole assistant turn from the write path

Status: designed, not built. Supersedes the text-only hybrid currently on
`chore/project-assistant-perf`.

## Why

The dashboard renders nothing until a whole assistant message is persisted,
then animates text it already holds, because there is no stream — the
transport polls `chat.load` on a 150ms→1.5s backoff. Perceived latency is
therefore the full generation, not time-to-first-token.

The hybrid already committed tees text deltas off the completions proxy and
publishes them over Redis. It works, but it only carries _text_: tool calls,
tool outputs, message ids and the turn-terminal signal still come from the
poll, so both mechanisms run at once. The completions proxy cannot do better —
it sees one model call, while a turn may be several completions with tool
calls between them, and "the turn is done" is a property of the persisted
rows, not of any single completion.

## Design

Publish from the **write path** instead of the proxy. `chatWriter` is already
the single place turn rows are persisted (`assistantsCore.SetChatMessageWriter`
in `server/cmd/gram/start.go`), which makes it the natural emit point.

Every persisted row emits a frame on the chat's channel:

| Frame         | Emitted when                              | Carries                       |
| ------------- | ----------------------------------------- | ----------------------------- |
| `text-delta`  | model produces text (proxy tee, as today) | text                          |
| `message`     | assistant row persisted                   | id, tool_calls, finish_reason |
| `tool-output` | tool row persisted                        | tool_call_id, output          |
| `done`        | turn reaches terminal state               | —                             |

The client opens one SSE connection per turn and stops polling.

### Sequence numbers and replay

Dropping the poll removes the safety net, so a dropped connection must not lose
a turn. Every frame carries a monotonic `seq` scoped to the chat. The client
tracks the last `seq` it applied and reconnects with `?after=<seq>`; the server
replays from there.

Redis pub/sub alone cannot replay — it is fire-and-forget. Use a capped Redis
stream (`XADD`/`XRANGE`, `MAXLEN ~ 1000`) keyed per chat, which gives ordering,
replay from an offset, and bounded memory. Frames are ephemeral: the persisted
rows remain the durable record, and a client with no usable offset falls back
to a single `chat.load` to resync.

## Steps

1. Redis stream broker: `Publish(chatID, frame) -> seq`, `Replay(chatID, after)`,
   `Subscribe(chatID, after)`. Replaces the pub/sub broker in
   `server/internal/chat/deltastream.go`; keep `teeCompletionStream`, which is
   tested and unchanged.
2. Emit `message` / `tool-output` / `done` frames from `chatWriter` as rows are
   persisted. This is the substantive change — everything else is plumbing.
3. `GET /chat/deltas` accepts `after=<seq>` and replays before tailing. Auth is
   per-handler via `directAuthorize` (these raw routes get no auth middleware —
   this cost a debugging cycle).
4. Client: subscribe once per turn, apply frames in `seq` order, reconnect with
   the last applied `seq`. Delete `pollForReplies` and the fake-stream emulation
   (`writeStreamedText`, `STREAM_*`). Keep one `chat.load` for initial history
   and as the resync path.
5. Tests: replay-after-offset, out-of-order/duplicate frames, reconnect
   mid-turn, and a turn with tool calls arriving across several frames.

## Gotchas paid for already

- `/chat/deltas` must be in `chatSessionsAllowedRoutes`
  (`server/internal/middleware/chat_session_cors.go`) or browsers get 401.
- The client must use `getServerURL()`; dashboard and API are different
  origins, so a relative URL silently hits the dashboard.
- The chat package's `TestMain` boots containers, so even pure unit tests need
  working local infra.
