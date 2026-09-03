---
"server": patch
---

Device coverage no longer counts a cloud coding session as evidence that a developer's own machine is running the device agent. Cloud environments enroll with a single shared identity, so when that identity was a real person's address, their managed device could report as covered on the strength of a heartbeat from a sandbox. The agent now declares what kind of machine it runs on, and only heartbeats from an individual's endpoint satisfy the email-matched coverage fallback. Heartbeats from cloud sandboxes and shared servers are recorded separately, so the usage is still visible without inflating anyone's coverage.
