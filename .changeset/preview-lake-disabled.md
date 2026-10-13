---
"server": patch
---

Streams skip the lake consumer when Pub/Sub is emulated and no lake bucket is mapped, so preview environments start instead of exiting.
