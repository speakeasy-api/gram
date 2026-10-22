---
"server": patch
---

Platform MCP plugin assignments now cover install modes. `get_plugin` returns each current assignment's `install_mode`, and `set_plugin_assignments` accepts optional `install_modes` keyed by assignment reference, keeping a reference's current mode when it is omitted. The assignment version also covers modes, so a mode-only edit made elsewhere is caught as a conflict.
