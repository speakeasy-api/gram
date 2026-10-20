---
"hooks": patch
---

Find a Linux `speakeasyd` installed in the device agent's state folder, `~/.local/state/speakeasy/bin` (or under `XDG_STATE_HOME` when it is set), which is where the dashboard's Linux setup now puts it. Managed identity attribution then works without the agent on `PATH`.
