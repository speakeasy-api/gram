# Can we support Pi for observability? (DNO-1081)

Investigation notes for DNO-1081. The question came out of a customer MCP-gateway
evaluation and an internal interest in daily-driving Pi as a harness.

**Short answer: yes for observability, and Pi is one of the easier harnesses we
would have added — its extension API is richer than Cursor's, Kimi's, Copilot's or
OpenClaw's. But Pi ships no MCP client at all, so the MCP-server half of a Gram
plugin has nothing to attach to.** Those are two separate answers and should not be
given as one.

Everything below was verified against `@earendil-works/pi-coding-agent@0.85.1`
(`pi.dev`, formerly `badlogic/pi-mono`, now `earendil-works/pi`) by running the real
CLI against a mock OpenAI-completions endpoint with a prototype Speakeasy extension
installed. Claims marked **[measured]** come from that run rather than from docs.

## 1. What Pi is

A deliberately minimal terminal coding harness. Four built-in tools (`read`, `write`,
`edit`, `bash`, plus `grep`/`find`/`ls`), four run modes (interactive, print/JSON, RPC,
SDK). Everything else — sub-agents, plan mode, permission prompts, MCP — is explicitly
out of core and pushed into TypeScript extensions or installable packages.

The reason it came up is the ecosystem argument: other harnesses embed Pi via its SDK,
OpenClaw among them. That argument is real but narrower than it sounds — see §6.

## 2. Observability: the integration point exists and is good

Pi extensions register typed handlers with `pi.on(event, handler)`. Handlers may be
async, and Pi awaits them. Several can mutate or cancel; `input` and `tool_call` are the
two that gate the main agent path.

Mapping Pi's events onto our unified `agenthooks` event kinds:

| `agenthooks` kind                  | Pi event                                                   | Decision surface                                                          |
| ---------------------------------- | ---------------------------------------------------------- | ------------------------------------------------------------------------- |
| `session.start`                    | `session_start` (`reason`: startup/reload/new/resume/fork) | observe                                                                   |
| `session.end`                      | `session_shutdown`                                         | observe                                                                   |
| `prompt.submitted`                 | `input`                                                    | `{action: "continue" \| "transform" \| "handled"}` — deny **and** rewrite |
| `tool.pre`                         | `tool_call`                                                | `{block, reason, terminate}`, plus `event.input` is mutable in place      |
| `tool.post`                        | `tool_result`                                              | `{content, details, isError, usage}` — full output replacement            |
| `tool.post` (observe)              | `tool_execution_start` / `_update` / `_end`                | observe, streaming-aware                                                  |
| `tool.error`                       | `tool_result` with `isError`, `tool_execution_end`         | observe                                                                   |
| `agent.stop`                       | `agent_end`, `agent_settled`, `turn_end`                   | observe                                                                   |
| `model.request`                    | `before_provider_request`, `before_provider_headers`       | observe                                                                   |
| `model.response`                   | `after_provider_response`, `message_end`                   | observe                                                                   |
| `compact.pre` / `compact.post`     | `session_before_compact` (`{cancel}`) / `session_compact`  | cancel                                                                    |
| `session.start` context injection  | `before_agent_start`                                       | `{message, systemPrompt}`                                                 |
| `permission.request`               | —                                                          | none (see §5)                                                             |
| `subagent.start` / `subagent.stop` | —                                                          | none, no sub-agents in core                                               |
| `mcp.inventory`                    | —                                                          | none, no MCP client                                                       |
| `notification`                     | —                                                          | none (`ctx.ui.notify` is outbound only)                                   |
| `file.edited`                      | —                                                          | derive from `edit`/`write` tool results                                   |

The sequence observed on one tool-using turn, in order **[measured]**:
`session_start`, `input`, `before_agent_start`, `agent_start`, `turn_start`,
`message_end`, `before_provider_request`, `after_provider_response`,
`tool_execution_start`, `tool_call`, `tool_result`, `tool_execution_end`, `turn_end`,
`agent_end`, `agent_settled`, `session_shutdown`.

Two things stand out against the providers we already support:

- **`tool_result` can replace output.** Today that is Claude Code, Gemini, OpenCode and
  Cursor (MCP only); Codex, OpenClaw, Kimi and Copilot cannot. Pi joins the first group.
