---
"server": patch
---

The hooks binary now tells the server when a Claude Cowork Stop hook ships no transcript, and why: an invalid transcript path, a read failure, or an empty transcript. The server logs the reason instead of the skip staying silent inside the Cowork VM.
