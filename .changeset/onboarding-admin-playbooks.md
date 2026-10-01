---
"server": minor
"admin": minor
"dashboard": patch
---

Onboarding use cases and playbooks in the admin dashboard. Staff create use cases and playbooks on a new Use Cases & Playbooks page: a playbook is ordered top-level steps that belong to a use case, shared and possibly its default, or to one customer, never both, and every playbook is checked for prerequisites. An organization's Overview page shows its assigned playbook and links to the page scoped to that organization, where staff write it a playbook of its own or assign it a shared one, refused when the recorded stack does not support a step. A shared playbook is a template: assigning it gives the organization a copy of its own, so later edits to the shared playbook never reach an organization already on it. The onboarding survey assigns a use case's default playbook and the customer setup wizard walks the assigned one. The preset selection editor and its Admin API are gone, the Admin MCP diagnostics tool reports the assigned playbook instead, and the Admin MCP's onboarding write proposal assigns a playbook, by ID or by use case, in place of setting task visibility. Enable it with the `assign_organization_onboarding_playbook` write operation.
