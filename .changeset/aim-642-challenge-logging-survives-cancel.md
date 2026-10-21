---
"server": patch
---

Authorization challenges are recorded even when the client disconnects before the request finishes, and a canceled request no longer logs feature-flag cache lock errors.
