# Hooks environment overrides

Every `speakeasy-hooks` entrypoint — the per-event hook path, `speakeasy-hooks
login`, `speakeasy-hooks pi serve`, and the `speakeasy-hooks drain` that
replays the offline spool — resolves its configuration the same way: the
deployment identity baked into the generated plugin's `speakeasy.json`, with
the `GRAM_HOOKS_*` variables layered on top. A variable exported into the
shell that launches the coding agent therefore reaches every hook process that
agent spawns, with no reinstall and no edit to the generated package.

The environment is not a universal channel. Some providers scrub the hook
environment before spawning the hook command, so on those only the flags baked
into the generated hook config arrive. Claude Code passes the environment
through.

## Variables

| Variable                           | Purpose                                                                                                          |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `GRAM_HOOKS_SERVER_URL`            | Gram API base the events are posted to. Defaults to `https://app.getgram.ai`.                                    |
| `GRAM_HOOKS_SITE_URL`              | Dashboard origin browser sign-in opens, for deployments that serve the dashboard off the API domain (local dev). |
| `GRAM_HOOKS_PROJECT_SLUG`          | Project the events route to.                                                                                     |
| `GRAM_HOOKS_ORG_ID`                | Organization the cached credential is scoped to.                                                                 |
| `GRAM_HOOKS_ORG_KEY`               | Shared org-wide hooks key, used when no per-user key resolves.                                                   |
| `GRAM_HOOKS_API_KEY`               | Explicit hooks-scoped key; wins over the cached one. The generic `GRAM_API_KEY` is an MCP credential and is ignored here. |
| `GRAM_HOOKS_AUTH_FILE`             | Credential cache path. Defaults to `$XDG_CONFIG_HOME/gram/hooks-auth.env`.                                       |
| `GRAM_HOOKS_BROWSER_LOGIN`         | `1`/`true` lets a fresh machine mint a per-user key through the dashboard.                                       |
| `GRAM_HOOKS_DISABLE_LOCAL_AUTH`    | `1` suppresses interactive sign-in, for shared gateways and CI images.                                           |
| `GRAM_HOOKS_LOGIN_FORCE`           | `1` re-mints a credential even when one is cached, and bypasses the sign-in cooldown.                            |
| `GRAM_HOOKS_LOGIN_TIMEOUT_SECONDS` | How long a browser sign-in waits for its callback. Defaults to 240.                                              |
| `GRAM_HOOKS_FAIL_OPEN`             | `1`/`true` allows the action when no verdict is obtainable (server unreachable, or 5xx).                         |
| `GRAM_HOOKS_NONBLOCKING`           | Legacy fail-open posture baked into older plugins. `GRAM_HOOKS_OBSERVABILITY_MODE` is its older name.            |
| `GRAM_HOOKS_DEBUG_LOG`             | Path the binary appends diagnostics to. Unset means no diagnostics.                                              |
| `GRAM_HOOKS_HOME`                  | Directory the plugin's bootstrap script caches the downloaded binary in. Read by the bootstrap, not the binary.  |

## Collecting a debug log

`GRAM_HOOKS_DEBUG_LOG` is the support channel for an intermittent hook
failure: it turns diagnostics on without touching the installed package.

1. Export it in the shell the coding agent is launched from:

   ```sh
   export GRAM_HOOKS_DEBUG_LOG="$HOME/speakeasy-hooks-debug.log"
   ```

2. Restart the coding agent from that shell. Hook processes inherit the
   agent's environment, so an already-running agent keeps the old one.

3. Reproduce the failure, then collect the file.

4. Unset the variable, restart the agent again, and delete the file. Nothing
   rotates or truncates it.

Each relayed event appends one line, as does each drain run:

```
event=PreToolUse type=tool.requested server=https://app.getgram.ai authfile=/home/dev/.config/gram/hooks-auth.env status=200 denied=false
gate event=PreToolUse elapsed_ms=84 block=false
drain: replayed=3 dropped=0 expired=0 skipped=0 remaining=0 aborted=false
```

The lines are a delivery trace — event name and canonical type, target server,
credential cache path, HTTP status, decision, and the reason behind a skip or
a fail-open. Prompts, tool inputs, and tool outputs never appear.

Writing the log is best effort and never changes what the hook does: the file
is opened append-only at mode `0600`, and a path that cannot be opened or
written is dropped silently rather than failing the hook.

## Providers that scrub the environment

Where the environment does not survive, the same diagnostics are reachable
through the `--debug-log=<path>` flag on the hook command in the plugin's
generated config (`hooks.json`), which must be added to every hook entry and
removed afterwards.

The flag is authoritative: when a command carries `--debug-log=` and the
environment also names `GRAM_HOOKS_DEBUG_LOG`, the flag's path is used. The
other overrides resolve the other way around — the environment is layered over
the command flags — because the debug log's flag exists precisely as the
channel of last resort, and a variable exported for one deployment must not
redirect it.

## Related docs

- [Plugins overview](../plugins/overview.md) — how observability packages are generated and published
- [OpenClaw install runbook](./openclaw-install.md) — the credential variables in a shared-gateway install
