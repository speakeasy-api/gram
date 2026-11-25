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
  suggested destination and a unique `opencode/<sanitized-name>-<suffix>` branch. Starts
  from the source checkout's HEAD unless a starting ref is supplied. Normal
  Worktrunk hooks initialize the worktree and provision its stack in the
  background, then leave it paused; run `mise run wake` to resume it.
- **Remove:** `wt remove --foreground --format=json --yes`.
  Runs the existing MCP cleanup and `nuke --keep-shared` pre-remove hooks before
  deleting files. Dirty worktrees require OpenCode's force confirmation; only
  then is `--force` passed. Branch deletion follows Worktrunk's configuration
  (by default, integrated branches are deleted and unmerged branches retained).
- **List:** `wt list --format=json` with schema 2, filtered to the repository
  root and worktrees whose private Git identity marker matches this plugin's
  durable storage. Reusing a removed worktree's path does not transfer ownership.

The `--yes` flag runs configured Worktrunk hooks non-interactively. Existing
worktrees keep their original OpenCode strategy; enabling this plugin does not
transfer ownership. Keep the plugin enabled while it owns worktrees so OpenCode
can remove them through it. Worktrunk subprocesses receive OpenCode's cancellation
signal; once creation succeeds, ownership bookkeeping finishes even if canceled.

## Pause idle stacks

OpenCode V2 automatically loads `plugins/pause-stack.js`, but it is opt-in.
At setup, both plugins read `GRAM_OPENCODE_PLUGINS` from `mise env --json` in
their location's directory, resolving config overrides and templates. The pause
plugin activates only when that colon-separated value contains the exact
ID `speakeasy.pause-stack` (surrounding whitespace is ignored). An unset variable
or failed lookup leaves it disabled.

Enable it locally by appending the ID to `GRAM_OPENCODE_PLUGINS` in
`mise.local.toml`, preserving any plugins already enabled:

```sh
if resolved="$(mise env --json)" && plugins="$(printf '%s' "$resolved" | jq -r '.GRAM_OPENCODE_PLUGINS // ""')"; then
  mise set --file mise.local.toml "GRAM_OPENCODE_PLUGINS=${plugins:+$plugins:}speakeasy.pause-stack"
fi
```

The append runs only after a successful read and JSON parse. An unset variable
is treated as an empty list, so the same command works for first-time setup.

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
