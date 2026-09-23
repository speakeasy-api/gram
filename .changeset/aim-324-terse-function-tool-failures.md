---
"@gram-ai/functions": patch
"function-runners": patch
---

Report a failed Gram Function tool call tersely. A JavaScript stack trace is no longer serialized into the failure response — it names minified frames inside the deployed bundle, which the tool's caller cannot act on — and is written to stderr instead, where it reaches the function's own logs. A failure caused by input validation now names each offending input on one line rather than repeating Zod's issue list as a pretty-printed JSON dump.
