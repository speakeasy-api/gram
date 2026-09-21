---
"server": patch
---

Plugin package publishes triggered by someone who is not a member of the project's organization, such as a Speakeasy admin editing a customer's MCP servers, now publish right away under an existing organization member instead of failing and waiting for the hourly sweep. Publishes that cannot find any member are recorded once instead of being retried.
