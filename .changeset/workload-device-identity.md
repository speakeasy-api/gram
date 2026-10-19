---
"server": minor
---

Headless boxes with no platform identity, such as a one-off VPS, can now enroll as workloads. An administrator who owns an agent, or holds agent:authorize on it, creates a single-use join token with `workloadIdentities.createDeviceJoinToken`; the agent's policy must grant `org:device_agent_sync`, and nothing else is required to enroll. The box registers its public key with `workloadIdentities.registerDevice` and is admitted and assigned that agent. Its signed assertions are then exchanged for workload sessions like any other workload's, and are verified against the key registered for that device alone. Neither the join token nor the admission names a hooks project: a workload names its project on every hooks request, and the agent's `org:hooks_ingest` grant for that project decides whether it is accepted.
