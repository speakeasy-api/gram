---
"@gram-ai/elements": minor
---

Add an optional `transport` field to `ElementsConfig`. When provided, Elements routes the chat through the supplied `ChatTransport` instead of its built-in client-side streaming transport — enabling consumers (e.g. the Gram dashboard's Project Assistant sidebar) to back the chat with a server-side assistant.
