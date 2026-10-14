# OpenCode development helpers

## Worktrunk worktrees

`plugins/worktrunk.js` registers the `speakeasy.worktrunk` strategy in OpenCode
V2. Enable it by appending `speakeasy.worktrunk` to the colon-separated
`GRAM_OPENCODE_PLUGINS` in the canonical checkout's `mise.local.toml`, preserving
existing IDs (see the commands below for the pause plugin). Reload the plugin
with `touch .opencode/plugins/worktrunk.js`, or restart OpenCode's server.

Requires Worktrunk with `switch/remove --format=json` and list JSON schema 2
(tested with `wt` 0.80.0).

- **Create:** `wt switch --create --format=json --no-cd --yes`, with OpenCode's
  suggested destination and a unique `opencode/<name>-<suffix>` branch. Starts
  from the source checkout's HEAD unless a starting ref is supplied. Normal
  Worktrunk hooks initialize and boot the stack.
- **Remove:** `wt remove --foreground --format=json --yes`.
  Runs the existing MCP cleanup and `nuke --keep-shared` pre-remove hooks before
  deleting files. Dirty worktrees require OpenCode's force confirmation; only
  then is `--force` passed. Branch deletion follows Worktrunk's configuration
  (by default, integrated branches are deleted and unmerged branches retained).
- **List:** `wt list --format=json` with schema 2, filtered to the repository
  root and paths recorded in this plugin's durable storage.

The `--yes` flag runs configured Worktrunk hooks non-interactively. Existing
worktrees keep their original OpenCode strategy; enabling this plugin does not
transfer ownership. Keep the plugin enabled while it owns worktrees so OpenCode
can remove them through it. Subprocesses receive OpenCode's cancellation signal.

## Pause idle stacks

OpenCode V2 automatically loads `plugins/pause-stack.js`, but it is opt-in.
At setup, the plugin reads `mise set GRAM_OPENCODE_PLUGINS` in its location's
directory. It activates only when that colon-separated value contains the exact
ID `speakeasy.pause-stack` (surrounding whitespace is ignored). An unset variable
or failed lookup leaves it disabled.

Enable it locally by appending the ID to `GRAM_OPENCODE_PLUGINS` in
`mise.local.toml`, preserving any plugins already enabled:

```sh
if plugins="$(mise set GRAM_OPENCODE_PLUGINS)"; then
  mise set --file mise.local.toml "GRAM_OPENCODE_PLUGINS=${plugins:+$plugins:}speakeasy.pause-stack"
fi
```

The append runs only after a successful read. For first-time setup, if mise
reports `Environment variable GRAM_OPENCODE_PLUGINS not found`, initialize it:

```sh
mise set --file mise.local.toml GRAM_OPENCODE_PLUGINS=speakeasy.pause-stack
```

After changing the variable, reload the plugin with
`touch .opencode/plugins/pause-stack.js`, or restart OpenCode's server.

When an enabled top-level session becomes idle (turn completion, interruption,
or failure), it runs
`mise run pause` in that session's directory. Child-agent completions and events
from other locations are ignored. The event handler waits for the pause command
to finish before processing the next event. It skips already-paused stacks and
live worktree boots, and checks OpenCode's active-session snapshot before pausing
so another top-level session in the same worktree (including a subdirectory)
can finish first. Failed state lookups skip the pause.

The existing pause task stops this worktree's services and containers while
preserving their data. Resume with `mise run wake` or by opening the dashboard.

To disable automatic pausing locally, add this to `.opencode/opencode.jsonc`:

```json
{
  "plugins": ["-speakeasy.pause-stack"]
}
```

## Tests

The Worktrunk lifecycle test uses an isolated repository and real `wt` commands:

```sh
mise exec -- node --test .opencode/tests/*.test.mjs
```
