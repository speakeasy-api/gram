---
"server": minor
---

A headless device can authenticate to the device-agent API as a workload instead of with an agent API key. It exchanges an assertion from its organization's Speakeasy device issuer at `/oauth/workload/device-agent/token` for a 15-minute bearer token. Plugin polling (`agent.getPlugins`) and hook ingestion (Cursor, Codex, and OpenTelemetry logs and metrics) accept that token in `Authorization: Bearer`. The request then acts as the agent assigned to the workload, under that agent's live permissions, so withdrawing the assignment or suspending the agent refuses the token on its next use. Hooks use the agent's project when it has one; otherwise they use the project named in `Gram-Project`.
