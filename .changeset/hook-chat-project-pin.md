---
"server": patch
---

Hook ingest pins a session to the project that first created its chat, so a later request with a different project header cannot stamp messages onto that chat. Chat list counts and last-message times now ignore those sibling-project rows, matching the transcript the UI can actually load.
