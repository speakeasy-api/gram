---
"server": minor
"dashboard": patch
---

Add an `agent:create` permission that controls who can create agents they own. `agent:write` includes it, and the Member role has it by default, so members keep creating agents unless an admin removes it from their role. Agents and API keys can't be given it.
