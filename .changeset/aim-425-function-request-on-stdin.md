---
"function-runners": patch
---

Gram Functions tool calls and resource reads no longer fail with "argument list too long" when the request is larger than 128 KiB. The runner hands the request to the function on stdin instead of as a command-line argument, which Linux caps at 128 KiB per argument, so large inputs such as base64-encoded media now reach the function. Request bodies over 4 MiB are rejected with a 413.
