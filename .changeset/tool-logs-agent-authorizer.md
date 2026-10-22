---
"server": minor
"dashboard": minor
---

Tool logs now show who an agent acted for. An agent call made on a person's behalf reads "agent ↳ on behalf of <person>", and filtering by that person includes the agent calls made for them. The new `agent_ids` filter narrows those to one agent. Calls recorded before this change show the agent alone.
