---
"dashboard": patch
---

An agent's permission editor now offers `org:device_agent_sync` and `org:hooks_ingest` under Platform access, so an agent running the device agent can be given what it needs to enroll devices, fetch its plugins and send hook events without going through Access. Both apply to the whole organization. A stored `org:hooks_ingest` grant limited to particular projects is kept exactly as it is and cannot be edited here.
