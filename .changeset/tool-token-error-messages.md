---
"server": patch
---

HTTP tools that get their access token with client credentials now say what to check when that token request fails, instead of repeating the underlying network error. The tool result and its tool call log name the cause (the token URL uses an address Speakeasy does not connect to directly or does not point anywhere, the token service refused the request, or it could not be reached or answered with an error) and who should check what.
