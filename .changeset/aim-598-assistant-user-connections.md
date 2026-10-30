---
"server": minor
---

When an assistant acts for a person, servers that use that person's own account now use their connection, not the assistant owner's. If the person has not connected yet, or must reconnect, the assistant sends them a private link. The link works only for that person while signed in to Speakeasy and expires after 15 minutes. If they don't have access, the link page says "No access" and offers no way to connect. In Slack, the link goes to the person as an ephemeral message or DM, never into the channel.
