---
"server": minor
---

Plugin assignments now carry an install mode for the device agent: `required` (installed, can't be turned off), `default` (installed, can be turned off) or `available` (off until the user turns it on). `setPluginAssignments` accepts `install_modes` per principal URN and keeps a principal's current mode when it is omitted. `agent.getPlugins` returns each plugin's `install_mode`, resolving overlapping assignments to the strictest mode, plus its display `name` and `description`. The observability plugin is always `required`.
