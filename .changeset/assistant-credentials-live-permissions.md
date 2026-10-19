---
"server": patch
---

Assistants no longer fail to respond in Slack when the person who mentions them has many permissions. An assistant's permissions for a turn are now checked against the current permissions of the assistant, its owner, and the person it acts for on every request, so a permission someone loses also stops applying to assistant turns already in progress.
