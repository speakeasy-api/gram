---
"server": patch
---

Pass the appropriate uintptr value in the slog Record when logging in `oops.ShareableError.Log()`. Previously, all log messages had their source location being the Log method itself which was not helpful.
