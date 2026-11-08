---
"server": patch
"dashboard": patch
---

Assistant runtimes no longer record a security event each time they start or stop. Every sandbox admits and withdraws its own workload identity, so a busy assistant filled the security log with hundreds of entries that no one had to act on. Creating the runtime's shared issuer, and any person withdrawing it, are still recorded as configuration changes. An agent's Credentials page now shows its running sandboxes as one "Assistant runtime" row with a count, instead of a row per sandbox.
