---
"server": patch
---

DNO-1412: Agent API keys and headless devices acting for an agent can send hooks to a project with only `org:hooks_ingest` bounded to that project. Hook ingestion no longer also requires `project:read` on the project named in `Gram-Project`, so a hooks-only agent is no longer refused as forbidden. Every other route, and every caller that is not an agent, still requires `project:read` on the selected project.
