# Hooks environment overrides

Every `speakeasy-hooks` entrypoint that carries a deployment — the per-event
hook path, `speakeasy-hooks login`, and `speakeasy-hooks pi serve` — resolves
its configuration the same way: the deployment identity baked into the
generated plugin's `speakeasy.json`, with the `SPEAKEASY_AI_HOOKS_*` variables layered
on top. A variable exported into the shell that launches the coding agent
therefore reaches every hook process that agent spawns, with no reinstall and
no edit to the generated package. The detached `speakeasy-hooks drain` that
replays the offline spool is the exception: it takes its deployment identity
(server, project, org) from the spooled events, not from `speakeasy.json` or
the environment. It still resolves credentials through `SPEAKEASY_AI_HOOKS_AUTH_FILE`
and `SPEAKEASY_AI_HOOKS_API_KEY` (the env key only for the deployment
`SPEAKEASY_AI_HOOKS_SERVER_URL` names, when it names one), and honours
`SPEAKEASY_AI_HOOKS_DEBUG_LOG`.

The environment is not a universal channel. Some providers scrub the hook
environment before spawning the hook command, so on those only the flags baked
into the generated hook config arrive. Claude Code passes the environment
through.

Each `SPEAKEASY_AI_*` variable below replaces a `GRAM_*` variable of the same
suffix (for example `GRAM_HOOKS_API_KEY`). The `GRAM_*` names are deprecated
but still read when the `SPEAKEASY_AI_*` name is unset or empty. The default
credential cache moved from `$XDG_CONFIG_HOME/gram/hooks-auth.env` to
`$XDG_CONFIG_HOME/speakeasy-ai/hooks-auth.env`; an existing cache at the old
path stays in use until the new file exists.

## Variables

| Variable                                    | Purpose                                                                                                                           |
| ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| `SPEAKEASY_AI_HOOKS_SERVER_URL`             | Gram API base the events are posted to. Defaults to `https://app.getgram.ai`.                                                     |
| `SPEAKEASY_AI_HOOKS_SITE_URL`               | Dashboard origin browser sign-in opens, for deployments that serve the dashboard off the API domain (local dev).                  |
| `SPEAKEASY_AI_HOOKS_PROJECT_SLUG`           | Project the events route to.                                                                                                      |
| `SPEAKEASY_AI_HOOKS_ORG_ID`                 | Organization the cached credential is scoped to.                                                                                  |
| `SPEAKEASY_AI_HOOKS_ORG_KEY`                | Shared org-wide hooks key, used when no per-user key resolves.                                                                    |
| `SPEAKEASY_AI_HOOKS_API_KEY`                | Explicit hooks-scoped key; wins over the cached one. The generic `SPEAKEASY_AI_API_KEY` is an MCP credential and is ignored here. |
| `SPEAKEASY_AI_HOOKS_AUTH_FILE`              | Credential cache path. Defaults to `$XDG_CONFIG_HOME/speakeasy-ai/hooks-auth.env`.                                                |
| `SPEAKEASY_AI_HOOKS_BROWSER_LOGIN`          | `1`/`true` lets a fresh machine mint a per-user key through the dashboard.                                                        |
| `SPEAKEASY_AI_HOOKS_DISABLE_LOCAL_AUTH`     | `1` suppresses interactive sign-in, for shared gateways and CI images.                                                            |
| `SPEAKEASY_AI_HOOKS_LOGIN_FORCE`            | `1` re-mints a credential even when one is cached, and bypasses the sign-in cooldown.                                             |
| `SPEAKEASY_AI_HOOKS_LOGIN_COOLDOWN_SECONDS` | How long after a sign-in attempt before another is tried automatically. Defaults to 6 hours.                                      |
| `SPEAKEASY_AI_HOOKS_LOGIN_TIMEOUT_SECONDS`  | How long a browser sign-in waits for its callback. Defaults to 240.                                                               |
| `SPEAKEASY_AI_HOOKS_FAIL_OPEN`              | `1`/`true` allows the action when no verdict is obtainable (server unreachable, or 5xx).                                          |
| `SPEAKEASY_AI_HOOKS_NONBLOCKING`            | Legacy fail-open posture baked into older plugins. `SPEAKEASY_AI_HOOKS_OBSERVABILITY_MODE` is its older name.                     |
| `SPEAKEASY_AI_HOOKS_DEBUG_LOG`              | Path the binary appends diagnostics to. Unset means no diagnostics.                                                               |
| `SPEAKEASY_AI_HOOKS_HOME`                   | Directory the plugin's bootstrap script caches the downloaded binary in. Read by the bootstrap, not the binary.                   |

## Collecting a debug log

`SPEAKEASY_AI_HOOKS_DEBUG_LOG` is the support channel for an intermittent hook
failure: it turns diagnostics on without touching the installed package.

1. Export it in the shell the coding agent is launched from:

   ```sh
   export SPEAKEASY_AI_HOOKS_DEBUG_LOG="$HOME/speakeasy-hooks-debug.log"
   ```

2. Restart the coding agent from that shell. Hook processes inherit the
   agent's environment, so an already-running agent keeps the old one.

3. Reproduce the failure, then collect the file.

4. Unset the variable, restart the agent again, and delete the file. Nothing
   rotates or truncates it.

Each relayed event appends a delivery line and, for gated events, a gate
timing line, plus a line for any skip, fail-open, or auth retry. Each drain run
appends one summary line:

```
event=PreToolUse type=tool.requested server=https://app.getgram.ai authfile=/home/dev/.config/speakeasy-ai/hooks-auth.env status=200 denied=false
gate event=PreToolUse elapsed_ms=84 block=false
drain: replayed=3 dropped=0 expired=0 skipped=0 remaining=0 aborted=false
```

The lines are a delivery trace — event name and canonical type, target server,
credential cache path, HTTP status, decision, and the reason behind a skip or
a fail-open. Prompts, tool inputs, and tool outputs never appear.

Writing the log is best effort and never changes what the hook does: the file
is opened append-only, and a path that cannot be opened or written is dropped
silently rather than failing the hook. A new file is created at mode `0600`;
an existing file (or a symlink's target) keeps its own permissions, so point
the variable at a fresh path in a directory only the user can read.

## Providers that scrub the environment

Where the environment does not survive, the same diagnostics are reachable
through the `--debug-log=<path>` flag on the hook command in the plugin's
generated config (`hooks.json`), which must be added to every hook entry and
removed afterwards.

The flag is authoritative: when a command carries `--debug-log=` and the
environment also names `SPEAKEASY_AI_HOOKS_DEBUG_LOG`, the flag's path is used. The
other overrides resolve the other way around — the environment is layered over
the command flags — because the debug log's flag exists precisely as the
channel of last resort, and a variable exported for one deployment must not
redirect it.

## Related docs

- [Plugins overview](../plugins/overview.md) — how observability packages are generated and published
- [OpenClaw install runbook](./openclaw-install.md) — the credential variables in a shared-gateway install
