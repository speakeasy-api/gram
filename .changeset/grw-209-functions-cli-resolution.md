---
"@gram-ai/functions": patch
---

Deploy with the renamed `speakeasy` CLI. The SDK uses `GRAM_CLI_PATH` when it is set, then the local `cli/bin/gram` build under `GRAM_DEV`, then `speakeasy` only when `speakeasy --control-plane-cli` confirms it is the AI Control Plane CLI and not the SDK generator, then `gram`. When none is found, the error lists the install commands.
