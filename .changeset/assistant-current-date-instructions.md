---
"server": patch
---

Re-inject the current date into the managed assistant's runtime instructions on every turn. The old AI Insights sidebar prompt included a dynamic date line; it was dropped when the prompt moved to a static server-side embed, leaving the assistant with no anchor for relative-time queries ("errors since Monday"). `composeInstructions` now appends `The current date is <UTC YYYY-MM-DD>.` per turn.
