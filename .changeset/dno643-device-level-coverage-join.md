---
"server": minor
"dashboard": minor
---

feat: device-level agent coverage behind a rollout flag

Coverage can now match a device's hardware serial against per-device agent
heartbeats instead of its assigned-user email, falling back to email when no
serial match exists. Adds an `agent_other_device` bucket for "the user runs
the agent, just not on this machine", and an `attestation` field so clients
word the coverage claim to match the mode. Gated per org by the
`device-level-coverage` PostHog flag; evidence pushes stay user-level until
the sink field names change with them.
