# OpenClaw install runbook

How a customer installs the Speakeasy observability plugin into OpenClaw, what
coverage they get, and how we qualify a new OpenClaw version.

Read [Plugins overview](../plugins/overview.md) first for how observability
packages are generated and published.

## Before you start

OpenClaw coverage is **not** uniform — it depends on how the customer's models
authenticate. Establish this first, because it changes what you promise them.

| Model auth mode                                       | What OpenClaw runs                                        | Hooks that fire                                                                                                   | Speakeasy coverage                                                                                                                                                              |
| ----------------------------------------------------- | --------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Embedded runtime (`agentRuntime: { id: "openclaw" }`) | Model + tool loop in-process                              | Full set: `before_agent_run`, `before_tool_call`, `after_tool_call`, `llm_output`, `agent_end`, session lifecycle | Full — capture **and** real-time tool blocking                                                                                                                                  |
| Claude CLI harness (`agentRuntime: claude-cli`)       | Delegates the loop to the Claude Code CLI, out-of-process | Only `agent_turn_prepare`, `before_prompt_build`, `before_message_write`                                          | Partial from OpenClaw. Tool and LLM activity is captured **only if Claude Code hooks are separately deployed on the same machine**; without that, these sessions are unobserved |

The trap: `openclaw models auth login` writes `agentRuntime: {id: "claude-cli"}`
**by default for every model** when a claude-cli profile exists on the machine.
A customer who authenticated interactively will land in the second row without
choosing to. Auth-profile ordering alone does not flip it — the per-model
`agentRuntime` does.

The two paths are complementary, but only when both are deployed. A machine
that followed this runbook and nothing else has **no** coverage for
claude-cli-routed models: OpenClaw's hooks do not fire for them, and no Claude
Code hooks are installed to pick them up. Either install the Claude Code
observability plugin alongside this one, or tell the customer those sessions
are not captured. Say which, or the OpenClaw dashboard will look empty to a
customer whose models all route through the Claude CLI.

## Install (developer laptop)

1. In the Gram dashboard, download the OpenClaw observability plugin ZIP
   (**Plugins → Observability → OpenClaw**). This mints a hooks API key and
   bakes it into the package's `speakeasy.json`. Requires `OrgAdmin`.

2. Install it. `openclaw plugins install` accepts an archive path directly, so
   the download works as-is:

   ```sh
   openclaw plugins install ./<org>-observability-openclaw.zip
   ```

   Or extract first and point at the directory:

   ```sh
   unzip <org>-observability-openclaw.zip
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
   conversation-scope hooks the plugin subscribes (`before_agent_run`,
   `llm_output`, `agent_end`) silently never fire — no error, no warning, just
   no prompts, no assistant responses and no usage. Tool hooks still fire, so a
   half-configured install looks _partly_ working, which is the most confusing
   failure mode we have here.

   OpenClaw gates further conversation hooks behind the same flag (`llm_input`
   and `before_agent_finalize` among them), but the plugin does not subscribe
   those and never reports them.

4. Restart the Gateway. Plugin changes are not hot-loaded.

5. Verify — see below.

Uninstall with `openclaw plugins uninstall speakeasy-observability`.

## Install (server / CI, no device agent)

The plugin does not require the Speakeasy device agent. For a shared gateway or
a CI image, install the same package but drive its credentials from the
environment instead of the key baked into the download.

**At image build time:**

1. Download the OpenClaw observability ZIP from the dashboard, as in step 1
   above. It is the same artifact; the baked key is simply overridden at run
   time by the variables below.

2. Install it into the image:

   ```sh
   openclaw plugins install ./<org>-observability-openclaw.zip
   ```

3. Preinstall the `speakeasy-hooks` binary. The plugin's bootstrap otherwise
   downloads it from `https://github.com/speakeasy-api/gram/releases` on the
   first hook firing, which needs egress from the running container and adds
   latency to the first turn. Set `GRAM_HOOKS_HOME` to a directory baked into
   the image and populate it at build time; the bootstrap uses that instead of
   its per-OS cache location. If the download is left in place, allowlist that
   host or the first hook fails with a bootstrap error.

**At run time**, supply:

| Variable                  | Purpose                                 |
| ------------------------- | --------------------------------------- |
| `GRAM_HOOKS_SERVER_URL`   | Gram server base URL                    |
| `GRAM_HOOKS_ORG_KEY`      | Org hooks key (mint one per deployment) |
| `GRAM_HOOKS_PROJECT_SLUG` | Target project; defaults to `default`   |
| `GRAM_HOOKS_ORG_ID`       | Org ID                                  |

Supplying these at run time keeps the key out of an image layer. Rotate by
replacing `GRAM_HOOKS_ORG_KEY` and restarting the gateway, with no reinstall.

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
  have tested, so **our shim owns its own deadlines**. There are three, and they
  are not interchangeable:

  | Budget               | Value | What it bounds                                                                                                         |
  | -------------------- | ----- | ---------------------------------------------------------------------------------------------------------------------- |
  | `GATE_TIMEOUT_MS`    | 10s   | `before_tool_call` and `before_agent_run`. Blocking hooks fail closed here, so a stalled gate resolves in 10s, not 60. |
  | `DEFAULT_TIMEOUT_MS` | 30s   | The observe-only hooks                                                                                                 |
  | `--timeout=60s`      | 60s   | The `agenthooks serve` process itself, not any individual handler deadline                                             |

  Without shim-owned deadlines a hung control-plane call would stall the agent
  turn indefinitely.

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

### Qualification procedure (manual)

Run this against a new OpenClaw build before adding it to the table above. It
is deliberately manual; see the note on canaries below.

1. Install the target version into an isolated profile so a real install is not
   disturbed:

   ```sh
   npm i -g openclaw@<version>
   openclaw --dev --version
   ```

2. Install the observability package into that profile and enable
   `allowConversationAccess`, per the install steps above. Use `--dev` for
   every command so the state stays under `~/.openclaw-dev`.

3. Confirm the model is on the embedded runtime, not the claude-cli harness.
   Under claude-cli the tool and LLM hooks never fire and the run proves
   nothing about the payloads.

4. Re-record the hook frames and diff them against
   `agenthookstest/fixtures/openclaw/` in the agenthooks repo. Check
   specifically that:
   - each subscribed hook still fires with the same name
   - `ctx.runId` is identical across `before_agent_run`, `before_tool_call`,
     `llm_output` and `agent_end`, since turn correlation depends on it and
     degrades silently if it breaks
   - `llm_output.event.usage` still carries the token fields
   - `before_tool_call` still honours `{block, blockReason}`

5. Run the capture verification above, plus one blocked tool call, and confirm
   both land in the dashboard.

6. Only then update the version table here and the fixture README. Any change
   to a hook's payload shape, name or return contract is a decoder change in
   `agenthooks/codec_openclaw.go`, not a docs change.

### Why there is no nightly canary

We deliberately do **not** run a scheduled job against OpenClaw `latest`. It
would page on OpenClaw's release cadence and registry flakes without telling us
what we actually support, which is the same reasoning that keeps the LiteLLM
real-proxy suite (`.github/workflows/litellm-e2e.yml`) on `workflow_dispatch`
rather than a schedule. The procedure above is the qualification gate instead.

`openclaw agent --local` does give a headless one-shot turn, so wiring OpenClaw
into the existing `hooks:e2e` harness alongside the other providers is the
natural home for an automated version of this. That is tracked separately
rather than bolted on here.

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
