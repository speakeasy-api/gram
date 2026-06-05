---
"@gram-ai/elements": minor
---

Add an optional `history.transformChatMessage` hook to `ElementsConfig`. It runs against every message loaded from `chat.load` before rendering: return a (possibly rewritten) message to render it, or `null` to omit it. This lets consumers keep product- or backend-specific transcript conventions (e.g. stripping a server-injected framing block, or hiding system events) out of the shared library — Elements itself stays agnostic to any such convention.
