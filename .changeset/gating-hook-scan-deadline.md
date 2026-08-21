---
"server": patch
---

Risk enforcement scans on the synchronous gating hook path now run under a 3s deadline instead of being bounded only by the prompt-policy judge's 10s per-call timeout, so a slow judge can no longer hold a prompt or tool call open for up to ten seconds. The deadline is propagated into the scan rather than enforced around it, so each policy's own fail-open/fail-closed mode still decides the outcome on expiry. Override with `GRAM_HOOKS_GATING_SCAN_TIMEOUT`.
