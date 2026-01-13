---
"server": patch
---

This change adds an `Accept: */*` header to requests from the tool proxy. This resolves issues with some APIs (eg. https://api.intercom.io) which rely on the Accept header's presence to return content
