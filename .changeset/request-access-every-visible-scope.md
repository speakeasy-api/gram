---
"server": patch
---

Requesting access now accepts every scope a person can be granted, including assistant, memory, auth client and risk policy block scopes, which the API previously refused before the request was checked. Requests stored for approval are still limited to the scopes that can be given temporarily.
