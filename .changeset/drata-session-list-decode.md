---
"server": patch
---

Fix the Drata evidence push failing to decode the session listing: the production API returns numeric session ids inside a data/pagination envelope, which the stranded-session sweep decoded as strings and then misreported via a bare-array fallback. The sweep now tolerates numeric or string ids, treats a null/absent data field as an empty sweep, and reports the envelope's real decode error.
