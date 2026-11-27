---
"server": minor
---

Accept platform-issued workload JWTs directly as `Authorization: Bearer` credentials at Project Gateways (`/mcp/gateway/{organizationSlug}/projects/{projectSlug}`) and the new project-scoped OpenAI-compatible inference route (`/projects/{projectID}/chat/completions`). Workload issuers must opt in with `allow_assertion_reuse` enabled, tokens must carry the endpoint's exact audience (`urn:speakeasy:mcp:project-gateway` or `urn:speakeasy:inference:workload`), and every request enforces live subject admission, agent assignment, and `mcp:connect` / `inference:execute` authorization.
