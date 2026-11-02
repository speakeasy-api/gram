---
"server": patch
---

A sign-in where the provider reports a zero or negative lifetime now works instead of showing as expired straight away. "Give access" can no longer issue a token while every connection for the server has expired, and the verify log now says why a connection had no usable credential.
