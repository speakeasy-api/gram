---
"server": minor
"dashboard": patch
---

Device coverage now says whether each active device enforces the policy Speakeasy served it. Every `agent_active` device carries a `policy_state` of `attested`, `outdated`, `unreported` or `halted`, the coverage counts break `agent_active` down by those states, and `listManagedDevices` can filter by them and returns the device's last reported agent state. A fresh state report now also counts as agent liveness, so a device whose build was blocked from polling still shows as active and halted.
