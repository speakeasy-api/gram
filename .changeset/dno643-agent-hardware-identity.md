---
"server": minor
---

feat: accept and store device-agent hardware identity

agent.getPlugins now accepts optional Gram-Device-Serial and
Gram-Device-Hostname headers and records a per-device heartbeat alongside the
existing per-user one. Coverage is unchanged — this only builds the data the
device-level join will read.
