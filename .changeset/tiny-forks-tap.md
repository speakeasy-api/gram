---
"server": patch
---

Updated the CORS middleware to include the `User-Agent` header in the `Access-Control-Allow-Headers` response. This allows clients to send the `User-Agent` header in cross-origin requests which is useful for debugging and analytics purposes.
