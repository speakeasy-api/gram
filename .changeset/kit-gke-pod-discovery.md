---
"server": patch
---

Kit assistants on managed GKE respond again: the server now finds a Kit sandbox's pod through its assigned Sandbox when the cluster's sandbox controller does not label pods with their claim, instead of waiting until the turn times out and retrying on a fresh runtime.
