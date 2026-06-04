---
"server": minor
---

Add `POST /rpc/assistants.ensureManagedAssistant`: returns the project's built-in Project Assistant, provisioning it (idempotently) on first access so the dashboard sidebar can resolve it out of the box. Gated by project read access. Also renames the managed assistant to "Project Assistant for {project}" to match the dashboard's "Project Assistant" branding. Foundation for the AGE-2631 sidebar cutover.
