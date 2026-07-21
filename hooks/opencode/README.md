# @gram/opencode-observability

Gram observability plugin for [opencode](https://opencode.ai). Maps opencode
plugin events to Gram's canonical hook vocabulary and ships them to
`POST /rpc/hooks.ingest`, the same provider-neutral endpoint used by Claude
Code, Cursor, and Codex.

## Install

Drop the built package (or this directory, unpublished) into an opencode
plugin location and reference it from `opencode.json`:

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "plugin": ["@gram/opencode-observability"],
}
```

Or copy `src/` into `~/.config/opencode/plugins/gram/` for a per-machine
install. opencode runs plugins on Bun, which executes TypeScript directly —
no build step required.

## Configuration

Env vars only. Never hardcode a key in source.

| Var               | Required | Default                  | Purpose                                                 |
| ----------------- | -------- | ------------------------ | ------------------------------------------------------- |
| `GRAM_URL`        | no       | `https://app.getgram.ai` | Gram server base URL                                    |
| `GRAM_KEY`        | yes      | —                        | Hooks-scoped API key (`Gram-Key` header)                |
| `GRAM_PROJECT`    | yes      | —                        | Target project slug (`Gram-Project` header)             |
| `GRAM_USER_EMAIL` | no       | —                        | Best-effort attribution when the key is shared org-wide |

If `GRAM_KEY`/`GRAM_PROJECT` are missing, the plugin logs one warning on the
first event delivery and keeps sending unauthenticated requests (fail-open,
matching the `hooks.ingest` endpoint's own behavior) rather than throwing and
blocking the agent. If `GRAM_URL` is not an `https` endpoint (loopback hosts
excepted for local dev), events are dropped to avoid sending `GRAM_KEY` and
payloads in plaintext.

## Behavior

- **Fail-open delivery**: every send is wrapped in a timeout (5s) and a small
  bounded retry (2 attempts, jittered backoff); all errors are swallowed so a
  dead network never blocks the coding session.
- **Idempotency**: each event gets its own `idempotency_key`, reused across
  its own retries via the `Idempotency-Key` header.
- **MCP identity resolution**: opencode names an MCP tool call
  `<server>_<tool>` (e.g. `context7_query-docs`), but Gram's shadow-MCP scanner
  and MCP attribution recognize the `mcp__<server>__<tool>` convention
  (`toolref.IsMCPToolName`) plus a structured `data.mcp` block
  (`canonicalMCPData`). On the first tool call the plugin reads the configured
  MCP servers from `client.config.get()` (`config.mcp`) and, for each MCP tool
  call, rewrites the name into that form _and_ attaches the server's identity —
  `server_name` plus `url` (remote servers) or `command` (local/stdio servers).
  Native tools (`bash`, `edit`, …) are left untouched with no `mcp` block.
  Sending the URL lets the server resolve gram-hosted vs shadow the same way it
  does for Claude Code / Codex / Cursor, rather than treating every opencode MCP
  call as shadow. Without this, opencode MCP calls are misclassified as native
  (local) tools and skip shadow-MCP detection entirely.
- **Model, token, and cost attribution**: on each completed assistant turn
  (`message.updated`) the plugin forwards the model id (`session.model`) and the
  turn's token/cost usage (`data.usage`: input/output/cache tokens + cost) from
  opencode's assistant message. These feed the model-usage, token-total, cost,
  and token time-series widgets on the server (`gen_ai.response.model` /
  `gen_ai.usage.*`).
- **Device attribution**: every event carries the machine `hostname`
  (`source.hostname`), which drives the "origin" (device) tier of the employee
  data-flow graph.

## Event coverage

| opencode hook / event                                 | canonical `event.type`                    | `source.raw_event_name`            |
| ----------------------------------------------------- | ----------------------------------------- | ---------------------------------- |
| `session.created`                                     | `session.started`                         | `session.created`                  |
| `session.idle`                                        | `session.ended`                           | `session.idle`                     |
| `session.deleted`                                     | `session.ended`                           | `session.deleted`                  |
| `tool.execute.before`                                 | `tool.requested`                          | `tool.execute.before`              |
| `tool.execute.after`                                  | `tool.completed`                          | `tool.execute.after`               |
| `message.part.updated` (tool part, `status: "error"`) | `tool.failed`                             | `tool.execute.error` (synthesized) |
| `chat.message`                                        | `prompt.submitted`                        | `message.submitted` (synthesized)  |
| `message.updated` (assistant, completed)              | `assistant.responded`                     | `message.completed` (synthesized)  |
| `permission.ask`                                      | `tool.requested` (with `permission_type`) | `permission.asked` (synthesized)   |

The synthesized `raw_event_name` values must stay in lockstep with
`parseOpencodeHookEvent` in `server/internal/hooks/events.go` — that switch
is what gives opencode events native event-name fidelity (e.g.
`PostToolUse` instead of a generic fallback) in the Gram dashboard.

opencode's `tool.execute.after` hook has no error field on its output, so
tool failures are detected from `message.part.updated` tool-part state
transitions instead.

## Development

```sh
pnpm -F @gram/opencode-observability type-check
pnpm -F @gram/opencode-observability test
```

`src/mapping.ts` is pure (no network) and carries the real test coverage.
`src/send.ts` handles delivery; `src/config.ts` resolves env config.
