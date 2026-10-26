---
"server": patch
---

The hooks bootstrap now unpacks the hooks binary with `node` when a machine has none of `unzip`, `busybox`, `python3` or `bsdtar`, such as a slim container image built to run a Node app. On those machines the hooks binary used to fail to install, and plugins that fail closed blocked every agent tool call.
