---
"server": patch
---

Shadow AI now reports ChatGPT Classic as running, not just installed. The scan target carries the app's process name alongside the bundle id it already had, so a device agent distinguishes the app being open from the app merely being present. Classic stays distinguishable from the current ChatGPT app, which ships under a different bundle id.
