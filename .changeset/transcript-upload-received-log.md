---
"server": patch
---

The server now logs every authenticated Claude transcript upload once, with its organization, project, session, entry and stored-row counts, response status, and the hooks binary build that sent it. Transcript uploads also carry the binary's device details on their request span. This makes one organization's uploads easy to follow, including uploads that store nothing.
