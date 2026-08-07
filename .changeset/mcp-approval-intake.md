---
"server": minor
---

Adds approval-request intake to the MCP approval API. Members can ask for a server to be reviewed by URL or launch command — no permission grant needed, matching the block and bypass surfaces — and admins can promote a risk-policy bypass request into the review queue, carrying the blocked employee's identity and justification with it. Repeat asks for the same server attach to the existing review rather than opening a second one, identity resolution runs at intake so evidence has something to hang off by the time an admin looks, and a re-request reopens a denied review with its decision history intact.
