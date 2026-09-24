# AGE-2708: session quarantine primitives across agent harnesses

Research for the `quarantine` risk policy action (AGE-2707 writes session quarantine state; this doc covers how to enforce it per harness). Question: do popular agent harnesses expose session-level quarantine or blocking primitives that an external control plane can drive, and if not, what can we build on their extension points?

Surveyed: Claude Code, OpenAI Codex CLI, Cursor, Gemini CLI, GitHub Copilot CLI. Primary sources only; each claim cites the doc or source file that owns it.

## Summary

No harness has a first-class "quarantine this session" primitive that an external control plane can invoke. Every harness that supports enforcement does so through per-event interception, so session quarantine is built the same way everywhere: a session-keyed deny circuit on the control plane, consulted by the per-event intercept point, that denies every prompt and tool call for the quarantined session. Gram already has exactly this shape in production for spend rules (the spend gate circuit), and hook adapters for Claude, Codex, and Cursor that translate a backend deny into each provider's native response. Quarantine slots in next to the spend gate with a session-scoped key.

Harness enforcement coverage:

- Claude Code: enforceable. PreToolUse + UserPromptSubmit hook denies, first-class HTTP hooks, managed settings can make hooks non-removable. Gram adapter shipped.
- Codex CLI: enforceable. PreToolUse/PermissionRequest hooks, execpolicy rules, requirements.toml managed layer; app-server mode adds real mid-flight interrupt. Gram adapter shipped.
- Cursor: enforceable. beforeShellExecution / beforeMCPExecution / beforeReadFile / preToolUse / beforeSubmitPrompt hooks; the only harness with opt-in fail-closed hooks; enterprise/team hook distribution. Gram adapter shipped.
- Gemini CLI: enforceable in principle. BeforeTool hooks with the same deny contract plus an admin policy tier; no Gram adapter today.
- Copilot CLI: enforceable in principle. preToolUse hooks including native HTTP hooks and a tamper-resistant policy.d layer; no Gram adapter today; GitHub-side admin controls are enable/disable only.

## Per-harness findings

### Claude Code

No first-class quarantine primitive, but the strongest building blocks of any harness.

