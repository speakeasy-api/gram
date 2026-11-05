---
"server": patch
---

Saving a server's Team Access or an auth client's access no longer fails with "unknown principal" when a rule names an agent that was deleted. Those rules are removed on save, and deleting an agent now removes its rules everywhere.
