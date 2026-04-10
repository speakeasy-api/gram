---
"dashboard": patch
---

Fix playground credential saving failing with "length of slug must be lesser or equal than 40" error. The environment slug format was shortened to stay within the server's 40-character limit.