- **`input` can rewrite the prompt, not just deny it or prepend to it.** No provider in
  our current matrix has `CapUpdateInput` on `prompt.submitted` — Claude Code, Codex and
  Gemini can only add context, Cursor is deny-only, and Copilot drops prompt-hook output
  entirely. Pi's `{action: "transform", text}` is a capability we do not have anywhere else.

### Token usage and cost arrive normalized **[measured]**

`message_end` carries a fully normalized usage object per assistant message, including a
computed cost breakdown:

```json
{
  "input": 1200,
  "output": 42,
  "cacheRead": 0,
  "cacheWrite": 0,
  "reasoning": 0,
  "totalTokens": 1242,
  "cost": {
    "input": 0,
    "output": 0,
    "cacheRead": 0,
    "cacheWrite": 0,
    "total": 0
  },
  "provider": "mock",
  "model": "mock-model",
  "stopReason": "stop"
}
```

This is what our per-turn usage rows want, and it is richer than what we splice together
for OpenClaw (where `agent_end` carries no usage and the shim has to cache `llm_output`).

### Gating is genuinely synchronous **[measured]**

The prototype denied a `bash` call for `cat /etc/shadow`. Pi did not run the command and
handed the model `isError: true` with our reason string. With the daemon reply delayed by
3s, the whole run took 3.67s — Pi really does await the promise, so a fail-closed deadline
is meaningful rather than decorative. Denying at `input` stopped the turn before any
provider request: the only hooks that fired were `session_start`, `input`,
`session_shutdown`.

## 3. Packaging: the OpenClaw pattern ports over directly

The prototype is the OpenClaw shim with the hook names changed. It spawns the consumer
binary once and proxies typed hooks over NDJSON stdio as `{seq, hook, event, ctx}` frames,
awaiting the reply only for gating hooks under a shim-owned deadline. `agenthooks serve`
already implements exactly that loop, so `servePi` is the OpenClaw branch with a different
codec.

Three packaging details, all **[measured]**:

- **Plain JavaScript is accepted.** Discovery matches `.ts` _or_ `.js`
  (`dist/core/extensions/loader.js`), so the generator can emit compiled JS and no
  TypeScript toolchain is needed on the customer machine. Unlike OpenClaw, which rejects
  TypeScript entry modules outright, Pi takes either — so the JS-only generator we already
  have satisfies both.
- **Install globally, not project-local.** `~/.pi/agent/extensions/<name>/index.js` loads in
  an untrusted project directory with no trust prompt and no `--approve`. Project-local
  `.pi/extensions` requires project trust and would produce a prompt per repo. A
  `settings.json` `extensions: ["/abs/path"]` entry is the other supported vector.
- **The shim must kill its child on `session_shutdown`.** Without it Pi hung for the full
  90s timeout instead of exiting in 0.7s, because the spawned consumer kept the event loop
  alive. OpenClaw's shim does the equivalent on `gateway_stop`; this is easy to miss because
  it only shows up in print/JSON mode where the process is supposed to exit.

## 4. What would have to change

Nothing here is architecturally novel — it is the OpenClaw provider walked again.

**`speakeasy-api/agenthooks` (separate repo):** `ProviderPi` constant and detection;
`codec_pi.go`; a `capMatrix` entry; `install/render_pi.go`; a `servePi` branch in
`serve.go`; quirk entries for the shutdown and ephemeral-session behaviours.

Proposed capability entry, given §2 and §5:

```go
ProviderPi: {
    KindToolPre:         caps(CapDeny, CapUpdateInput, CapStopAgent), // terminate:true
    KindPromptSubmitted: caps(CapDeny, CapUpdateInput, CapAddContext), // action:"transform"
    KindToolPost:        caps(CapReplaceOutput),
    KindSessionStart:    caps(CapAddContext, CapSystemMessage), // before_agent_start
},
```

`CapAsk` is deliberately absent: see §5.

**Gram:** the touchpoints are enumerable, and `git log -S openclaw` walks all of them.

- `server/design/plugins/design.go` — add `pi` to the provider `Enum`, regenerate Goa.
- `server/internal/plugins/generate.go` / `impl.go` — package generator, shim template,
  README section, download filename.
- `server/internal/hooks/spend_gate.go` — add `pi` to the gated-adapter list.
- `server/internal/hooks/ingest_hooks.go` — session-id stability list (but see §5).
- `server/internal/agent/aitargets/aitargets.go` — Shadow AI target: binary `pi`, config dir
  `~/.pi`, process `pi`. Worth doing independently of everything else, since it is how we
  find out whether anyone is running Pi at all.
