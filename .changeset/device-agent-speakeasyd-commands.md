---
"dashboard": patch
---

Keep the device agent off `PATH` in the Linux setup steps. Both binaries now go into `~/.local/state/speakeasy/bin` without `sudo`, so they no longer overwrite another `speakeasy` binary in `/usr/local/bin` or share the SDK generator's `~/.speakeasy` folder, and the agent can update itself there. The verify and personal enrollment steps call `speakeasy status` and `speakeasy enroll` by that path, which fixes the invalid `speakeasyd status` step. With a resolved version, the Linux download commands use no shell variables, so they also run in fish.
