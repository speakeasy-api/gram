---
"server": minor
---

A headless device can authenticate to the device-agent API as a workload instead of with an agent API key. It exchanges an identity assertion, from a platform issuer the organization trusts or from its Speakeasy device issuer, at its organization's `/oauth/workload/device-agent/{organization_id}/token` for a 15-minute bearer token. Plugin polling (`agent.getPlugins`) and hook ingestion (Cursor, Codex, and OpenTelemetry logs and metrics) accept that token in `Authorization: Bearer`. The workload stays the authenticated actor and acts for the agent assigned to it, under that agent's live permissions, so withdrawing the assignment or suspending the agent refuses the token on its next use. Hooks go to the project named in `Gram-Project`, which must be the project the workload's admission is bound to for hooks and one the agent may send hooks to; a workload whose admission names no hooks project sends no hooks.