- `client/dashboard` — `AGENT_PROVIDERS` entry, icon, `ACTIVE_AGENT_PROVIDER_IDS.plugins`,
  plus `harnessEnvelopes.ts`, `formatPlatform.ts` and `chatLogs/transcript.ts`.
- `hooks/relay/provision.go` and `hooks/cmd/speakeasy-hooks/main.go` — provider case and flag help.
- **ClickHouse migrations.** `hook_source IN (...)` predicates are hardcoded in the summary
  materialized views (`server/clickhouse/schema.sql` around the turn-usage and tool-call
  predicates) and in `mart.sql`. Adding a source means the standard MV migration dance, and
  `server/internal/telemetry/repo/sessions.go` keeps a mirrored copy that
  `sessions_schema_sync_test.go` asserts against. This is the most invasive piece and the
  one most likely to be underestimated.

No Postgres schema change is needed, and `chat/sources.go` needs no alias — like `opencode`
and `openclaw`, the generated package always passes one spelling.

## 5. Gaps and risks

1. **No MCP client, at all.** Pi's README is blunt: _"No MCP. Build CLI tools with READMEs,
   or build an extension that adds MCP support."_ For an MCP-gateway evaluation this is the
   headline. Gram plugins bundle MCP servers _and_ observability hooks; for Pi only the
   second half has a receiver. Shipping toolsets to Pi would mean Gram also authoring and
   maintaining an MCP-client extension for Pi, which is a different and larger project than
   the observability work. Worth confirming which half the customer actually wants before
   quoting anything.
2. **`pi -ne` disables every extension. [measured]** One documented flag and all
   observability is gone, with no server-side trace beyond the session going quiet. Other
   harnesses have escape hatches too, but Pi's is a single advertised flag, and the product
   actively encourages users to edit their own extensions. Anyone buying this for compliance
   rather than for insight needs to hear that plainly.
3. **No permission event.** Pi has no permission system to hook; `ctx.ui.confirm` exists but
   only in interactive mode, so print/JSON/RPC runs have no UI to prompt. `CapAsk` must
   degrade to deny, the same posture we took for Kimi.
4. **Ephemeral sessions have no session id. [measured]** With `--no-session`,
   `ctx.sessionManager.getSessionFile()` returns null. Otherwise the session file name
   carries a UUIDv7. The codec needs to synthesize a per-process id for the ephemeral case.
5. **No sub-agent events**, because core has no sub-agents. Extensions can add them, and
   those would be invisible to us.
6. **Pre-1.0 and moving fast.** 0.85.1, with an explicitly extensible event surface and no
   stability guarantee. We would need a pinned version plus conformance tests, and should
   expect breakage at a higher rate than Claude Code or Codex.
7. **`@earendil-works/pi-telemetry` exists** — the monorepo ships "vendor-neutral telemetry
   contracts, reference adapter, conformance tests, and typed schemas". Not evaluated here.
   If it is a stable contract it may be a better integration point than raw extension hooks,
   and it should be looked at before anyone writes `codec_pi.go`.

## 6. On the "many harnesses are built on Pi" argument

Real but narrower than it sounds. Harnesses embed `@earendil-works/pi-agent-core` or
`pi-ai`, not the `pi` CLI, and the extension API verified here belongs to
`pi-coding-agent` — the CLI layer. An embedder gets the agent runtime and typically builds
its own event surface on top, which is exactly why OpenClaw needed its own plugin system
and its own codec on our side despite being a Pi consumer. Supporting the Pi CLI does not
transitively support its embedders. What _might_ generalize is a `pi-agent-core` adapter,
but that is a separate investigation and a much weaker claim.

## 7. Recommendation

Split the answer:

- **Observability on the Pi CLI:** supportable, well-understood, no architectural blockers.
  Confined to a new `agenthooks` provider plus the enumerated Gram touchpoints, of which
  the ClickHouse MV predicates are the most invasive.
- **Pi as an MCP gateway target:** not supportable as things stand, because Pi has no MCP
  client. This needs a product decision, not an engineering estimate.

Cheap first step regardless of that decision: add Pi to the Shadow AI target catalog. It is
a handful of lines, ships independently, and tells us whether anyone is actually running Pi
before we build a provider for it.

Reproduction harness for the measured claims is not committed; it is a mock
OpenAI-completions server plus the prototype extension described in §3, and is quick to
rebuild from this document.
