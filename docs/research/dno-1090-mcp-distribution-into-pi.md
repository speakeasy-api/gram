# MCP distribution into Pi: assessment

Date: 2026-09-15. Investigation for DNO-1090. Spike code: `docs/research/dno-1090-pi-spike` on this branch, throwaway.

Pi is the coding agent published by earendil-works as `@earendil-works/pi-coding-agent`; 0.85.1 is current and is the version everything below was run against.

## Verdict

Pi is the cheapest host Gram has looked at, not the most expensive, and the premise in the ticket is only half right. Pi really does ship no MCP client — with Gram's generated `mcp.json` sitting in both `~/.pi/agent/mcp.json` and `./.mcp.json`, stock Pi offers the model nothing but `read`, `bash`, `edit`, `write`. But Pi's extension API is strong enough that Gram can _be_ the MCP client in-process, and the same module also carries the observability and policy halves. No adapter API has to be built by anyone else, and nothing has to change in Gram's generator to get a working MVP.

Recommendation: build a **Gram-owned Pi extension** that embeds an MCP client, registers Gram tools under the `mcp__<server>__<tool>` convention `server/internal/toolref` already parses, and ships through Gram's existing GitHub publish channel via `pi install git:`. A 244-line dependency-free extension does all of this today.

Do not build on the community adapters (`pi-mcp-adapter`, `pi-mcp-extension`). They work — `pi-mcp-adapter` reads Gram's generated `mcp.json` unmodified, credential and all — but they either hide every Gram tool behind one proxy tool or rename them into a shape `toolref` does not recognize, which silently drops Gram tool calls out of MCP attribution, shadow-MCP matching, and MCP-only enforcement. Details in [Why not the community adapters](#why-not-the-community-adapters).

## Why Pi is structurally different from every other Gram host

Every host Gram distributes MCP to today needs two artifacts, because the host owns the MCP client and Gram can only hand it configuration. (OpenClaw is the exception that proves the shape: hooks only, no MCP half.)

| Host        | MCP half                                                | Observability half                                                                |
| ----------- | ------------------------------------------------------- | --------------------------------------------------------------------------------- |
| Claude Code | `.mcp.json` read by Claude's own MCP client             | separate plugin: `hooks/hooks.json` + `bootstrap.sh` + pinned `agenthooks` binary |
| Cursor      | `mcp.json`                                              | separate plugin, Cursor's hook event names                                        |
| Codex       | `.mcp.json` (`http_headers`)                            | separate plugin, plus `bootstrap.ps1` for Windows                                 |
| OpenCode    | `<slug>/mcp.json` + a TS loader calling the config hook | separate TS shim proxying to `agenthooks serve` over NDJSON                       |
| Copilot     | portable `agent-plugins/<slug>/mcp.json`                | separate plugin, camelCase hook events                                            |
| Pi          | **nothing consumes a config**                           | extension events                                                                  |

On Pi the two halves collapse into one artifact. `pi.registerTool()` supplies the MCP half and `pi.on("tool_call" | "tool_result" | …)` supplies the observability half, in the same TypeScript module, in-process. That removes, for this host: a hooks JSON dialect, the bash and PowerShell bootstrappers, the pinned-binary download, the NDJSON frame protocol, the per-hook timeout policy, and the second marketplace entry. Enforcement stops being an exit-code protocol and becomes a return value.

The cost of that leverage is that Gram owns an MCP client implementation it currently does not have to own on any host.

## What the spike proves

`docs/research/dno-1090-pi-spike/run.sh` drives real Pi 0.85.1 headless — a scripted OpenAI-compatible model registered through `pi.registerProvider()` stands in for an LLM, so no provider credential is involved — against a stand-in MCP server that reproduces Gram's wire contract (streamable HTTP on one URL, bearer auth in an `Authorization` header). The configs driving it are genuine output of `generateMCPFiles` in `server/internal/plugins`, with the MCP URL pointed at the stand-in. Every case asserts on the MCP server's own record of `tools/call`, not just on Pi's output.

