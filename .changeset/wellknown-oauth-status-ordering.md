---
"server": patch
---

Build well-known OAuth metadata response body before writing 200 status so error paths surface as the real status code instead of 200 with an error body
