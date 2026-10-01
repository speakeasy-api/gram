---
"server": patch
---

Anthropic inference hooks no longer record a second copy of a session that another capture lane already stores. An organization running inference hooks alongside agent hooks or the Anthropic compliance import used to get two conversations per session, metered and analyzed twice; a delivery whose session resolves to a chat one of those lanes owns in the same project now adopts that conversation, writing its acceptance checkpoint there and archiving nothing. Enforcement is unchanged: every delivery is still evaluated in full, and a conversation this endpoint already archived keeps its transcript so a session is never split in two.
