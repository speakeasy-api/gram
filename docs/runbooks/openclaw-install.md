# OpenClaw install runbook

How a customer installs the Speakeasy observability plugin into OpenClaw, what
coverage they get, and how we qualify a new OpenClaw version.

Read [Plugins overview](../plugins/overview.md) first for how observability
packages are generated and published.

## Before you start

OpenClaw coverage is **not** uniform — it depends on how the customer's models
authenticate. Establish this first, because it changes what you promise them.

| Model auth mode                                       | What OpenClaw runs                                        | Hooks that fire                                                                                                   | Speakeasy coverage                                                                                                                     |
| ----------------------------------------------------- | --------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Embedded runtime (`agentRuntime: { id: "openclaw" }`) | Model + tool loop in-process                              | Full set: `before_agent_run`, `before_tool_call`, `after_tool_call`, `llm_output`, `agent_end`, session lifecycle | Full — capture **and** real-time tool blocking                                                                                         |
| Claude CLI harness (`agentRuntime: claude-cli`)       | Delegates the loop to the Claude Code CLI, out-of-process | Only `agent_turn_prepare`, `before_prompt_build`, `before_message_write`                                          | Partial from OpenClaw — but these sessions are covered by Speakeasy's **Claude Code** hooks, which the CLI honors via managed settings |

The trap: `openclaw models auth login` writes `agentRuntime: {id: "claude-cli"}`
**by default for every model** when a claude-cli profile exists on the machine.
A customer who authenticated interactively will land in the second row without
choosing to. Auth-profile ordering alone does not flip it — the per-model
`agentRuntime` does.

This is complementary coverage, not a hole: between the two paths every session
is observed. But say so explicitly, or the OpenClaw dashboard will look empty
to a customer whose models all route through the Claude CLI.

## Install (developer laptop)

1. In the Gram dashboard, download the OpenClaw observability plugin ZIP
   (**Plugins → Observability → OpenClaw**). This mints a hooks API key and
   bakes it into the package's `speakeasy.json`. Requires `OrgAdmin`.

2. Install it. `openclaw plugins install` accepts a directory or an archive
   path, so either works:

   ```sh
   openclaw plugins install ./<org>-observability-openclaw
   ```

   Add `--force` to replace an existing install — without it the command
   refuses rather than upgrading. This copies the package to
   `<profile>/extensions/speakeasy-observability`.

3. Enable conversation-scope hooks in the OpenClaw config:

   ```json
   {
     "plugins": {
       "entries": {
         "speakeasy-observability": {
           "enabled": true,
           "hooks": { "allowConversationAccess": true }
         }
       }
     }
   }
   ```

   **This step is not optional.** Without `allowConversationAccess`, the
   conversation hooks (`before_agent_run`, `llm_input`, `llm_output`,
   `agent_end`, `before_agent_finalize`) silently never fire — no error, no
   warning, just no prompts, no assistant responses and no usage. Tool hooks
   still fire, so a half-configured install looks _partly_ working, which is
   the most confusing failure mode we have here.

4. Restart the Gateway. Plugin changes are not hot-loaded.

5. Verify — see below.

Uninstall with `openclaw plugins uninstall speakeasy-observability`.

## Install (server / CI, no device agent)

The plugin does not require the Speakeasy device agent. For a shared gateway or
a CI image, skip the dashboard download and drive the hooks runtime entirely
from the environment:

| Variable                  | Purpose                                 |
| ------------------------- | --------------------------------------- |
| `GRAM_HOOKS_SERVER_URL`   | Gram server base URL                    |
| `GRAM_HOOKS_ORG_KEY`      | Org hooks key (mint one per deployment) |
| `GRAM_HOOKS_PROJECT_SLUG` | Target project; defaults to `default`   |
| `GRAM_HOOKS_ORG_ID`       | Org ID                                  |

Bake the plugin directory into the image and install it at build time; supply
the variables at run time so the key is not baked into a layer. Rotate by
replacing `GRAM_HOOKS_ORG_KEY` and restarting the gateway — no reinstall.

