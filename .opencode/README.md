# OpenCode development helpers

OpenCode V2 automatically loads `plugins/pause-stack.js`, but it is opt-in.
At setup, the plugin reads `mise set GRAM_OPENCODE_PLUGINS` in its location's
directory. It activates only when that comma-separated value contains the exact
ID `speakeasy.pause-stack` (surrounding whitespace is ignored). An unset variable
or failed lookup leaves it disabled.

Enable it locally by adding the ID to `GRAM_OPENCODE_PLUGINS` in
`mise.local.toml`. If no other plugins are enabled:

```sh
mise set --file mise.local.toml GRAM_OPENCODE_PLUGINS=speakeasy.pause-stack
```

After changing the variable, reload the plugin with
`touch .opencode/plugins/pause-stack.js`, or restart OpenCode's server.

When an enabled top-level session becomes idle (turn completion, interruption,
or failure), it runs
`mise run pause` in that session's directory. Child-agent completions and events
from other locations are ignored. Overlapping pause requests are coalesced.

The existing pause task stops this worktree's services and containers while
preserving their data. Resume with `mise run wake` or by opening the dashboard.

To disable automatic pausing locally, add this to `.opencode/opencode.jsonc`:

```json
{
  "plugins": ["-speakeasy.pause-stack"]
}
```
