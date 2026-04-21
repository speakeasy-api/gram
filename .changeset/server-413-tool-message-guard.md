---
"server": patch
---

Add a defense-in-depth 413 guard on the `/completion` chat proxy — reject any
single tool-result message over 200KB with a clean HTTP 413 / `request_too_large`
error instead of forwarding to OpenRouter where it would surface as an opaque
"prompt is too long" 400. Clients are expected to truncate tool outputs
before sending (see `@gram-ai/elements` `tools.maxOutputBytes`), but this
guard keeps the error surface clean if they don't.
