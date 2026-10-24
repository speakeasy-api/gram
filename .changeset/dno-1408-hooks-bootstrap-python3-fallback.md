---
"server": patch
---

The hooks bootstrap now unpacks the hooks binary with `python3`, or failing that `bsdtar`, when a machine has neither `unzip` nor `busybox`, such as the official OpenClaw container image. On those machines the hooks binary used to fail to install, and plugins that fail closed blocked every agent tool call.