| Case | What it establishes                                                                                                   |
| ---- | --------------------------------------------------------------------------------------------------------------------- |
| A    | Gram's Claude-dialect `mcp.json` drives the bridge unchanged; both tools register and one round-trips to the server   |
| B    | Gram's OpenCode-dialect file (`mcp` key, `type: remote`) drives the same bridge unchanged                             |
| C    | `${env:GRAM_API_KEY}` resolves from the environment, as in Gram's published packages                                  |
| D    | With that variable unset the header is dropped and the connection fails; no unauthenticated fallback                  |
| E    | A config with the credential stripped fails the same way, and zero calls reach the server                             |
| F    | A Gram policy verdict blocks a bridged tool call before it leaves the machine                                         |
| G    | The same verdict blocks Pi's built-in `bash`                                                                          |
| H    | The MCP-SDK-based bridge variant behaves identically to the fetch-only one                                            |
| I    | Delivered as an installed Pi package it auto-loads, from an unrelated working directory, resolving its own `mcp.json` |
| J    | Tools registered as `mcp__<server>__<tool>` are accepted by Pi and round-trip, with the prefix stripped upstream      |

## Findings

### 1. Gram needs no generator change for an MVP

The bridge reads Gram's Claude dialect and Gram's OpenCode dialect without modification (cases A and B). Across all five host dialects the payload is the same three fields — URL, header bag, server name — and only the key spellings differ (`mcpServers` vs `mcp`, `headers` vs `http_headers`). A Pi extension that accepts those spellings can be pointed at an artifact Gram already publishes.

So the MVP is one new artifact — the extension — and no change to `generate.go`, no new platform enum, no generator-version bump, no republish. A Pi-native dialect and a `plugin.json`-style manifest are worth adding when Pi becomes a first-class surface, but they are not on the critical path.

### 2. The credential model transfers intact

Gram's per-org packages inline `Authorization: Bearer <key>`; published keyless packages defer to `${env:GRAM_API_KEY}`. Both work (A, C). Both failure modes fail closed: a missing environment variable drops the header rather than sending an empty credential, and the request is rejected at the server (D), which is the rule Gram's OpenCode loader already applies. A config with no credential at all behaves identically (E). In no failure case did a tool reach the model's tool list, so there is no silent degradation into an unauthenticated session.

The exception is OAuth-authenticated Gram servers. Gram emits those with no headers, expecting the host's MCP client to run the authorization code flow. The spike has no such flow, so those servers land in the connect-failure path. See [Gaps](#gaps-and-risks).

### 3. Enforcement is better on Pi than on the hook hosts

`pi.on("tool_call")` can return `{ block: true, reason }`, and the handler runs before the tool executes. A Gram policy verdict blocked a bridged Gram tool (F) and Pi's built-in `bash` (G), with the MCP server confirming zero calls in both cases. That is the same enforcement point Gram gets from `PreToolUse`-style hooks elsewhere, but as an in-process function return rather than a subprocess exit code with a timeout policy — no bootstrap, no fail-open/fail-closed decision forced by a spawn failure.

For the requesting customer's ask specifically — granular RBAC and auditing for agent accounts — this is the part that matters, and it is the part Pi makes easier rather than harder.

### 4. Tool-name attribution works with no Gram-side code

`server/internal/toolref` is the single source of truth for attributing namespaced tool names, recognising Claude's `mcp__<server>__<function>` and Cursor's `MCP:<function>`. It backs shadow-MCP matching and telemetry attribution in the hooks package and attribution in `risk_analysis`. Because a Gram-owned bridge chooses the name it registers, it can emit a convention `toolref` already parses. Pi accepts such names and round-trips them, stripping the prefix before calling upstream (J).

That means MCP attribution, shadow-MCP matching, and MCP-only enforcement paths work on Pi without touching `toolref` or adding a Pi branch — provided Gram owns the bridge. It is precisely what the community adapters take away.

### 5. Distribution rides the channel Gram already has

