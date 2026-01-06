---
"@gram-ai/elements": patch
---

The chat handler has been removed as the chat request now happens client side. A new session handler has been added to the server package, which should be implemented by consumers in their backends.
