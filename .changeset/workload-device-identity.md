---
"server": minor
---

Headless boxes with no platform identity, such as a one-off VPS, can now enroll as workloads. An administrator who owns an agent, or holds agent:authorize on it, creates a single-use join token with `workloadIdentities.createDeviceJoinToken`, naming the project the box may send hooks to (`hooks_project_id`). The box registers its public key with `workloadIdentities.registerDevice` and is admitted, bound to that hooks project, and assigned that agent; the response returns the project's id and slug. Platform workloads take an optional `hooks_project_id` on `admitSubject` and `updateSubject`. Its signed assertions are then exchanged for workload sessions like any other workload's, and are verified against the key registered for that device alone.
