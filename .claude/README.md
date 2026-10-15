# Claude Code development helpers

## Pause idle stacks

`session-hooks/pause-stack.sh` pauses the worktree's stack once a Claude Code
session has nothing left to do. `settings.json` runs it on `UserPromptSubmit`,
`Stop` and `SessionEnd`. It is opt-in. It acts only when the colon-separated
`GRAM_OPENCODE_PLUGINS`, resolved with `mise env --json` in the session's
worktree, contains the exact ID `speakeasy.pause-stack`. This is the same switch
that enables the OpenCode pause plugin. An unset variable or a failed lookup
leaves it disabled.

Enable it locally by appending the ID to `GRAM_OPENCODE_PLUGINS` in
`mise.local.toml`, preserving any plugins already enabled:

```sh
if resolved="$(mise env --json)" && plugins="$(printf '%s' "$resolved" | jq -r '.GRAM_OPENCODE_PLUGINS // ""')"; then
  mise set --file mise.local.toml "GRAM_OPENCODE_PLUGINS=${plugins:+$plugins:}speakeasy.pause-stack"
fi
```

Each pause check re-reads the setting, so changes take effect without a restart.

- **Settled turns:** a turn that ends with background subagents, shells or
  monitors still running does not pause the stack. That work wakes the session
  when it finishes, and the turn that leaves nothing running pauses it.
  Scheduled wakeups (`/loop`, `ScheduleWakeup`) are not waited for.
- **Multiple sessions:** each prompt marks its session busy in
  `<gitdir>/gram-claude-sessions/`. The last session in the worktree to settle
  or close pauses the stack. The hooks are synchronous, so one session's
  events never overlap.
- **Interruptions:** an interrupted turn fires no `Stop`, so that session stays
  busy until its next turn ends or it closes. The idle-pause sweep still
  applies in the meantime.
- **Skipped:** already-paused stacks, live initial boots, and worktrees without
  their own `mise.local.toml`, which were never provisioned. One nested in the
  main checkout would otherwise use the main checkout's stack.

The pause runs through `pitchfork start pause-stack`, like the idle-pause sweep,
so it finishes even if the session closes. Without a supervisor it runs
`mise run pause` in the background. Resume with `mise run wake` or by opening the
dashboard.

## Tests

```sh
mise exec -- node --test .claude/tests/*.test.mjs
```
