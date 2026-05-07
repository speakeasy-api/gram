---
"server": minor
---

The assistant runtime now compacts conversation history as it approaches the model's context window: older turns are summarised so long-running assistants can keep going past the original window limit. System prompt, context items, and the most recent turns are preserved.
