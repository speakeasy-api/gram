---
"server": patch
---

Fix three Drata evidence-push defects found running against the live API. The stranded-session sweep failed to decode the session listing (Drata returns numeric session ids inside a data/pagination envelope; the sweep decoded them as strings and misreported the failure via a bare-array fallback) — session ids now tolerate numbers or strings, a null/absent data field counts as an empty sweep, and the envelope's real decode error surfaces. An empty fleet now clears evidence by deleting records directly, because Drata refuses to complete a session with no records. Per-record schema-validation rejections hidden inside 2xx upload responses now fail the push instead of silently publishing a partial fleet.
