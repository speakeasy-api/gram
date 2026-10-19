---
"dashboard": patch
---

Point device agent setup at `speakeasyd` commands. The Linux steps now install only `speakeasyd`, so they no longer overwrite another `speakeasy` binary in `/usr/local/bin`. Status, enroll and config path commands use `speakeasyd` on every platform. With a resolved version, the Linux download command uses no shell variables, so it also runs in fish.
