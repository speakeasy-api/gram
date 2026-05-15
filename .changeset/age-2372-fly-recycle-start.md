---
"server": patch
---

Assistant runtimes no longer get stuck unresponsive after a Gram release. When the assistant runtime image was upgraded in place, the underlying VM was being left stopped, so the next chat turn timed out and the assistant stopped responding. Subsequent turns now bring the runtime back up cleanly.
