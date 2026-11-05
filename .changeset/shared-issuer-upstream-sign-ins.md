---
"dashboard": patch
---

With upstream auth enforced, two remote MCP servers that share one organization-wide sign-in no longer show each other's upstream. The agent wizard's connection check, the connection it assigns when you create or save an agent, its Connect and Reauthenticate links, and the server's Enterprise Managed Authorization section now read each server's own credentials. Without enforcement they still read the sign-in's registrations, which route the call.