Pi installs packages from `npm:`, `git:` (including `github.com/user/repo@ref` shorthand, pinned tags or commits), raw HTTPS/SSH git URLs, and local paths. Gram already publishes plugins to a GitHub repository, so `pi install git:github.com/<org>/<repo>@<tag>` consumes Gram's existing publish target; a root `package.json` with a `pi.extensions` manifest is all that repo needs to also be a Pi package. Two other vectors come free:

- `.pi/settings.json` committed to a customer repo lists packages, and Pi installs missing ones on startup once the project is trusted — a team-wide rollout without touching developer machines individually.
- `pi install /path/to/package` works on a local directory, so Gram's existing ZIP download flow works with an unzip step.

Case I verifies the package shape end to end: installed, auto-loaded with no `-e` flag, resolving its own `mcp.json` relative to the module from an unrelated working directory.

### 6. The package can be dependency-free

Two bridge variants were built and behave identically (A vs H): 244 lines speaking MCP streamable HTTP over `fetch`, and 158 lines on `@modelcontextprotocol/sdk`. Pi runs `npm install` for packages it installs, so the SDK dependency is _workable_ — but the fetch-only variant keeps the generated package self-contained, with no install-time network fetch and no third-party runtime code, which is the posture Gram's other generated packages have and the one an ISV with locked-down machines is likelier to accept. The SDK variant buys transport negotiation and protocol-version handling that the hand-rolled client would otherwise have to grow.

Bridge startup — connect plus `tools/list` — measured 19–20 ms against a loopback server. It runs inside `session_start`, so in production it becomes one WAN round trip per configured server on the critical path of Pi opening a session.

## Why not the community adapters

`pi-mcp-adapter` (2.34.0) and `pi-mcp-extension` (1.5.0) exist, and the former genuinely works with Gram's output: dropped at `~/.pi/agent/mcp.json`, Gram's generated config connected to the stand-in server with Gram's credential and executed a tool call. The problem is the tool names Pi ends up offering the model. From one identical Gram config:

| Option                                   | Tools the model sees                                                                  |
| ---------------------------------------- | ------------------------------------------------------------------------------------- |
| Stock Pi                                 | `read, bash, edit, write`                                                             |
| Gram-owned bridge                        | `read, bash, edit, write, crm_list_projects, crm_create_task`                         |
| `pi-mcp-adapter`, config untouched       | `read, bash, edit, write, mcpScript, mcp, mcp__crm`                                   |
| `pi-mcp-adapter` + its `directTools` key | `read, bash, edit, write, mcpScript, crm_crm_list_projects, crm_crm_create_task, mcp` |

Reproduce with `probe-tool-names.sh`.

In its default mode the adapter exposes one proxy tool and the model discovers Gram tools through it, so every Gram tool call appears to Gram as a call to a tool named `mcp` — per-tool attribution, per-tool policy, and per-tool spend are all gone. Promoting tools to first class requires `directTools`, an adapter-specific key Gram would have to generate for a third party's schema, and even then the names arrive double-prefixed (`crm_crm_create_task`). Neither shape matches `mcp__<server>__<function>` or `MCP:<function>`, so `toolref.IsMCPToolName` returns false and Gram treats them as native tools.

On top of that, either adapter puts third-party community code in Gram's credential path, on a release cadence and config schema Gram does not control. The adapter is a reasonable thing to tell a customer to install this afternoon. It is not a foundation for a Gram distribution channel.

## Effort and what it touches

**MVP (a POC-grade Pi target).** One new artifact: the extension package, plus a repo-root `pi` manifest on the published plugin repo and an install line in the docs. Touches nothing in `server/`. The spike is most of it; what it lacks is `tools/list` pagination, `notifications/tools/list_changed` refresh, OAuth, retry and reconnect, and the telemetry transport.

**Productized (Pi as a first-class Gram surface).** The onboarding footprint is the same one OpenCode and Copilot paid, minus the hooks-packaging work Pi does not need:

