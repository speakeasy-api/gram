# OpenCode development helpers

OpenCode V2 automatically loads `plugins/pause-stack.js`, but it is opt-in.
At setup, the plugin reads `mise set GRAM_OPENCODE_PLUGINS` in its location's
directory. It activates only when that comma-separated value contains the exact
ID `speakeasy.pause-stack` (surrounding whitespace is ignored). An unset variable
or failed lookup leaves it disabled.

Enable it locally by appending the ID to `GRAM_OPENCODE_PLUGINS` in
`mise.local.toml`, preserving any plugins already enabled:

```sh
plugins="$(mise set GRAM_OPENCODE_PLUGINS 2>/dev/null || true)"
mise set --file mise.local.toml "GRAM_OPENCODE_PLUGINS=${plugins:+$plugins,}speakeasy.pause-stack"
```

After changing the variable, reload the plugin with
`touch .opencode/plugins/pause-stack.js`, or restart OpenCode's server.

When an enabled top-level session becomes idle (turn completion, interruption,
or failure), it runs
`mise run pause` in that session's directory. Child-agent completions and events
from other locations are ignored. The event handler waits for the pause command
to finish before processing the next event.

The existing pause task stops this worktree's services and containers while
preserving their data. Resume with `mise run wake` or by opening the dashboard.

To disable automatic pausing locally, add this to `.opencode/opencode.jsonc`:

```json
{
  "plugins": ["-speakeasy.pause-stack"]
}
```
