---
"server": patch
---

Add a (chat_id, generation, created_at, seq) index on chat_messages so the DNO-536 transcript ordering — (created_at, seq) within a generation — is served by an ordered index scan and keyset pagination keeps its LIMIT early-stop instead of sorting the generation's full row set per page.
