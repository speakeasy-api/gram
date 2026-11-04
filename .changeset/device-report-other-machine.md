---
"server": patch
---

Device coverage no longer credits a device with a policy report from another machine. When a device's own agent hasn't reported, its assigned user's report stands in only if it names no other serial and comes from the same OS family, so a MacBook no longer shows "Enforcing policy" because the same person's Windows machine reported.