Sessions from a shared gateway attribute to the org, not to an individual.
Per-user attribution on shared gateways is tracked separately in DNO-971.

## Verify

1. Run a turn that uses a tool:

   ```sh
   openclaw agent --local "list the files in this directory"
   ```

2. In the dashboard, filter sessions by agent type **OpenClaw**. You should see
   the session with its prompt, the assistant response, the tool call, and
   token/cost totals on the turn.

3. If the session appears but has **no prompt or usage**, step 3 of the install
   was missed — `allowConversationAccess` is off.

4. If **no session appears at all**, check in order:
   - `openclaw plugins doctor` — reports plugin load issues directly, and is
     the fastest way to tell "failed to load" from "loaded but not reporting".
   - Gateway restarted after install?
   - `openclaw plugins list` shows `speakeasy-observability` enabled?
   - Is the model on the Claude CLI harness? (see the coverage table above)
   - Gateway logs for `speakeasy-observability` errors.

## Enforcement

`before_tool_call` is a real blocking surface: a deny returns
`{block: true, blockReason}`, the tool never executes, and the reason text is
delivered to the model as the tool result.

Two behaviors worth knowing:

- **`after_tool_call` still fires for a blocked call**, with the block text as
  the result. The provider marks these as blocked rather than completed; a
  blocked call must not show up as a successful tool call.
- **OpenClaw does not impose the 15s fail-closed policy timeout** we originally
  briefed. A `before_tool_call` handler that stalls is awaited in full. Per-hook
  `timeoutMs` exists on the registration API but is opt-in in the versions we
  have tested. **Our shim therefore owns its own deadline** (`--timeout=60s`);
  without it a hung control-plane call would stall the agent turn indefinitely.

## Version-pin policy

OpenClaw's plugin API is not versioned independently of OpenClaw itself, so the
supported range is whatever we have actually qualified.

| Version    | Status                                                                                                                                      |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026.6.34  | Ground-truthed in the DNO-950 spike; all payload shapes and the hook vocabulary in `agenthookstest/fixtures/openclaw/` come from this build |
| 2026.7.1-2 | In use locally; **not yet re-qualified**                                                                                                    |

Note the drift: the fixtures our decoder is tested against were captured from
2026.6.34, and nothing in the repo records that. Treat the fixture corpus as
version-stamped evidence, not as a permanent contract.

To qualify a new OpenClaw version, follow the LiteLLM precedent
(`server/internal/litellm/fixtures/`): re-record the hook payloads from the new
build, diff them against the checked-in fixtures, run the verification above
for both capture and a blocked tool call, and only then document the version as
supported. A change to any hook's payload shape, name or return contract is a
decoder change in `agenthooks/codec_openclaw.go`, not a docs change.

We deliberately do **not** run a nightly canary against OpenClaw `latest`. It
would page on OpenClaw's release cadence and registry flakes without telling us
what we actually support — the same reasoning the LiteLLM real-proxy suite is
`workflow_dispatch` rather than scheduled.

## Operational gotchas

- The gateway process argv is just `openclaw`. Find it by port
  (`lsof -iTCP:18789`, or `19001` under `--dev`), not by process name.
- Interactive `models auth login --provider anthropic` installs a LaunchAgent
  (`ai.openclaw.open`) running a default-profile gateway on 18789 that
  **respawns when killed**. Relevant when a customer asks what installing
  OpenClaw touches on a laptop.
- `--dev` isolates state under `~/.openclaw-dev` on port 19001; `--profile
<name>` isolates under `~/.openclaw-<name>`. Use one of these when testing so
  you do not disturb a real install.
- **Plugins see gateway secrets.** `gateway_start` hands the plugin the full
  gateway config including `gateway.auth.token`. The provider scrubs
  config-shaped payloads before anything leaves the machine, `raw` passthrough
  included. Preserve that when touching the OpenClaw codec.