- `server/internal/plugins/generate.go`: a Pi generator, observability slug, `hooksSubtreePrefixes` entry, and a `hooksGeneratorVersion` bump (CI diffs `PublishedHooksFiles()` against it).
- `server/design/plugins/design.go`: `pi` in the `downloadObservabilityPlugin` platform enum, plus `GenerateObservabilityPluginPackage` and the filename in `impl.go`.
- `server/internal/hooks`: a `parsePiHookEvent` and a `telemetryHookEventName` arm. No `toolref` change (finding 4).
- Dashboard: `ACTIVE_AGENT_PROVIDER_IDS`, install instructions content, icon and label.
- ClickHouse session predicates and the adoption marts, if Pi sessions should count in usage UI — Copilot still lacks this, so there is precedent for deferring.
- `spend_gate`, device-agent targets, `aitargets` catalog: product calls, not requirements. OpenCode is excluded from spend today.

The one dependency worth calling out early: on every other host, telemetry reaches Gram through the pinned `agenthooks` binary, which is an external Go module (`github.com/speakeasy-api/agenthooks`) that would need a Pi provider. Pi may not need it at all — `/rpc/hooks.ingest` is documented as the stable backend contract, authenticated with `Gram-Key` and `Gram-Project` on the `hooks` scope, taking a `hook.ingest.v1` feature-first payload. A TypeScript extension can POST that directly, with no binary download and no external module bump. That trades away whatever the binary provides (buffering, retry, local cache, the quarantine and spend round trips) for a much shorter path. It is the main open design question below.

## Gaps and risks

- **OAuth servers.** Unverified and unimplemented. Gram emits OAuth servers with no headers on the assumption the host's client runs the flow; a Gram-owned bridge would have to implement discovery, dynamic client registration, and PKCE, or use the MCP SDK's auth support. If the customer's servers are Gram-proxied with a Gram key this never comes up; if they are OAuth it is the largest single piece of remaining work.
- **Only part of Pi's event surface was exercised.** `session_start`, `session_shutdown`, `tool_call` and `tool_result` were run. Pi documents a much wider surface — `input` for prompt submission, `before_provider_request` and `after_provider_response` for model traffic and usage, `turn_start`/`turn_end`, compaction and session-switch events — which on paper maps well onto Gram's ingest payload and in places exceeds what other hosts' hooks expose. None of that was verified.
- **Trust and blast radius.** Pi extensions run in-process with the user's full permissions and are explicitly documented as arbitrary code; project-local extensions load only after the project is trusted. Comparable to the hooks Gram already ships, but Pi's own docs are blunter about it, and a Gram extension would sit in the model's tool-dispatch path rather than beside it.
- **Credential at rest.** Unchanged from other hosts: a per-org package carries the Gram key in a plaintext config file.
- **Platform churn.** Pi is at 0.85.x on a fast release cadence; the extension API is documented but young. The bridge depends on `pi.registerTool` accepting a JSON Schema straight from `tools/list` (it does today, because Pi's typebox parameters are JSON Schema at runtime) and on tool registration during `session_start`. Both are documented behaviours, but this is a thinner contract than a `hooks.json` schema.
- **Stand-in, not a live tenant.** The MCP server in the spike reproduces Gram's contract; it is not a Gram deployment. Nothing here exercises Gram's gateway, real toolsets, RBAC, or a real Gram MCP URL. The configs are real generator output, so the client side is faithful, but an end-to-end run against a live Gram project is still owed.

## Open questions

1. **Are the customer's target servers Gram-proxied (Gram API key) or OAuth?** This decides whether the OAuth flow is MVP work or deferrable.
2. **Telemetry transport: POST `/rpc/hooks.ingest` from the extension, or spawn the pinned `agenthooks` binary as the OpenCode shim does?** Direct ingest is far simpler and unblocks Pi from the external module; the binary keeps Pi consistent with every other host and inherits buffering, retry, and the enforcement round trips.
3. **Does Pi need a generated per-org package, or is one published extension plus an existing `mcp.json` enough for the POC?** Finding 1 says the latter works; the former is what makes Pi look like the other hosts in the dashboard.
4. **Is a dependency-free client worth maintaining over the MCP SDK?** Self-contained packages and no install-time network, versus protocol negotiation for free.
5. **Do Pi sessions need to land in ClickHouse session and usage aggregation on day one,** or is the Copilot precedent (ingest works, usage UI lags) acceptable?
