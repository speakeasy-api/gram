---
"server": patch
---

Name the signed-in Claude account on Cowork sessions from their first upload. Inside the Cowork VM, where no device agent runs, `speakeasy-hooks` now reads the account email Claude provides and sends it with each transcript upload. A Cowork session no longer shows as anonymous when its first turn reaches Speakeasy before the session's first OTEL export.
