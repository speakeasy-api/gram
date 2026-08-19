---
"server": minor
---

Creating a blocking shadow-MCP policy — or transitioning one into blocking — now replays the project's recorded MCP approval decisions onto it, in the same transaction. Previously, ordering decided what an approval meant: approve a server while no blocking policy exists and there is nothing to write a grant on, create the policy later, and the server was blocked while its review still read approved. The decision record stores its blast radius precisely so a later policy can honor it, and now it does: standing approvals get their bypass audience, standing denials get block rules under allow-by-default, and an allow-by-default policy that cannot express a standing person-scoped approval refuses to be created, naming the servers, instead of silently widening what was recorded.
