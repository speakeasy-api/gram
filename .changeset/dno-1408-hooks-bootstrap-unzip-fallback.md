---
"server": patch
---

The hooks bootstrap script now installs the hooks binary on machines without `unzip` or `busybox`, such as slim container images like the official OpenClaw image. It falls back to extracting the archive with `python3`, then `bsdtar`, before reporting that no extraction tool is available.