Per-call intercept: the `PreToolUse` hook fires before every tool call; stdin JSON carries `session_id`, `tool_name`, `tool_input`, `permission_mode`. A deny is either exit code 2 (stderr becomes the blocking message; overrides even a JSON allow) or exit 0 with `{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": "..."}}`; valid decisions are `deny`, `allow`, `ask` (https://code.claude.com/docs/en/hooks). Permission `deny` rules in settings block in every mode including `bypassPermissions`, and hooks run before all permission rules (https://code.claude.com/docs/en/agent-sdk/permissions, https://code.claude.com/docs/en/permission-modes). The Agent SDK adds `canUseTool` returning `{behavior: "deny", message}`, but it only fires when nothing earlier auto-approved; the docs direct per-call checks to PreToolUse hooks (https://code.claude.com/docs/en/agent-sdk/permissions).

Remote decision: first-class HTTP hooks exist: `{"type": "http", "url": "...", "timeout": 30}` POSTs the hook input JSON to a remote endpoint and the response body uses the same JSON output contract, so the control plane can return the deny directly without a local shim; `allowedHttpHookUrls` allowlists hook URLs from managed policy (https://code.claude.com/docs/en/hooks, https://code.claude.com/docs/en/settings). Timeouts are per-hook (documented defaults: 600s for command/HTTP hooks, 30s for UserPromptSubmit). On timeout or a non-zero non-2 exit the hook fails OPEN: the tool call continues through normal permission flow, and the docs warn not to rely on a stalled hook as a gate. The one documented fail-closed exception is Agent SDK callback hooks (https://code.claude.com/docs/en/hooks).

Session level: composable, not first-class. A PreToolUse hook with matcher `"*"` (or omitted) denies every tool call; `UserPromptSubmit` deny (`"decision": "block"` or exit 2) blocks the prompt and erases it, preventing new turns; `"continue": false` in any hook output "stops Claude Code from processing entirely" with a `stopReason` and takes precedence over event-specific decisions, the closest thing to a kill switch (https://code.claude.com/docs/en/hooks). The Stop hook is a continuation-forcer, not a killer. Agent SDK sessions also have `interrupt()` (streaming-input mode) and mid-session `setPermissionMode("dontAsk")`, which auto-denies everything not pre-approved (https://code.claude.com/docs/en/agent-sdk/typescript, https://code.claude.com/docs/en/agent-sdk/permissions). SessionStart/SessionEnd hooks cannot block.

Bypass and management: `--dangerously-skip-permissions` does not disable hooks or deny rules; managed settings (`/etc/claude-code/managed-settings.json`, macOS `/Library/Application Support/ClaudeCode/managed-settings.json`, Windows HKLM policy) are highest precedence, can define hooks, and `allowManagedHooksOnly` restricts execution to managed-settings hooks so users cannot remove them; `disableBypassPermissionsMode: "disable"` kills bypass mode (https://code.claude.com/docs/en/settings, https://code.claude.com/docs/en/permission-modes). Without those managed locks a user can freely edit user/project settings and delete the hook.

Remaining hole even with hooks: `canUseTool` only fires when nothing earlier auto-approved a call, so per-call remote checks belong in PreToolUse hooks, which run before every other permission step and apply even in bypass mode (https://code.claude.com/docs/en/agent-sdk/permissions). Headless CLI can also delegate prompts to an MCP tool via `--permission-prompt-tool` (https://code.claude.com/docs/en/cli-reference).

Session identity: hook stdin includes `session_id` on every event; OTel export attaches `session.id` to metrics by default (https://code.claude.com/docs/en/monitoring-usage).

One field to verify at build time: whether Stop-hook deny uses top-level `decision`/`reason` or the `hookSpecificOutput` wrapper (https://code.claude.com/docs/en/hooks#stop).

### OpenAI Codex CLI

No first-class quarantine primitive, but three per-call deny mechanisms and, in server mode, real mid-flight control.

Per-call intercept: three layers. (1) Lifecycle hooks (newer than the fire-and-forget `notify` option): `PreToolUse`, `PermissionRequest`, `UserPromptSubmit`, `SessionStart`/`SessionEnd`, `Stop`, subagent and compaction events, configured as `[[hooks.PreToolUse]]` command handlers in config; stdin JSON includes `session_id`, `turn_id`, `cwd`, `hook_event_name`. A PreToolUse deny is stdout `{"hookSpecificOutput": {"permissionDecision": "deny", "permissionDecisionReason": "..."}}` (PermissionRequest uses `{"decision": {"behavior": "deny", "message": "..."}}`), or exit code 2 with stderr (https://developers.openai.com/codex/hooks; app-server hook types in codex-rs/app-server-protocol/src/protocol/v2/hook.rs). (2) execpolicy rules: Starlark `.rules` files (`~/.codex/rules/`, project, admin layers) with `prefix_rule` decisions `allow | prompt | forbidden`, most-restrictive-wins (https://github.com/openai/codex/blob/main/docs/execpolicy.md). (3) `approval_policy` (`untrusted | on-request | never | granular`) and `sandbox_mode` (`read-only | workspace-write | danger-full-access`) in config.toml (https://developers.openai.com/codex/config-reference; enums in codex-rs/protocol/src/protocol.rs).

Remote decision: hooks are arbitrary local commands and can consult an HTTP API per call. In `codex app-server` mode (bidirectional JSON-RPC over stdio) approvals are fully programmatic: the server sends `item/commandExecution/requestApproval` / `item/fileChange/requestApproval` with `threadId`/`turnId`/`itemId` and the client answers `accept | acceptForSession | decline | cancel` (https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md; codex-rs/protocol/src/approvals.rs). The MCP-server variant delivers approvals as MCP elicitations that default to denied for non-compliant clients (https://github.com/openai/codex/blob/main/codex-rs/docs/codex_mcp_interface.md, https://github.com/openai/codex/issues/18268).

Session level: in app-server mode, `turn/interrupt` aborts an in-flight turn (core `Op::Interrupt`), `turn/steer` redirects it, and `thread/start`/`thread/resume`/`turn/start` accept per-thread approval-policy and sandbox overrides, so a controller can force read-only + deny-all per thread (codex-rs/app-server/README.md, codex-rs/protocol/src/protocol.rs). Note `approval_policy = "never"` means "never ask", not "never run". For plain CLI use, quarantine is hook-composed: deny every `PreToolUse`/`PermissionRequest` for a quarantined `session_id`.

Bypass and management: user config.toml and its hooks are user-editable. The enforced layer is `requirements.toml` (`/etc/codex/requirements.toml`, Windows ProgramData path, cloud-delivered bundles, macOS MDM): admins can pin `allowed_approval_policies`, `allowed_sandbox_modes`, MCP allowlists, restrictive rules (only `prompt`/`forbidden`), managed `[hooks]`, and `allow_managed_hooks_only = true` to ignore user/project hooks (https://developers.openai.com/codex/enterprise/managed-configuration, https://github.com/openai/codex/blob/main/docs/config.md).

Session identity: `session_id` on every hook payload (subagents reuse the parent's), `thread_id`/`turn_id`/`item_id` throughout the app-server protocol (https://developers.openai.com/codex/hooks; codex-rs/app-server-protocol/src/protocol/v2/).

### Cursor (agent + CLI)

No session-kill hook primitive, but the most complete per-call enforcement surface, and the only harness with configurable fail-closed semantics.

Per-call intercept: Agent Hooks (https://cursor.com/docs/hooks) cover `preToolUse`, `beforeShellExecution`, `beforeMCPExecution`, `beforeReadFile`, `beforeSubmitPrompt`, `afterFileEdit`, `sessionStart`/`sessionEnd`, `stop`, subagent events, and more. Hooks are executables (JSON stdin/stdout) configured in `hooks.json` at four levels. Deny contract: `{"permission": "allow" | "deny" | "ask", "user_message": "...", "agent_message": "..."}` (snake_case); exit code 2 also blocks; matchers filter tools/commands.

Remote decision: hooks are arbitrary processes, so calling an HTTP control plane per call is unrestricted. Security-critical hook scripts should use an HTTPS Gram endpoint and must validate its certificate; disabling TLS verification turns fail-closed configuration into an interceptable bypass. Default is fail-open ("By default, hook failures (crash, timeout, invalid JSON) allow the action through"), but `"failClosed": true` per hook blocks on failure, and docs recommend it for security-critical `beforeMCPExecution` hooks; `timeout` is configurable in seconds with no documented default, and a timeout counts as failure so failClosed applies to it (https://cursor.com/docs/hooks). Cursor is the only surveyed harness with documented opt-in fail-closed timeout semantics. Known community-reported bug: malformed JSON hook output silently allowing commands (https://forum.cursor.com/t/beforeshellexecution-hook-malformed-json-response-silently-allows-command-instead-of-blocking/152669; forum report, not docs).

Session level: no kill-session hook. Quarantine is compositional: deny in all tool hooks plus `beforeSubmitPrompt` returning `{"continue": false, "user_message": "..."}` to block further turns; `stop` is not a kill switch (it fires at completion and can only auto-submit a followup) (https://cursor.com/docs/hooks). Cloud Agents do have mid-flight termination: `POST /v1/agents/{id}/runs/{runId}/cancel`, terminal and non-resumable (https://cursor.com/docs/cloud-agent/api/endpoints). The Teams Admin API has no agent-termination endpoint (https://cursor.com/docs/account/teams/admin-api).

Bypass and management: precedence Enterprise > Team > Project > User. Enterprise hooks are system files in admin-writable paths (`/etc/cursor/hooks.json`, macOS `/Library/Application Support/Cursor/hooks.json`, Windows `C:\ProgramData\Cursor\hooks.json`) distributable via MDM; Team hooks are pushed from the dashboard and auto-synced every 30 minutes (https://cursor.com/docs/hooks). Docs do not state an explicit tamper-proofing guarantee; enforcement rests on OS file permissions/MDM. The CLI supports the same hooks including team-managed distribution (https://cursor.com/docs/cli/changelog) plus static allow/deny permission arrays (https://cursor.com/docs/cli/reference/permissions). Gap: cloud agents do not run `beforeMCPExecution` or prompt-based hooks (https://cursor.com/docs/hooks).

Session identity: every hook receives `conversation_id` and `generation_id` plus `user_email`, `transcript_path` (https://cursor.com/docs/hooks). `conversation_id` is the natural quarantine key, and it is what Gram's Cursor adapter already maps into `hookevents.Event.ConversationID`.

### Gemini CLI

No first-class quarantine primitive, but hooks and a policy engine cover both per-call and session-wide denial.

Per-call intercept: a hooks system with 11 events including `BeforeTool`, `BeforeToolSelection`, `AfterTool`, `SessionStart`, `SessionEnd`; hooks are shell commands in settings.json with per-tool regex matchers; stdin JSON carries `session_id`, `transcript_path`, `cwd`, `hook_event_name` plus `tool_name`/`tool_input` for `BeforeTool` (https://github.com/google-gemini/gemini-cli/blob/main/docs/hooks/index.md, docs/hooks/reference.md). A `BeforeTool` deny is stdout `{"decision": "deny", "reason": "..."}` or exit code 2 with stderr as the reason; the reason is fed to the model as a tool error (docs/hooks/reference.md, docs/hooks/writing-hooks.md). Separately, a static policy engine (`packages/core/src/policy/`, decisions `allow`/`deny`/`ask_user`, priority-ordered TOML rules matching toolName wildcard, args regex, command prefix, MCP server, mode) provides declarative denies with a `denyMessage` (https://github.com/google-gemini/gemini-cli/blob/main/docs/reference/policy-engine.md, packages/core/src/policy/types.ts). `--allowed-tools` is deprecated in favor of the policy engine.

Remote decision: hooks are arbitrary shell commands run synchronously per event, so a `BeforeTool` hook can call an external API per call; docs place no network restriction on hooks (docs/hooks/index.md). The policy engine itself is static TOML with no documented remote fetch or reload.

Session level: two composable mechanisms. A wildcard `BeforeTool` matcher denies every execution; `BeforeToolSelection` can emit `toolConfig.mode: "NONE"`, disabling ALL tools for the model call (docs/hooks/writing-hooks.md). In ACP mode (JSON-RPC over stdio) the embedding client can `cancel` an in-flight prompt and `setSessionMode` mid-session (docs/cli/acp-mode.md). No external session-kill API exists in plain interactive/headless mode; the A2A server is experimental with no documented cancellation contract.

Bypass and management: four settings layers with system overrides highest (`/etc/gemini-cli/settings.json` on Linux, `GEMINI_CLI_SYSTEM_SETTINGS_PATH` override), plus an admin policy tier (`/etc/gemini-cli/policies`, tier base 5) that "always override[s] User, Workspace, and Default policies"; `security.disableYoloMode` forces confirmations (https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/enterprise.md, docs/reference/policy-engine.md, docs/admin/enterprise-controls.md). Root-owned system files are the enforcement boundary; user-level hooks are removable by the user.

Session identity: `session_id` is in every hook's stdin payload (docs/hooks/reference.md).

### GitHub Copilot CLI

No built-in freeze/kill API, but as of mid-2026 a first-class hooks system with native HTTP hooks makes per-call remote denial and hook-composed quarantine feasible.

Per-call intercept: hooks cover `sessionStart`, `sessionEnd`, `userPromptSubmitted`, `preToolUse`, `postToolUse`, `permissionRequest` (CLI only), `agentStop`, subagent events, `errorOccurred`. A `preToolUse` deny is stdout JSON `{"permissionDecision": "deny", "permissionDecisionReason": "..."}` or exit code 2 (https://docs.github.com/en/copilot/reference/hooks-reference). Static controls also exist: `--allow-tool`/`--deny-tool` patterns where "deny rules always take precedence over allow rules, even when `--allow-all` is set" (https://docs.github.com/en/copilot/how-tos/copilot-cli/use-copilot-cli/allowing-tools), and saved approvals in `~/.copilot/permissions-config.json`.

Remote decision: native HTTP hooks: `{"type": "http", "url": "https://...", "headers": {...}, "timeoutSec": 30}` POST the hook payload per event and read the decision from the response; HTTPS required. Fail-mode is mixed: command `preToolUse` hooks are fail-closed on crash (crash = deny) but timeouts are always fail-open, and HTTP hooks are fail-open (https://docs.github.com/en/copilot/reference/hooks-reference). The Copilot SDK (github/copilot-sdk) exposes a typed `PreToolUseHandler` over JSON-RPC for managed/headless execution (https://docs.github.com/en/copilot/how-tos/copilot-sdk/hooks/pre-tool-use).

Session level: no block-all or kill primitive; compositional via a `preToolUse`/`permissionRequest` hook keyed on `sessionId` (permissionRequest output supports `"behavior": "deny"` plus an `interrupt` boolean); `agentStop` hooks can block continuation turns (https://docs.github.com/en/copilot/reference/hooks-reference).

Bypass and management: user and repo hooks are trivially bypassable (`"disableAllHooks": true`, user-owned files, flags override saved approvals). The tamper-resistant slot is policy hooks: `/etc/github-copilot/policy.d/*.json` (Windows `C:\ProgramData\GitHub\Copilot\policy.d\*.json`) load first and "Policy hooks always run regardless" of `disableAllHooks` (https://docs.github.com/en/copilot/reference/hooks-reference). GitHub-side enterprise controls are coarse: the Copilot CLI policy is enable/disable only, plus MCP allowlist policies; admins cannot set tool permissions from github.com (https://docs.github.com/en/copilot/how-tos/copilot-cli/administer-copilot-cli-for-your-enterprise).

Session identity: every hook payload includes `sessionId`, `timestamp`, and `cwd` on all events; sessions are resumable via `--continue` / `/resume` (https://docs.github.com/en/copilot/reference/hooks-reference). Known caveats: open bugs report `preToolUse` not enforced in subagents (https://github.com/github/copilot-cli/issues/2392) and plugin-defined hooks not firing (github/copilot-cli#2540). Config dir override is `COPILOT_HOME`, not `COPILOT_CONFIG_DIR` (https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/configure-copilot-cli). BYOK bypasses enterprise policies (https://docs.github.com/en/copilot/how-tos/copilot-cli/administer-copilot-cli-for-your-enterprise).

## How Gram intercepts hook traffic today

All file paths below are in the gram repo.

### Transport and deny contract

Installed hooks call Gram synchronously over HTTPS on two generations of endpoints (`server/internal/hooks/README.md`):

- Legacy per-provider endpoints: `/rpc/hooks.claude`, `/rpc/hooks.cursor`, `/rpc/hooks.codex`, plus Claude OTEL ingestion.
- Unified ingest: `/rpc/hooks.ingest`, authenticated with `Gram-Key` + `Gram-Project` (`hooks` key scope). Payload is feature-first (`prompt.submitted`, `tool.requested`, ...), response is provider-neutral: `{"decision": "allow"}` or `{"decision": "deny", "reason": "...", "message": "..."}`. Generated hook glue translates that into the provider-native shape.

Provider adapters render denies natively: Claude `permissionDecision` (`server/internal/hooks/claude_hooks.go:896` area), Codex `Decision: "deny"` (`server/internal/hooks/codex_hooks.go:55`), Cursor `Permission: "deny"` (`server/internal/hooks/cursor_hooks.go:61`).

### Event model carries session identity

`server/internal/hookevents/event.go` defines the canonical event. Every event carries `ConversationID` (the provider session id) plus `EventContext{OrganizationID, ProjectID, User}`. Event types include `user_prompt_submit`, `before_tool_use`, `before_mcp_execution`, `permission_request`, `session_start`, `stop`, `session_end`. A quarantine circuit can therefore be keyed on `(organization_id, project_id, conversation_id)` (optionally plus user) with no schema work on the ingest side.

### The spend gate circuit is the existing template

`server/internal/spendrules/gate.go` implements the pattern quarantine needs:

- Redis-only hot path: `Gate.CheckBlocked(org, user)` reads `spend_gate:rules:{org}` and `spend_gate:actor:{org}:{user}` cache entries; a background evaluator (30s cycle) and API writes keep them fresh; entries carry a 30 minute TTL.
- Fail-open by design: nil gate, unresolved identity, and cache errors all resolve to "not blocked" (`server/internal/hooks/spend_gate.go`).
- Ordering: the spend gate runs before any risk-policy scan, in `evaluateCanonicalHook` (`server/internal/hooks/ingest_hooks.go:556` area) for the unified path and at per-provider call sites on the legacy paths (`claude_hooks.go:853`, `codex_hooks.go:108,221,245`, `cursor_hooks.go:129,243,292`). It gates `prompt.submitted` and `tool.requested` for the claude/codex/cursor adapters (`spendGatedAdapter`, `server/internal/hooks/spend_gate.go:46`); opencode passes through pending a product decision.
- The schema comment documents the circuit semantics: rules with `action = 'block'` "open a circuit that denies the actor's Claude hook traffic until the window resets" (`server/database/schema.sql:6161`).

### Risk policy enforcement path

`server/internal/hooks/risk_scan.go` wires hook events into `risk.Scanner.ScanForEnforcement` (`server/internal/risk/scanner.go:254`), which loads enabled enforcing policies per project and fans out detectors synchronously inside the hook request. Deny/warn results are rendered per transport (block reason, warn challenge with out-of-band ack link). Expensive scanners (Presidio, prompt-injection judge) run in `pystreams` behind the AIS-402 request-reply pub/sub enforcement topics with an org-level deadline and failure-mode setting; cheap detectors run in-process. A quarantine check is not a scan at all: on the healthy-cache path it is a single Redis circuit read next to `checkSpendGate`, before `ScanForEnforcement`. If Redis fails, Gram also reads the durable organization `hooks_fail_open` setting from Postgres before deciding whether the request may proceed, so the error path is deliberately not Redis-only.

### Where quarantine slots in

1. AGE-2707's `quarantine` action writes session state (the policy hit that opened the quarantine, the session key, expiry/lift conditions).
2. That write also populates a Redis circuit entry, e.g. `session:quarantine:{org}:{project}:{conversation_id}`, mirroring `spend_gate:actor:*` (TTL + evaluator/API refresh so a lift propagates within seconds). Organization and project scope prevent equal harness session ids in different tenants from sharing a circuit.
3. `evaluateCanonicalHook` (and the three legacy paths) gains a `checkQuarantineGate` beside `checkSpendGate`, keyed by `ev.ConversationID`, denying `prompt.submitted` and `tool.requested` (which covers `before_tool_use`, `before_mcp_execution`, and `permission_request` shapes). Unlike the spend gate's actor key, the quarantine key is session-scoped; an actor-scoped variant (quarantine every session of a user) is the same mechanism with the spend gate's existing key shape.
4. Deny rendering may reuse the existing provider response shapes, but quarantine lifecycle auditing is dedicated: `session_quarantine:open` and `session_quarantine:release` records identify the durable quarantine row and session. It does not masquerade as or reuse a generic policy-block audit row.

Fail-mode: reuse the audited organization `hooks_fail_open` feature rather than introduce a second, orphaned setter. Cache errors query that durable setting: enabled organizations fail open; organizations without it fail closed for quarantine containment. If the settings lookup itself fails, availability wins and the request fails open.

## Recommendation

Build quarantine as a control-plane circuit, not a harness feature. No harness offers a "quarantine session" API, but every enforceable harness already routes per-event decisions through Gram's hooks, and a session-keyed deny circuit turns per-event denial into session quarantine uniformly.

Enforcement design:

1. AGE-2707's `quarantine` action writes session quarantine state and mirrors it into a Redis circuit keyed `(org, project, conversation_id)`, modeled on `spendrules.Gate` (evaluator/API refresh + TTL so lifts propagate without waiting for expiry).
2. Add `checkQuarantineGate` beside `checkSpendGate` in `evaluateCanonicalHook` and the three legacy provider paths, denying `prompt.submitted` and `tool.requested` (covers before_tool_use, before_mcp_execution, permission_request). Existing adapters render the deny natively for Claude (`permissionDecision: "deny"`), Codex (`decision: "deny"`), and Cursor (`permission: "deny"`); prompt denies freeze new turns, tool denies freeze the current one. Record dedicated `session_quarantine:open` and `session_quarantine:release` audit events against the quarantine row.
3. Deny messages should state the quarantine authoritatively to the model (mirroring `renderWarnAgentReason`) and give the human a block view link explaining how the quarantine lifts.

Per-harness enforcement:

- Claude Code: PreToolUse + UserPromptSubmit denies via the installed Gram hook. Recommend orgs set managed settings with `allowManagedHooksOnly` for tamper resistance. Harness timeouts fail open, so Gram must answer fast; the added circuit read is one Redis GET on the existing synchronous path (target: no measurable change to hook p99, which is already dominated by risk scans).
- Codex CLI: same hook path (PreToolUse/PermissionRequest). Tamper resistance via `requirements.toml` with managed `[hooks]` and `allow_managed_hooks_only`. If Gram ever embeds Codex via app-server, `turn/interrupt` plus per-thread read-only sandbox is a true mid-flight kill; not needed for hook-based quarantine.
- Cursor: hook denies across shell/MCP/read/preToolUse plus `beforeSubmitPrompt` `continue: false`. Call Gram over HTTPS with certificate validation enabled, and set `failClosed: true` on the gated hooks so a Gram outage or invalid response cannot be exploited to escape quarantine (unique among harnesses). Gap: cloud agents skip `beforeMCPExecution` and prompt hooks; the Cloud Agents cancel API is the fallback there if we ever manage those.
- Gemini CLI: buildable, needs a Gram hook adapter (new work; ingest is adapter-agnostic so this is glue plus enforcement enablement in `spendGatedAdapter`-style gating). `BeforeTool` deny JSON matches Gram's contract shape almost exactly; `BeforeToolSelection` `toolConfig.mode: "NONE"` can strip tools from the model call as an extra layer. Admin policy tier gives tamper resistance. Until an adapter ships: alert-only.
- Copilot CLI: buildable, needs an adapter; native HTTP hooks could even point directly at a Gram endpoint without local glue, with `policy.d` as the non-removable slot. HTTP hooks are fail-open and subagent enforcement has a known bug, so treat Copilot as best-effort enforcement plus alerting until those close. Until then: alert-only.

Fail-mode: use the existing audited `hooks_fail_open` organization feature. The healthy-cache path remains one Redis read. On a cache error, Gram consults Postgres: organizations with `hooks_fail_open` enabled proceed, while all others fail closed for quarantine containment; if the settings lookup also fails, Gram fails open to avoid a global outage. Pair containment mode with Cursor `failClosed` and deny-by-default permission postures where managed configuration allows.

Structural limits to state in the ticket:

- Quarantine takes effect at the next hook event; no surface can abort a tool call already executing.
- Enforcement is cooperative below the managed layers: a user who can edit user-level settings can strip hooks unless the org deploys managed settings (Claude), requirements.toml (Codex), enterprise/team hooks (Cursor), the admin policy tier (Gemini), or policy.d (Copilot). Hook removal is visible to Gram as the session going silent, which is itself an alertable signal.
- Harnesses with no installed Gram hook (any harness, not just Gemini/Copilot) are observe-and-alert only.
